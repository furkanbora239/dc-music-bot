package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	dca "github.com/user/dc-music-bot/internal/dca"
)

// StreamSession manages an active DCA encoding and streaming session to Discord voice.
type StreamSession struct {
	encodingSession *dca.EncodeSession
	streamSession   *dca.StreamingSession
	done            chan error
	cancel          context.CancelFunc
	mu              sync.Mutex
	closed          bool
}

// PlayStream starts encoding and streaming an audio URL directly to the given
// VoiceConnection. The streamURL is resolved lazily and refreshed on demand by
// resolver, which is wrapped in an HTTP range-chunked reader (see NewRangeReader)
// so that googlevideo-style URLs (which reject full GETs) can be streamed
// reliably. All StreamSession semantics (Pause/Resume/IsPaused/Position/Stop/
// Done/Wait) and vc.Speaking(true/false) behavior are preserved.
func PlayStream(ctx context.Context, resolver URLResolver, vc *discordgo.VoiceConnection, volume int) (*StreamSession, error) {
	if vc == nil {
		return nil, errors.New("voice connection is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Fail fast on an already-dead voice connection. Its OpusSend channel has
	// already been closed by discordgo, so starting a stream would be pointless.
	// (The dca StreamingSession is now panic-safe against a closed OpusSend and
	// would otherwise end gracefully with ErrVoiceConnClosed; this guard just
	// short-circuits the trivially-unrecoverable case without redesigning the
	// player.)
	if vc.Status == discordgo.VoiceConnectionStatusDead {
		return nil, errors.New("voice connection is dead")
	}

	dcaVolume := 256
	if volume > 0 && volume <= 100 {
		dcaVolume = (volume * 256) / 100
	}

	opts := *dca.StdEncodeOptions
	opts.RawOutput = true
	opts.Bitrate = 96
	opts.Volume = dcaVolume
	opts.Application = dca.AudioApplicationAudio
	opts.BufferedFrames = 100

	// streamCtx is cancelled on Stop so the range reader aborts promptly when
	// ffmpeg is killed (broken downstream pipe) without leaking HTTP requests.
	streamCtx, cancel := context.WithCancel(ctx)
	reader := NewRangeReader(streamCtx, resolver, RangeConfig{})

	encodingSession, err := dca.EncodeMem(reader, &opts)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start ffmpeg encoding: %w", err)
	}

	_ = vc.Speaking(true)

	// encodeDone is the channel the vendored dca StreamingSession reports its
	// completion error on (once). It is consumed by EXACTLY ONE goroutine below
	// to avoid the classic double-consumer race: previously both a local
	// goroutine and the player (via StreamSession.Done()) read the same channel
	// and raced to steal the single completion error, which left the player
	// hanging forever in select with no audio and no logs (BUG 2).
	encodeDone := make(chan error, 1)
	streamingSession := dca.NewStream(encodingSession, vc, encodeDone)

	// done is the public completion channel returned by Done(). It is buffered
	// so the single fan-out goroutine can forward the completion error without
	// ever blocking, even if the player has not yet started reading.
	done := make(chan error, 1)

	sess := &StreamSession{
		encodingSession: encodingSession,
		streamSession:   streamingSession,
		done:            done,
		cancel:          cancel,
	}

	// Single-consumer fan-out: this goroutine is the ONLY reader of encodeDone.
	// It (a) forwards the completion error to sess.done (exactly once),
	// (b) stops speaking to Discord, and (c) stops the session. Done()/Wait()
	// therefore each receive the completion exactly once.
	go func() {
		err := <-encodeDone
		_ = vc.Speaking(false)
		sess.Stop()
		sess.done <- err
	}()

	return sess, nil
}

// Pause pauses audio transmission.
func (s *StreamSession) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed && s.streamSession != nil {
		s.streamSession.SetPaused(true)
	}
}

// Resume resumes audio transmission.
func (s *StreamSession) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed && s.streamSession != nil {
		s.streamSession.SetPaused(false)
	}
}

// IsPaused returns true if playback is currently paused.
func (s *StreamSession) IsPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamSession != nil {
		return s.streamSession.Paused()
	}
	return false
}

// Position returns current playback duration.
func (s *StreamSession) Position() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamSession != nil {
		return s.streamSession.PlaybackPosition()
	}
	return 0
}

// Stop cleanly terminates the encoder and cleans up processes.
func (s *StreamSession) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		if s.cancel != nil {
			s.cancel()
		}
		if s.encodingSession != nil {
			s.encodingSession.Truncate()
			s.encodingSession.Cleanup()
		}
	}
}

// Done returns the channel that notifies upon playback finish or error.
func (s *StreamSession) Done() <-chan error {
	return s.done
}

// Wait blocks until playback finishes or fails. Returns nil on normal completion (io.EOF).
func (s *StreamSession) Wait() error {
	err := <-s.done
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
