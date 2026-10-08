package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/user/dc-music-bot/internal/player"
)

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleSlashCommand(s, i)
	case discordgo.InteractionMessageComponent:
		b.handleButtonComponent(s, i)
	}
}

func (b *Bot) handleSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	switch data.Name {
	case "play":
		b.handlePlay(s, i)
	case "pause":
		b.handlePause(s, i)
	case "resume":
		b.handleResume(s, i)
	case "skip":
		b.handleSkip(s, i)
	case "stop":
		b.handleStop(s, i)
	case "queue":
		b.handleQueue(s, i)
	case "nowplaying":
		b.handleNowPlaying(s, i)
	case "loop":
		b.handleLoop(s, i)
	case "shuffle":
		b.handleShuffle(s, i)
	case "volume":
		b.handleVolume(s, i)
	case "autoplay":
		b.handleAutoplay(s, i)
	case "help":
		b.handleHelp(s, i)
	default:
		b.respondText(s, i, "Unknown command", true)
	}
}

func (b *Bot) handlePlay(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Defer response immediately to avoid Discord timeout
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	query := i.ApplicationCommandData().Options[0].StringValue()
	voiceChID := b.findUserVoiceChannel(s, i.GuildID, i.Member.User.ID)
	if voiceChID == "" {
		b.editDeferredResponse(s, i, "❌ You must be inside a voice channel to use `/play`.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	tracks, err := b.source.Search(ctx, query)
	if err != nil || len(tracks) == 0 {
		errMsg := "no tracks found"
		if err != nil {
			errMsg = err.Error()
		}
		b.editDeferredResponse(s, i, fmt.Sprintf("❌ Could not find track for query: `%s` (%s)", query, errMsg))
		return
	}

	track := tracks[0]
	track.RequestedBy = i.Member.User.Username

	p := b.manager.GetOrCreate(
		i.GuildID,
		voiceChID,
		i.ChannelID,
		s,
		b.source,
		b.cfg.DefaultVolume,
		b.cfg.IdleTimeout,
		b.cfg.DefaultAutoplay,
	)
	p.CancelEmptyTimer()

	currentPlaying := p.NowPlaying()
	for _, t := range tracks {
		t.RequestedBy = i.Member.User.Username
		p.Enqueue(t)
	}

	if len(tracks) > 1 {
		if currentPlaying == nil {
			b.editDeferredResponse(s, i, fmt.Sprintf("📑 Loaded playlist (**%d** tracks)! ▶️ Now playing: **[%s](%s)** by *%s*",
				len(tracks), tracks[0].Title, tracks[0].WebpageURL, tracks[0].Artist))
		} else {
			b.editDeferredResponse(s, i, fmt.Sprintf("📑 Added **%d** tracks from playlist to queue! Starting with **[%s](%s)**",
				len(tracks), tracks[0].Title, tracks[0].WebpageURL))
		}
	} else {
		track := tracks[0]
		if currentPlaying == nil {
			b.editDeferredResponse(s, i, fmt.Sprintf("▶️ Loading **[%s](%s)** by *%s*...", track.Title, track.WebpageURL, track.Artist))
		} else {
			pos := p.Queue().Size()
			b.editDeferredResponse(s, i, fmt.Sprintf("✅ Added to queue: **[%s](%s)** by *%s* (`%s`) — Position #%d",
				track.Title, track.WebpageURL, track.Artist, track.FormatDuration(), pos))
		}
	}
}

func (b *Bot) handlePause(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil || p.NowPlaying() == nil {
		b.respondText(s, i, "❌ Nothing is currently playing.", true)
		return
	}
	p.TogglePause()
	b.respondText(s, i, "⏸️ Playback paused.", false)
}

func (b *Bot) handleResume(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil || p.NowPlaying() == nil {
		b.respondText(s, i, "❌ Nothing is currently playing.", true)
		return
	}
	p.TogglePause()
	b.respondText(s, i, "▶️ Playback resumed.", false)
}

func (b *Bot) handleSkip(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil || p.NowPlaying() == nil {
		b.respondText(s, i, "❌ Nothing is currently playing.", true)
		return
	}
	p.Skip()
	b.respondText(s, i, "⏭️ Skipped current track.", false)
}

func (b *Bot) handleStop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil {
		b.respondText(s, i, "❌ Bot is not active in voice.", true)
		return
	}
	p.Stop()
	b.respondText(s, i, "⏹️ Stopped playback, cleared queue, and left voice.", false)
}

func (b *Bot) handleQueue(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil {
		b.respondText(s, i, "❌ Queue is empty.", true)
		return
	}

	tracks := p.Queue().List()
	nowPlaying := p.NowPlaying()

	if nowPlaying == nil && len(tracks) == 0 {
		b.respondText(s, i, "❌ Queue is currently empty.", true)
		return
	}

	var sb strings.Builder
	if nowPlaying != nil {
		sb.WriteString(fmt.Sprintf("**Now Playing:**\n▶️ [%s](%s) (`%s`) [Requested by: %s]\n\n",
			nowPlaying.Title, nowPlaying.WebpageURL, nowPlaying.FormatDuration(), nowPlaying.RequestedBy))
	}

	if len(tracks) > 0 {
		sb.WriteString("**Upcoming in Queue:**\n")
		limit := 10
		if len(tracks) < limit {
			limit = len(tracks)
		}
		for idx := 0; idx < limit; idx++ {
			t := tracks[idx]
			sb.WriteString(fmt.Sprintf("`%d.` [%s](%s) (`%s`)\n",
				idx+1, t.Title, t.WebpageURL, t.FormatDuration()))
		}
		if len(tracks) > limit {
			sb.WriteString(fmt.Sprintf("\n*...and %d more tracks*", len(tracks)-limit))
		}
	} else {
		sb.WriteString("*No upcoming tracks in queue.*")
	}

	embed := &discordgo.MessageEmbed{
		Title:       "📜 Current Music Queue",
		Description: sb.String(),
		Color:       0x5865F2,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Total songs in queue: %d • Loop: %s", len(tracks), p.Queue().LoopMode().String()),
		},
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

func (b *Bot) handleNowPlaying(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil || p.NowPlaying() == nil {
		b.respondText(s, i, "❌ Nothing is currently playing.", true)
		return
	}

	track := p.NowPlaying()
	embed := &discordgo.MessageEmbed{
		Title:       "🎶 Now Playing",
		Description: fmt.Sprintf("**[%s](%s)**", track.Title, track.WebpageURL),
		Color:       0x1DB954,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Artist", Value: track.Artist, Inline: true},
			{Name: "Duration", Value: track.FormatDuration(), Inline: true},
			{Name: "Volume", Value: fmt.Sprintf("%d%%", p.Volume()), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Requested by %s • Loop: %s", track.RequestedBy, p.Queue().LoopMode().String()),
		},
	}
	if track.Thumbnail != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: track.Thumbnail}
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

func (b *Bot) handleLoop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil {
		b.respondText(s, i, "❌ Bot is not active in voice.", true)
		return
	}

	modeStr := i.ApplicationCommandData().Options[0].StringValue()
	switch modeStr {
	case "track":
		p.Queue().SetLoopMode(player.LoopTrack)
		b.respondText(s, i, "🔂 Loop mode set to: **Repeat Track**", false)
	case "queue":
		p.Queue().SetLoopMode(player.LoopQueue)
		b.respondText(s, i, "🔁 Loop mode set to: **Repeat Queue**", false)
	default:
		p.Queue().SetLoopMode(player.LoopOff)
		b.respondText(s, i, "➡️ Loop mode turned **Off**", false)
	}
}

func (b *Bot) handleShuffle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil || p.Queue().IsEmpty() {
		b.respondText(s, i, "❌ Not enough songs in queue to shuffle.", true)
		return
	}
	p.Queue().Shuffle()
	b.respondText(s, i, "🔀 Queue has been randomly shuffled!", false)
}

func (b *Bot) handleVolume(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil {
		b.respondText(s, i, "❌ Bot is not active in voice.", true)
		return
	}
	lvl := int(i.ApplicationCommandData().Options[0].IntValue())
	p.SetVolume(lvl)
	b.respondText(s, i, fmt.Sprintf("🔊 Volume set to **%d%%**", lvl), false)
}

func (b *Bot) handleAutoplay(s *discordgo.Session, i *discordgo.InteractionCreate) {
	p := b.manager.Get(i.GuildID)
	if p == nil {
		b.respondText(s, i, "❌ Bot is not active in voice.", true)
		return
	}

	opts := i.ApplicationCommandData().Options
	if len(opts) == 0 {
		enabled := p.ToggleAutoplay()
		if enabled {
			b.respondText(s, i, "📻 Autoplay is now **enabled**! Recommended songs will play automatically when the queue is empty.", false)
		} else {
			b.respondText(s, i, "📻 Autoplay is now **disabled**.", false)
		}
		return
	}

	mode := opts[0].StringValue()
	switch mode {
	case "on":
		p.SetAutoplay(true)
		b.respondText(s, i, "📻 Autoplay is now **enabled**! Recommended songs will play automatically when the queue is empty.", false)
	case "off":
		p.SetAutoplay(false)
		b.respondText(s, i, "📻 Autoplay is now **disabled**.", false)
	case "status":
		status := "disabled"
		if p.Autoplay() {
			status = "enabled"
		}
		b.respondText(s, i, fmt.Sprintf("📻 Autoplay is currently **%s**.", status), false)
	default:
		b.respondText(s, i, "❌ Invalid mode. Use `on`, `off`, or `status`.", true)
	}
}

func (b *Bot) handleHelp(s *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := &discordgo.MessageEmbed{
		Title:       "📖 Music Bot Help & Commands",
		Description: "Self-hosted Go Discord Music Bot powered by YouTube Music and yt-dlp.",
		Color:       0x5865F2,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "`/play <query>`", Value: "Search YouTube Music or input URL to play audio", Inline: false},
			{Name: "`/pause` & `/resume`", Value: "Pause or unpause the music stream", Inline: false},
			{Name: "`/skip`", Value: "Skip the currently playing track", Inline: false},
			{Name: "`/stop`", Value: "Stop playing, wipe queue, and leave voice channel", Inline: false},
			{Name: "`/queue`", Value: "Display the list of upcoming songs", Inline: false},
			{Name: "`/nowplaying`", Value: "Show details of the current track", Inline: false},
			{Name: "`/loop <off|track|queue>`", Value: "Configure repeat options", Inline: false},
			{Name: "`/shuffle`", Value: "Randomly shuffle upcoming songs", Inline: false},
			{Name: "`/volume <1-100>`", Value: "Adjust voice audio volume", Inline: false},
			{Name: "`/autoplay [on|off|status]`", Value: "Toggle or configure autoplay for recommended tracks", Inline: false},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Open-source Discord Music Bot written in Go",
		},
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

func (b *Bot) handleButtonComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	p := b.manager.Get(i.GuildID)
	if p == nil {
		b.respondText(s, i, "❌ Bot is no longer active in voice.", true)
		return
	}

	switch customID {
	case "btn_toggle_pause":
		paused := p.TogglePause()
		if paused {
			b.respondText(s, i, "⏸️ Playback paused.", true)
		} else {
			b.respondText(s, i, "▶️ Playback resumed.", true)
		}
	case "btn_skip":
		p.Skip()
		b.respondText(s, i, "⏭️ Skipped current track.", true)
	case "btn_stop":
		p.Stop()
		b.respondText(s, i, "⏹️ Stopped playback.", true)
	case "btn_toggle_loop":
		current := p.Queue().LoopMode()
		nextMode := player.LoopOff
		switch current {
		case player.LoopOff:
			nextMode = player.LoopTrack
		case player.LoopTrack:
			nextMode = player.LoopQueue
		case player.LoopQueue:
			nextMode = player.LoopOff
		}
		p.Queue().SetLoopMode(nextMode)
		b.respondText(s, i, fmt.Sprintf("🔁 Loop mode changed to: **%s**", nextMode.String()), true)
	case "btn_toggle_autoplay":
		enabled := p.ToggleAutoplay()
		if enabled {
			b.respondText(s, i, "📻 Autoplay **enabled**! Recommended tracks will play when the queue ends.", true)
		} else {
			b.respondText(s, i, "📻 Autoplay **disabled**.", true)
		}
	default:
		slog.Warn("Unhandled button click", "customID", customID)
	}
}

func (b *Bot) findUserVoiceChannel(s *discordgo.Session, guildID, userID string) string {
	g, err := s.State.Guild(guildID)
	if err != nil {
		g, err = s.Guild(guildID)
		if err != nil {
			return ""
		}
	}

	for _, vs := range g.VoiceStates {
		if vs.UserID == userID {
			return vs.ChannelID
		}
	}
	return ""
}

func (b *Bot) respondText(s *discordgo.Session, i *discordgo.InteractionCreate, content string, ephemeral bool) {
	var flags discordgo.MessageFlags
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   flags,
		},
	})
}

func (b *Bot) editDeferredResponse(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})
}

func (b *Bot) handleVoiceStateUpdate(s *discordgo.Session, vsu *discordgo.VoiceStateUpdate) {
	if vsu == nil || vsu.GuildID == "" {
		return
	}

	p := b.manager.Get(vsu.GuildID)
	if p == nil {
		return
	}

	botChID := p.VoiceChannelID()
	if botChID == "" {
		return
	}

	// 1. If this voice state update is for the bot itself
	if s.State.User != nil && vsu.UserID == s.State.User.ID {
		if vsu.ChannelID == "" {
			// Bot was disconnected from voice
			slog.Info("Bot was disconnected from voice channel", "guild", vsu.GuildID)
			p.Close()
			return
		}
		if vsu.ChannelID != botChID {
			// Bot was moved to another channel
			slog.Info("Bot moved to new voice channel", "guild", vsu.GuildID, "oldChannel", botChID, "newChannel", vsu.ChannelID)
			p.SetVoiceChannel(vsu.ChannelID)
			botChID = vsu.ChannelID
		}
	}

	// 2. Count active human users in the bot's current voice channel
	nonBotUsers := b.countNonBotUsersInChannel(vsu.GuildID, botChID)
	if nonBotUsers == 0 {
		p.StartEmptyTimer(b.cfg.EmptyChannelTimeout)
	} else if nonBotUsers > 0 {
		p.CancelEmptyTimer()
	}
}

func (b *Bot) countNonBotUsersInChannel(guildID, channelID string) int {
	g, err := b.session.State.Guild(guildID)
	if err != nil || g == nil {
		g, err = b.session.Guild(guildID)
		if err != nil || g == nil {
			return -1
		}
	}

	count := 0
	for _, vs := range g.VoiceStates {
		if vs.ChannelID != channelID {
			continue
		}
		if b.session.State.User != nil && vs.UserID == b.session.State.User.ID {
			continue
		}
		if vs.Member != nil && vs.Member.User != nil {
			b.botCache.Store(vs.UserID, vs.Member.User.Bot)
			if vs.Member.User.Bot {
				continue
			}
		} else if b.isUserBot(guildID, vs.UserID) {
			continue
		}
		count++
	}
	return count
}

func (b *Bot) isUserBot(guildID, userID string) bool {
	if val, ok := b.botCache.Load(userID); ok {
		return val.(bool)
	}

	m, err := b.session.State.Member(guildID, userID)
	if err == nil && m.User != nil {
		b.botCache.Store(userID, m.User.Bot)
		return m.User.Bot
	}

	m, err = b.session.GuildMember(guildID, userID)
	if err == nil && m.User != nil {
		b.botCache.Store(userID, m.User.Bot)
		return m.User.Bot
	}

	return false
}
