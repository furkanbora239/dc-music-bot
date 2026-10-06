package dca

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// fakeOpusReader is an in-memory OpusReader used for tests. It hands back the
// pre-loaded frames in order and then returns io.EOF, mimicking a real encoder
// that has exhausted its source.
type fakeOpusReader struct {
	frames [][]byte
	idx    int
	dur    time.Duration
}

func (f *fakeOpusReader) OpusFrame() ([]byte, error) {
	if f.idx >= len(f.frames) {
		return nil, io.EOF
	}
	fr := f.frames[f.idx]
	f.idx++
	return fr, nil
}

func (f *fakeOpusReader) FrameDuration() time.Duration {
	return f.dur
}

// TestNewStream_ClosedOpusSend_NoPanic reproduces the production crash:
// discordgo closes vc.OpusSend when the voice session dies (websocket 4006).
// NewStream must NOT panic; the done channel must receive ErrVoiceConnClosed
// exactly once and the stream goroutine must exit cleanly.
func TestNewStream_ClosedOpusSend_NoPanic(t *testing.T) {
	vc := &discordgo.VoiceConnection{OpusSend: make(chan []byte)}
	// Simulate discordgo tearing down the voice connection.
	close(vc.OpusSend)

	src := &fakeOpusReader{
		frames: [][]byte{{0x01, 0x02, 0x03}},
		dur:    20 * time.Millisecond,
	}

	// Buffered so the internal notification goroutine never blocks.
	done := make(chan error, 1)

	// If the stream goroutine still panicked, the whole test process would die.
	_ = NewStream(src, vc, done)

	select {
	case err := <-done:
		if !errors.Is(err, ErrVoiceConnClosed) {
			t.Fatalf("expected ErrVoiceConnClosed, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out: stream did not report completion on closed OpusSend")
	}

	// Ensure completion is delivered EXACTLY ONCE: a second read must block.
	select {
	case err := <-done:
		t.Fatalf("done delivered more than once: %v", err)
	case <-time.After(200 * time.Millisecond):
		// expected: no second delivery
	}
}

// TestNewStream_LiveOpusSend_FramesFlow verifies the normal path still works:
// with a live OpusSend channel, frames flow through and done fires on source EOF.
func TestNewStream_LiveOpusSend_FramesFlow(t *testing.T) {
	vc := &discordgo.VoiceConnection{OpusSend: make(chan []byte)}
	src := &fakeOpusReader{
		frames: [][]byte{{0x01}, {0x02}, {0x03}},
		dur:    20 * time.Millisecond,
	}
	done := make(chan error, 1)

	received := make(chan int, 1)
	go func() {
		count := 0
		for range vc.OpusSend {
			count++
		}
		received <- count
	}()

	_ = NewStream(src, vc, done)

	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("expected io.EOF, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out: stream did not finish on source EOF")
	}

	// Stream goroutine has already exited (EOF returned before any further
	// send), so closing OpusSend is safe and lets the drainer finish.
	close(vc.OpusSend)

	select {
	case count := <-received:
		if count != len(src.frames) {
			t.Fatalf("expected %d frames sent, got %d", len(src.frames), count)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drainer did not observe the sent frames")
	}
}
