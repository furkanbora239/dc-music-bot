package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/user/dc-music-bot/internal/bot"
	"github.com/user/dc-music-bot/internal/config"
)

func main() {
	// Configure structured logger
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(handler))

	slog.Info("Starting dc-music-bot...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Configuration error", "error", err)
		os.Exit(1)
	}

	if cfg.LogLevel == "debug" {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		slog.SetDefault(slog.New(handler))
	}

	slog.Info("Configuration loaded successfully",
		"default_volume", cfg.DefaultVolume,
		"default_autoplay", cfg.DefaultAutoplay,
		"idle_timeout", cfg.IdleTimeout,
		"empty_channel_timeout", cfg.EmptyChannelTimeout,
		"ytdlp_cookies_file", cfg.YtDlpCookiesFile,
		"ytdlp_cookies_from_browser", cfg.YtDlpCookiesFromBrowser,
	)

	// Initialize bot
	musicBot, err := bot.NewBot(cfg)
	if err != nil {
		slog.Error("Failed to initialize bot", "error", err)
		os.Exit(1)
	}

	// Start bot
	if err := musicBot.Start(); err != nil {
		slog.Error("Failed to start bot", "error", err)
		os.Exit(1)
	}

	slog.Info("dc-music-bot is running. Press CTRL+C to terminate.")

	// Await termination signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Signal received, performing graceful shutdown...")
	musicBot.Stop()
}
