package player

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

	session *discordgo.Session
	vc      *discordgo.VoiceConnection
	source  audio.Source
	queue   *Queue

	nowPlaying    *audio.Track
	streamSession *audio.StreamSession
	volume        int
	idleTimeout   time.Duration
	idleTimer     *time.Timer
	emptyTimer    *time.Timer

	autoplay      bool
	playedHistory []string
	lastTrack     *audio.Track

	voiceCtx     context.Context
	voiceCancel  context.CancelFunc
	wakeSignal   chan struct{}
	skipSignal   chan struct{}
	stopSignal   chan struct{}
	disconnectFn func(guildID string)

	mu     sync.RWMutex
	closed bool
}

// streamErrorHint returns an extra hint line to append to a user-facing stream
// resolution error message when the error indicates YouTube is bot-blocking
// the server's IP (yt-dlp's "Sign in to confirm you're not a bot"). It returns
// "" when the error does not match, so the base message format stays unchanged.
func streamErrorHint(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(strings.ToLower(err.Error()), "sign in to confirm") {
		return "\n💡 Hint: YouTube is bot-blocking this server's IP. Configure `YTDLP_COOKIES_FILE` (or `YTDLP_COOKIES_FROM_BROWSER`) in `.env` with fresh YouTube cookies and restart the bot."
	}
	return ""
}

// NewPlayer constructs a new guild player and launches its background playback loop.
func NewPlayer(
	guildID, voiceChannelID, textChannelID string,
	session *discordgo.Session,
	source audio.Source,
	volume int,
	idleTimeout time.Duration,
	defaultAutoplay bool,
	disconnectFn func(guildID string),
) *Player {
	p := &Player{
		guildID:        guildID,
		voiceChannelID: voiceChannelID,
		textChannelID:  textChannelID,
		session:        session,
		source:         source,
		queue:          NewQueue(),
		volume:         volume,
		idleTimeout:    idleTimeout,
		autoplay:       defaultAutoplay,
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

// VoiceChannelID returns the current voice channel ID of the player.
func (p *Player) VoiceChannelID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.voiceChannelID
}

// SetVoiceChannel updates target voice channel ID.
func (p *Player) SetVoiceChannel(chID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.voiceChannelID = chID
}

// StartEmptyTimer starts a countdown timer to disconnect if the voice channel is empty.
// If the timer is already running, this is a no-op so the existing countdown continues.
func (p *Player) StartEmptyTimer(timeout time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed || timeout <= 0 {
		return
	}

	if p.emptyTimer != nil {
		// Timer already running, let it continue
		return
	}

	slog.Info("Voice channel is empty of users, starting empty channel timer", "guild", p.guildID, "timeout", timeout)
	p.emptyTimer = time.AfterFunc(timeout, func() {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		p.emptyTimer = nil
		p.mu.Unlock()

		slog.Info("Empty voice channel timeout reached, disconnecting", "guild", p.guildID)
		p.sendTextMessage("⏱️ Disconnected from voice: channel was empty.")
		p.Close()
	})
}

// CancelEmptyTimer cancels any active empty channel timer.
func (p *Player) CancelEmptyTimer() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.emptyTimer != nil {
		slog.Info("Voice channel is no longer empty, cancelled empty timer", "guild", p.guildID)
		p.emptyTimer.Stop()
		p.emptyTimer = nil
	}
}

// Enqueue adds a track to the queue and awakens the loop if idle.
func (p *Player) Enqueue(track *audio.Track) {
	p.queue.Push(track)
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

// Autoplay returns true if autoplay is currently enabled.
func (p *Player) Autoplay() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.autoplay
}

// SetAutoplay enables or disables autoplay mode.
func (p *Player) SetAutoplay(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.autoplay = enabled
}

// ToggleAutoplay toggles autoplay mode and returns the new state.
func (p *Player) ToggleAutoplay() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.autoplay = !p.autoplay
	return p.autoplay
}

func (p *Player) recordHistory(id string) {
	if id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.playedHistory = append(p.playedHistory, id)
	if len(p.playedHistory) > 50 {
		p.playedHistory = p.playedHistory[len(p.playedHistory)-50:]
	}
}

func (p *Player) isInHistory(id string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, h := range p.playedHistory {
		if h == id {
			return true
		}
	}
	return false
}

func (p *Player) fetchAutoplayTrack(seed *audio.Track) *audio.Track {
	if seed == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	recs, err := p.source.GetRecommendations(ctx, seed, 10)
	if err != nil || len(recs) == 0 {
		slog.Warn("Failed to fetch autoplay recommendations", "error", err, "seed", seed.Title)
		return nil
	}

	// 1. Pick the first recommendation not in recent history
	for _, rec := range recs {
		if rec.ID == seed.ID {
			continue
		}
		if !p.isInHistory(rec.ID) {
			rec.RequestedBy = "Autoplay 📻"
			p.recordHistory(rec.ID)
			return rec
		}
	}

	// 2. If all recommendations were in history, pick the first non-seed one
	for _, rec := range recs {
		if rec.ID != seed.ID {
			rec.RequestedBy = "Autoplay 📻"
			p.recordHistory(rec.ID)
			return rec
		}
	}

	return nil
}

// Close destroys the player and terminates voice connection.
func (p *Player) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	if p.queue != nil {
		p.queue.Clear()
	}
	if p.streamSession != nil {
		p.streamSession.Stop()
	}
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}
	if p.emptyTimer != nil {
		p.emptyTimer.Stop()
		p.emptyTimer = nil
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
			var seedTrack *audio.Track
			if p.nowPlaying != nil {
				seedTrack = p.nowPlaying
				p.lastTrack = p.nowPlaying
			} else {
				seedTrack = p.lastTrack
			}
			canAutoplay := p.autoplay && loopMode == LoopOff && seedTrack != nil

			if canAutoplay {
				p.mu.Unlock()
				slog.Info("Queue empty and autoplay enabled, fetching recommendation", "guild", p.guildID, "seed", seedTrack.Title)
				rec := p.fetchAutoplayTrack(seedTrack)

				p.mu.Lock()
				if p.closed {
					p.mu.Unlock()
					return
				}
				if !p.queue.IsEmpty() {
					nextTrack = p.queue.Pop()
					if rec != nil {
						p.queue.Push(rec)
					}
				} else if rec != nil {
					nextTrack = rec
				}
			}
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
		p.lastTrack = nextTrack
		if nextTrack.ID != "" {
			p.playedHistory = append(p.playedHistory, nextTrack.ID)
			if len(p.playedHistory) > 50 {
				p.playedHistory = p.playedHistory[len(p.playedHistory)-50:]
			}
		}
		targetVoiceCh := p.voiceChannelID
		volume := p.volume
		p.mu.Unlock()

		// Ensure voice connection is active
		if err := p.ensureVoiceConnection(targetVoiceCh); err != nil {
			slog.Error("Failed to connect to voice channel", "error", err, "guild", p.guildID)
			p.sendTextMessage(fmt.Sprintf("❌ Could not connect to voice channel: %v", err))
			continue
		}

		// Resolve the direct stream URL for playback.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		streamURL, err := p.source.GetStreamURL(ctx, nextTrack)
		cancel()

		if err != nil || streamURL == "" {
			slog.Error("Failed to resolve stream URL", "error", err, "track", nextTrack.Title)
			p.sendTextMessage(fmt.Sprintf("❌ Error resolving stream for **%s**: %v%s", nextTrack.Title, err, streamErrorHint(err)))
			continue
		}

		slog.Info("Audio stream URL acquired, initializing playback", "track", nextTrack.Title, "volume", volume)

		// Start streaming. Directly streams the resolved stream URL via FFmpeg with native auto-reconnect.
		streamSess, err := audio.PlayStream(context.Background(), func(ctx context.Context) (string, error) {
			if nextTrack.StreamURL != "" {
				return nextTrack.StreamURL, nil
			}
			return p.source.GetStreamURL(ctx, nextTrack)
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

	if p.vc != nil && p.vc.Status == discordgo.VoiceConnectionStatusReady && (voiceChID == "" || voiceChID == p.voiceChannelID) {
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

	slog.Info("Joining voice channel", "guild", p.guildID, "channel", voiceChID)
	vc, err := p.session.ChannelVoiceJoin(p.voiceCtx, p.guildID, voiceChID, false, false)
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
	p.voiceChannelID = voiceChID
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
	sess := p.session
	autoplayEnabled := p.autoplay
	p.mu.RUnlock()
	if chID == "" || sess == nil {
		return
	}

	autoplayStr := "Off"
	autoplayBtnStyle := discordgo.SecondaryButton
	if autoplayEnabled {
		autoplayStr = "On"
		autoplayBtnStyle = discordgo.SuccessButton
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
			Text: fmt.Sprintf("Requested by %s • Loop: %s • Autoplay: %s", track.RequestedBy, p.queue.LoopMode().String(), autoplayStr),
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
				discordgo.Button{
					Label:    "📻 Autoplay",
					Style:    autoplayBtnStyle,
					CustomID: "btn_toggle_autoplay",
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
