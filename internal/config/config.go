package config

import (
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds the application configuration parameters.
type Config struct {
	DiscordToken    string
	DiscordAppID    string
	GuildID         string
	AudioSourceMode string
	PipedAPIURL     string
	DefaultVolume   int
	IdleTimeout         time.Duration
	EmptyChannelTimeout time.Duration
	LogLevel            string
	DefaultAutoplay     bool

	// Optional yt-dlp cookie rescue options for YouTube's "Sign in to confirm
	// you're not a bot" wall on some egress IPs. Both are OFF (empty) by
	// default, in which case extraction runs in guest (no-login) mode exactly
	// as before.
	//
	// YTDLP_COOKIES_FILE: path to a Netscape-format cookies.txt.
	// YTDLP_COOKIES_FROM_BROWSER: browser name for --cookies-from-browser.
	// If both are set, the file takes precedence.
	YtDlpCookiesFile        string
	YtDlpCookiesFromBrowser string
}

// Load loads configuration from environment variables and optionally a .env file.
func Load() (*Config, error) {
	// Try loading .env if it exists, ignore error if missing
	_ = godotenv.Load()

	token := strings.TrimSpace(os.Getenv("DISCORD_TOKEN"))
	if token == "" {
		return nil, errors.New("DISCORD_TOKEN environment variable is required")
	}

	appID := strings.TrimSpace(os.Getenv("DISCORD_APP_ID"))
	guildID := strings.TrimSpace(os.Getenv("DISCORD_GUILD_ID"))

	sourceMode := strings.ToLower(strings.TrimSpace(os.Getenv("AUDIO_SOURCE_MODE")))
	if sourceMode == "" {
		sourceMode = "hybrid"
	}

	pipedURL := strings.TrimSpace(os.Getenv("PIPED_API_URL"))
	if pipedURL == "" {
		pipedURL = "https://api.piped.private.coffee"
	}
	pipedURL = strings.TrimRight(pipedURL, "/")

	volume := 100
	if volStr := os.Getenv("DEFAULT_VOLUME"); volStr != "" {
		if v, err := strconv.Atoi(volStr); err == nil && v >= 1 && v <= 100 {
			volume = v
		}
	}

	idleTimeout := 3 * time.Minute
	if timeoutStr := os.Getenv("IDLE_TIMEOUT"); timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil && d > 0 {
			idleTimeout = d
		}
	}

	emptyChannelTimeout := 1 * time.Minute
	if timeoutStr := os.Getenv("EMPTY_CHANNEL_TIMEOUT"); timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil && d >= 0 {
			emptyChannelTimeout = d
		}
	}

	logLevel := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL")))
	if logLevel == "" {
		logLevel = "info"
	}

	defaultAutoplay := false
	if apVal := strings.ToLower(strings.TrimSpace(os.Getenv("DEFAULT_AUTOPLAY"))); apVal == "true" || apVal == "1" || apVal == "yes" || apVal == "on" {
		defaultAutoplay = true
	}

	// Optional yt-dlp cookie rescue options. Empty (the default) keeps the bot
	// in guest (no-login) mode with exactly the same yt-dlp arguments as before.
	cookiesFile := strings.TrimSpace(os.Getenv("YTDLP_COOKIES_FILE"))
	cookiesFromBrowser := strings.TrimSpace(os.Getenv("YTDLP_COOKIES_FROM_BROWSER"))

	// A configured cookie file that is missing/unreadable must not crash the
	// bot: warn and drop it so we continue as guest (or fall back to a
	// configured browser, if any).
	if cookiesFile != "" {
		if st, err := os.Stat(cookiesFile); err != nil {
			slog.Warn("YTDLP_COOKIES_FILE is set but the file was not found; continuing as guest without it",
				"path", cookiesFile, "error", err)
			cookiesFile = ""
		} else if st.IsDir() {
			slog.Warn("YTDLP_COOKIES_FILE is set but points to a directory; continuing as guest without it",
				"path", cookiesFile)
			cookiesFile = ""
		}
	}

	// When both are configured, the explicit cookies.txt wins.
	if cookiesFile != "" && cookiesFromBrowser != "" {
		slog.Info("Both YTDLP_COOKIES_FILE and YTDLP_COOKIES_FROM_BROWSER are set; YTDLP_COOKIES_FILE takes precedence",
			"cookies_file", cookiesFile,
			"ignored_browser", cookiesFromBrowser)
		cookiesFromBrowser = ""
	}

	return &Config{
		DiscordToken:            token,
		DiscordAppID:            appID,
		GuildID:                 guildID,
		AudioSourceMode:         sourceMode,
		PipedAPIURL:             pipedURL,
		DefaultVolume:           volume,
		IdleTimeout:             idleTimeout,
		EmptyChannelTimeout:     emptyChannelTimeout,
		LogLevel:                logLevel,
		DefaultAutoplay:         defaultAutoplay,
		YtDlpCookiesFile:        cookiesFile,
		YtDlpCookiesFromBrowser: cookiesFromBrowser,
	}, nil
}
