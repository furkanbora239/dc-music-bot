package player

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/user/dc-music-bot/internal/audio"
)

// Player manages playback and voice states for a specific guild.
type Player struct {
	guildID        string
	voiceChannelID string
	textChannelID  string

	session  *discordgo.Session
	vc       *discordgo.VoiceConnection
	resolver *audio.Resolver
	queue    *Queue

	nowPlaying    *audio.Track
	streamSession *audio.StreamSession
	volume        int
	idleTimeout   time.Duration
	idleTimer     *time.Timer

	voiceCtx     context.Context
	voiceCancel  context.CancelFunc
	wakeSignal   chan struct{}
	skipSignal   chan struct{}
	stopSignal   chan struct{}
	disconnectFn func(guildID string)

	mu     sync.RWMutex
	closed bool
}

// NewPlayer constructs a new guild player and launches its background playback loop.
func NewPlayer(
	guildID, voiceChannelID, textChannelID string,
	session *discordgo.Session,
	resolver *audio.Resolver,
	volume int,
	idleTimeout time.Duration,
	disconnectFn func(guildID string),
) *Player {
	p := &Player{
		guildID:        guildID,
		voiceChannelID: voiceChannelID,
		textChannelID:  textChannelID,
		session:        session,
		resolver:       resolver,
		queue:          NewQueue(),
		volume:         volume,
		idleTimeout:    idleTimeout,
		wakeSignal:     make(chan struct{}, 1),
		skipSignal:     make(chan struct{}, 1),
		stopSignal:     make(chan struct{}, 1),
		disconnectFn:   disconnectFn,
	}

	go p.playbackLoop()
	return p
}

// Queue returns the player's track queue.
func (p *Player) Queue() *Queue {
	return p.queue
}

// NowPlaying returns the track currently playing, or nil.
func (p *Player) NowPlaying() *audio.Track {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nowPlaying
}

// Volume returns current volume.
func (p *Player) Volume() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.volume
}

// SetVolume sets the volume level (1-100).
func (p *Player) SetVolume(v int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v < 1 {
		v = 1
	} else if v > 100 {
		v = 100
	}
	p.volume = v
}

// SetTextChannel updates the channel where messages are sent.
func (p *Player) SetTextChannel(chID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.textChannelID = chID
}

// SetVoiceChannel updates target voice channel ID.
func (p *Player) SetVoiceChannel(chID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.voiceChannelID = chID
}

// Enqueue adds a track to the queue and awakens the loop if idle.
func (p *Player) Enqueue(track *audio.Track) {
	p.queue.Push(track)
	select {
	case p.wakeSignal <- struct{}{}:
	default:
	}
}

// EnqueueFront adds a track to the front of the queue.
func (p *Player) EnqueueFront(track *audio.Track) {
	p.queue.PushFront(track)
	select {
	case p.wakeSignal <- struct{}{}:
	default:
	}
}

// TogglePause toggles playback pause state. Returns true if now paused.
func (p *Player) TogglePause() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.streamSession == nil {
		return false
	}
	if p.streamSession.IsPaused() {
		p.streamSession.Resume()
		return false
	}
	p.streamSession.Pause()
	return true
}

// Skip interrupts the current track and moves to the next.
func (p *Player) Skip() {
	select {
	case p.skipSignal <- struct{}{}:
	default:
	}
}

// Stop stops playback, flushes queue, and disconnects.
func (p *Player) Stop() {
	select {
	case p.stopSignal <- struct{}{}:
	default:
	}
}

// Close destroys the player and terminates voice connection.
func (p *Player) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.queue.Clear()
	if p.streamSession != nil {
		p.streamSession.Stop()
	}
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}
	if p.voiceCancel != nil {
		p.voiceCancel()
		p.voiceCancel = nil
	}
	if p.vc != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = p.vc.Disconnect(ctx)
		cancel()
		p.vc = nil
	}
	p.mu.Unlock()

	if p.disconnectFn != nil {
		p.disconnectFn(p.guildID)
	}
}

// playbackLoop is the core worker goroutine processing tracks.
func (p *Player) playbackLoop() {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}

		loopMode := p.queue.LoopMode()
		var nextTrack *audio.Track

		if loopMode == LoopTrack && p.nowPlaying != nil {
			nextTrack = p.nowPlaying
		} else {
			if loopMode == LoopQueue && p.nowPlaying != nil {
				p.queue.Push(p.nowPlaying)
			}
			nextTrack = p.queue.Pop()
		}

		if nextTrack == nil {
			p.nowPlaying = nil
			// Start idle timer
			if p.idleTimer != nil {
				p.idleTimer.Stop()
			}
			p.idleTimer = time.NewTimer(p.idleTimeout)
			p.mu.Unlock()

			slog.Info("Player entered idle state", "guild", p.guildID, "timeout", p.idleTimeout)

			select {
			case <-p.wakeSignal:
				p.mu.Lock()
				if p.idleTimer != nil {
					p.idleTimer.Stop()
				}
				p.mu.Unlock()
				continue
			case <-p.idleTimer.C:
				slog.Info("Player idle timeout reached, leaving voice", "guild", p.guildID)
				p.sendTextMessage("⏱️ Disconnected from voice due to inactivity.")
				p.Close()
				return
			case <-p.stopSignal:
				slog.Info("Player stop signal received during idle", "guild", p.guildID)
				p.Close()
				return
			}
		}

		p.nowPlaying = nextTrack
		targetVoiceCh := p.voiceChannelID
		volume := p.volume
		p.mu.Unlock()

		// Ensure voice connection is active
		if err := p.ensureVoiceConnection(targetVoiceCh); err != nil {
			slog.Error("Failed to connect to voice channel", "error", err, "guild", p.guildID)
			p.sendTextMessage(fmt.Sprintf("❌ Could not connect to voice channel: %v", err))
			continue
		}

		// Preflight: ensure the track can be resolved to a stream URL before
		// committing to playback. The range reader will re-resolve (fresh) on
		// demand during streaming via ResolveStreamURLFresh.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		preflightURL, err := p.resolver.ResolveStreamURL(ctx, nextTrack)
		cancel()

		if err != nil || preflightURL == "" {
			slog.Error("Failed to resolve stream URL", "error", err, "track", nextTrack.Title)
			p.sendTextMessage(fmt.Sprintf("❌ Error resolving stream for **%s**: %v", nextTrack.Title, err))
			continue
		}

		slog.Info("Audio stream URL acquired, initializing playback", "track", nextTrack.Title, "volume", volume)

		// Start streaming. The resolver closure force-refreshes the track's
		// stream URL (bypassing its cache) on every fresh URL request, so the
		// range reader can recover from mid-stream 403s by re-resolving at the
		// same byte offset. Passing a nil context lets PlayStream manage its own
		// cancellable context, which is cancelled by StreamSession.Stop on
		// skip/stop so no orphaned HTTP requests remain.
		streamSess, err := audio.PlayStream(nil, func(ctx context.Context) (string, error) {
			url, err := p.resolver.ResolveStreamURLFresh(ctx, nextTrack)
			if err != nil {
				return "", err
			}
			if url == "" {
				return "", fmt.Errorf("resolver returned empty stream URL for track %q", nextTrack.Title)
			}
			return url, nil
		}, p.vc, volume)
		if err != nil {
			slog.Error("Failed to play audio stream", "error", err, "track", nextTrack.Title)
			p.sendTextMessage(fmt.Sprintf("❌ Audio playback failed for **%s**: %v", nextTrack.Title, err))
			continue
		}

		p.mu.Lock()
		p.streamSession = streamSess
		p.mu.Unlock()

		slog.Info("Playback successfully started", "track", nextTrack.Title)

		// Send Now Playing notification
		p.sendNowPlayingEmbed(nextTrack)

		// Wait for completion, skip, or stop
		select {
		case err := <-streamSess.Done():
			if err != nil {
				slog.Warn("Track stream ended with error", "error", err, "track", nextTrack.Title)
			} else {
				slog.Info("Track playback finished normally", "track", nextTrack.Title)
			}
		case <-p.skipSignal:
			slog.Info("Track skipped", "guild", p.guildID, "track", nextTrack.Title)
			streamSess.Stop()
		case <-p.stopSignal:
			slog.Info("Playback stopped by user", "guild", p.guildID)
			streamSess.Stop()
			p.Close()
			return
		}

		p.mu.Lock()
		p.streamSession = nil
		p.mu.Unlock()
	}
}

func (p *Player) ensureVoiceConnection(voiceChID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.vc != nil && p.vc.Status == discordgo.VoiceConnectionStatusReady {
		return nil
	}

	if p.vc != nil {
		if p.voiceCancel != nil {
			p.voiceCancel()
			p.voiceCancel = nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = p.vc.Disconnect(ctx)
		cancel()
		p.vc = nil
	}

	p.voiceCtx, p.voiceCancel = context.WithCancel(context.Background())

	vc, err := p.session.ChannelVoiceJoin(p.voiceCtx, p.guildID, voiceChID, false, true)
	if err != nil {
		p.voiceCancel()
		p.voiceCancel = nil
		return err
	}

	// Await DAVE E2EE protocol handshake if required by Discord voice server
	daveCtx, daveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer daveCancel()
	if err := vc.WaitForDAVEReady(daveCtx); err != nil {
		slog.Warn("DAVE E2EE handshake warning", "error", err, "guild", p.guildID)
	}

	p.vc = vc
	return nil
}

func (p *Player) sendTextMessage(msg string) {
	p.mu.RLock()
	chID := p.textChannelID
	p.mu.RUnlock()
	if chID != "" && p.session != nil {
		_, _ = p.session.ChannelMessageSend(chID, msg)
	}
}

func (p *Player) sendNowPlayingEmbed(track *audio.Track) {
	p.mu.RLock()
	chID := p.textChannelID
	p.mu.RUnlock()
	if chID == "" || p.session == nil {
		return
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🎶 Now Playing",
		Description: fmt.Sprintf("**[%s](%s)**", track.Title, track.WebpageURL),
		Color:       0x1DB954, // Spotify-like vibrant green
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Artist / Channel",
				Value:  track.Artist,
				Inline: true,
			},
			{
				Name:   "Duration",
				Value:  track.FormatDuration(),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Requested by %s • Loop: %s", track.RequestedBy, p.queue.LoopMode().String()),
		},
	}

	if track.Thumbnail != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: track.Thumbnail,
		}
	}

	// Interactive components (Buttons)
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "⏯️ Pause / Resume",
					Style:    discordgo.SecondaryButton,
					CustomID: "btn_toggle_pause",
				},
				discordgo.Button{
					Label:    "⏭️ Skip",
					Style:    discordgo.PrimaryButton,
					CustomID: "btn_skip",
				},
				discordgo.Button{
					Label:    "⏹️ Stop",
					Style:    discordgo.DangerButton,
					CustomID: "btn_stop",
				},
				discordgo.Button{
					Label:    "🔁 Loop",
					Style:    discordgo.SecondaryButton,
					CustomID: "btn_toggle_loop",
				},
			},
		},
	}

	msg := &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	}

	_, _ = p.session.ChannelMessageSendComplex(chID, msg)
}
