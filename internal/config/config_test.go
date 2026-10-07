package config

import (
	"os"
	"testing"
	"time"
)

func TestConfigLoadRequiredToken(t *testing.T) {
	_ = os.Unsetenv("DISCORD_TOKEN")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when DISCORD_TOKEN is missing, got nil")
	}
}

func TestConfigLoadDefaults(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "test-token-123")
	_ = os.Unsetenv("AUDIO_SOURCE_MODE")
	_ = os.Unsetenv("DEFAULT_VOLUME")
	_ = os.Unsetenv("IDLE_TIMEOUT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DiscordToken != "test-token-123" {
		t.Errorf("expected token 'test-token-123', got '%s'", cfg.DiscordToken)
	}
	if cfg.AudioSourceMode != "hybrid" {
		t.Errorf("expected default source mode 'hybrid', got '%s'", cfg.AudioSourceMode)
	}
	if cfg.DefaultVolume != 100 {
		t.Errorf("expected default volume 100, got %d", cfg.DefaultVolume)
	}
	if cfg.IdleTimeout != 3*time.Minute {
		t.Errorf("expected default idle timeout 3m, got %v", cfg.IdleTimeout)
	}
	if cfg.DefaultAutoplay != false {
		t.Errorf("expected default autoplay false, got %v", cfg.DefaultAutoplay)
	}
}

func TestConfigLoadAutoplay(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "tok")
	t.Setenv("DEFAULT_AUTOPLAY", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.DefaultAutoplay {
		t.Errorf("expected autoplay true, got %v", cfg.DefaultAutoplay)
	}
}

func TestConfigLoadCustom(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "custom-token")
	t.Setenv("AUDIO_SOURCE_MODE", "ytdlp")
	t.Setenv("PIPED_API_URL", "https://custom.piped.instance/")
	t.Setenv("DEFAULT_VOLUME", "75")
	t.Setenv("IDLE_TIMEOUT", "5m")
	_ = os.Unsetenv("YTDLP_COOKIES_FILE")
	_ = os.Unsetenv("YTDLP_COOKIES_FROM_BROWSER")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.AudioSourceMode != "ytdlp" {
		t.Errorf("expected source mode 'ytdlp', got '%s'", cfg.AudioSourceMode)
	}
	if cfg.PipedAPIURL != "https://custom.piped.instance" {
		t.Errorf("expected trailing slash removed from piped url, got '%s'", cfg.PipedAPIURL)
	}
	if cfg.DefaultVolume != 75 {
		t.Errorf("expected volume 75, got %d", cfg.DefaultVolume)
	}
	if cfg.IdleTimeout != 5*time.Minute {
		t.Errorf("expected idle timeout 5m, got %v", cfg.IdleTimeout)
	}
}

// Guest mode (default): no cookie options configured => both fields empty.
func TestConfigCookiesDefaultGuest(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "tok")
	_ = os.Unsetenv("YTDLP_COOKIES_FILE")
	_ = os.Unsetenv("YTDLP_COOKIES_FROM_BROWSER")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.YtDlpCookiesFile != "" {
		t.Errorf("expected empty cookies file in guest mode, got %q", cfg.YtDlpCookiesFile)
	}
	if cfg.YtDlpCookiesFromBrowser != "" {
		t.Errorf("expected empty cookies browser in guest mode, got %q", cfg.YtDlpCookiesFromBrowser)
	}
}

// A configured, existing cookies file is passed through verbatim.
func TestConfigCookiesFileValid(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "tok")
	_ = os.Unsetenv("YTDLP_COOKIES_FROM_BROWSER")

	dir := t.TempDir()
	path := dir + "/cookies.txt"
	if err := os.WriteFile(path, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatalf("failed to write temp cookies file: %v", err)
	}
	t.Setenv("YTDLP_COOKIES_FILE", "  "+path+"  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.YtDlpCookiesFile != path {
		t.Errorf("expected cookies file %q (trimmed), got %q", path, cfg.YtDlpCookiesFile)
	}
}

// A missing cookies file must not crash: warn and continue as guest (empty).
func TestConfigCookiesFileMissingContinuesGuest(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "tok")
	_ = os.Unsetenv("YTDLP_COOKIES_FROM_BROWSER")
	t.Setenv("YTDLP_COOKIES_FILE", "/no/such/dir/cookies.txt")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error when cookies file missing, got %v", err)
	}
	if cfg.YtDlpCookiesFile != "" {
		t.Errorf("expected cookies file cleared to guest when missing, got %q", cfg.YtDlpCookiesFile)
	}
}

// Browser-only mode is passed through verbatim.
func TestConfigCookiesFromBrowser(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "tok")
	_ = os.Unsetenv("YTDLP_COOKIES_FILE")
	t.Setenv("YTDLP_COOKIES_FROM_BROWSER", "firefox")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.YtDlpCookiesFromBrowser != "firefox" {
		t.Errorf("expected browser 'firefox', got %q", cfg.YtDlpCookiesFromBrowser)
	}
}

// Precedence: when both are set (with a usable file), the file wins and the
// browser option is cleared.
func TestConfigCookiesBothPreferFile(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "tok")

	dir := t.TempDir()
	path := dir + "/cookies.txt"
	if err := os.WriteFile(path, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatalf("failed to write temp cookies file: %v", err)
	}
	t.Setenv("YTDLP_COOKIES_FILE", path)
	t.Setenv("YTDLP_COOKIES_FROM_BROWSER", "chrome")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.YtDlpCookiesFile != path {
		t.Errorf("expected cookies file %q to win, got %q", path, cfg.YtDlpCookiesFile)
	}
	if cfg.YtDlpCookiesFromBrowser != "" {
		t.Errorf("expected browser cleared by precedence, got %q", cfg.YtDlpCookiesFromBrowser)
	}
}
