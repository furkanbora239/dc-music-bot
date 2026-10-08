package bot

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/user/dc-music-bot/internal/audio"
	"github.com/user/dc-music-bot/internal/config"
	"github.com/user/dc-music-bot/internal/player"
)

// Bot represents the music bot instance.
type Bot struct {
	cfg      *config.Config
	session  *discordgo.Session
	resolver *audio.Resolver
	manager  *player.Manager
	botCache sync.Map
}

// NewBot constructs a new Bot instance.
func NewBot(cfg *config.Config) (*Bot, error) {
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates
	session.LogLevel = discordgo.LogWarning
	if cfg.LogLevel == "debug" {
		session.LogLevel = discordgo.LogDebug
	}

	resolver := audio.NewResolver(cfg.AudioSourceMode, cfg.PipedAPIURL, cfg.YtDlpCookiesFile, cfg.YtDlpCookiesFromBrowser)
	manager := player.NewManager()

	b := &Bot{
		cfg:      cfg,
		session:  session,
		resolver: resolver,
		manager:  manager,
	}

	session.AddHandler(b.handleInteraction)
	session.AddHandler(b.handleVoiceStateUpdate)

	return b, nil
}

// Start opens the Discord connection and registers Slash Commands.
func (b *Bot) Start() error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord gateway session: %w", err)
	}

	appID := b.cfg.DiscordAppID
	if appID == "" && b.session.State.User != nil {
		appID = b.session.State.User.ID
	}

	slog.Info("Connected to Discord",
		"username", b.session.State.User.Username,
		"discriminator", b.session.State.User.Discriminator,
		"appID", appID,
	)

	// Register Slash Commands
	slog.Info("Registering application slash commands...")
	_, err := b.session.ApplicationCommandBulkOverwrite(appID, b.cfg.GuildID, Commands)
	if err != nil {
		return fmt.Errorf("failed to bulk overwrite slash commands: %w", err)
	}

	slog.Info("Slash commands registered successfully",
		"command_count", len(Commands),
		"scope", map[bool]string{true: "guild", false: "global"}[b.cfg.GuildID != ""],
	)

	return nil
}

// Stop gracefully shuts down active streams, players, and the Discord connection.
func (b *Bot) Stop() {
	slog.Info("Shutting down music bot...")
	b.manager.Shutdown()
	if err := b.session.Close(); err != nil {
		slog.Warn("Error closing discord session", "error", err)
	}
	slog.Info("Bot shutdown complete.")
}
