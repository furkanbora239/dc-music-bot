package audio

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// The fixed base flag slices mirror exactly what Search and GetStreamURL pass
// today, so the guest-mode expectations are literal snapshots of the current
// command line.
var (
	searchBase = []string{
		"--dump-json",
		"--flat-playlist",
		"--no-playlist",
		"--skip-download",
		"--no-warnings",
	}
	streamBase = []string{
		"-g",
		"-f", "ba/b",
		"--no-playlist",
		"--no-warnings",
	}
)

func TestBuildArgsGuestIdenticalToLegacy(t *testing.T) {
	y := NewYtDlpSource(YtDlpOptions{BinaryPath: "yt-dlp"})

	got := y.buildArgs(searchBase, "ytsearch1:test")
	want := []string{
		"--source-address", "0.0.0.0", "--js-runtimes", "node",
		"--dump-json",
		"--flat-playlist",
		"--no-playlist",
		"--skip-download",
		"--no-warnings",
		"ytsearch1:test",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("guest search args changed:\n got %q\nwant %q", got, want)
	}

	got = y.buildArgs(streamBase, "https://youtu.be/dQw4w9WgXcQ")
	want = []string{
		"--source-address", "0.0.0.0", "--js-runtimes", "node",
		"-g",
		"-f", "ba/b",
		"--no-playlist",
		"--no-warnings",
		"https://youtu.be/dQw4w9WgXcQ",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("guest stream args changed:\n got %q\nwant %q", got, want)
	}
}

func TestBuildArgsCookiesFile(t *testing.T) {
	y := NewYtDlpSource(YtDlpOptions{BinaryPath: "yt-dlp", CookiesFile: "/secrets/cookies.txt"})

	got := y.buildArgs(streamBase, "target")
	want := []string{
		"--source-address", "0.0.0.0", "--js-runtimes", "node",
		"-g",
		"-f", "ba/b",
		"--no-playlist",
		"--no-warnings",
		"--cookies", "/secrets/cookies.txt",
		"target",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("file-mode args:\n got %q\nwant %q", got, want)
	}
}

func TestBuildArgsCookiesFromBrowser(t *testing.T) {
	y := NewYtDlpSource(YtDlpOptions{BinaryPath: "yt-dlp", CookiesFromBrowser: "firefox"})

	got := y.buildArgs(searchBase, "target")
	want := []string{
		"--source-address", "0.0.0.0", "--js-runtimes", "node",
		"--dump-json",
		"--flat-playlist",
		"--no-playlist",
		"--skip-download",
		"--no-warnings",
		"--cookies-from-browser", "firefox",
		"target",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("browser-mode args:\n got %q\nwant %q", got, want)
	}
}

func TestBuildArgsBothSetFileWins(t *testing.T) {
	// Constructed directly with both options: the builder itself must prefer
	// the cookies file even before config-level precedence is applied.
	y := NewYtDlpSource(YtDlpOptions{BinaryPath: "yt-dlp", CookiesFile: "/secrets/cookies.txt", CookiesFromBrowser: "chrome"})

	got := y.buildArgs(streamBase, "target")
	want := []string{
		"--source-address", "0.0.0.0", "--js-runtimes", "node",
		"-g",
		"-f", "ba/b",
		"--no-playlist",
		"--no-warnings",
		"--cookies", "/secrets/cookies.txt",
		"target",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("both-set args:\n got %q\nwant %q", got, want)
	}
	for _, a := range got {
		if a == "--cookies-from-browser" {
			t.Fatalf("browser flag leaked into args when cookies file is set: %q", got)
		}
	}
}

func TestBuildArgsProxy(t *testing.T) {
	y := NewYtDlpSource(YtDlpOptions{BinaryPath: "yt-dlp", Proxy: "socks5://100.64.0.5:1080"})

	got := y.buildArgs(streamBase, "target")
	want := []string{
		"--source-address", "0.0.0.0", "--js-runtimes", "node",
		"--proxy", "socks5://100.64.0.5:1080",
		"-g",
		"-f", "ba/b",
		"--no-playlist",
		"--no-warnings",
		"target",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("proxy args:\n got %q\nwant %q", got, want)
	}
}

func TestBuildArgsPOTProvider(t *testing.T) {
	y := NewYtDlpSource(YtDlpOptions{BinaryPath: "yt-dlp", PotBaseURL: "http://bgutil-provider:4416"})

	got := y.buildArgs(streamBase, "target")
	want := []string{
		"--source-address", "0.0.0.0", "--js-runtimes", "node",
		"--extractor-args", "youtubepot-bgutilhttp:base_url=http://bgutil-provider:4416",
		"-g",
		"-f", "ba/b",
		"--no-playlist",
		"--no-warnings",
		"target",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pot args:\n got %q\nwant %q", got, want)
	}
}

func TestBuildArgsPlayerClientsWithPOT(t *testing.T) {
	y := NewYtDlpSource(YtDlpOptions{
		BinaryPath:    "yt-dlp",
		PotBaseURL:    "http://bgutil-provider:4416",
		PlayerClients: "mweb,tv,web_safari",
	})

	got := y.buildArgs(streamBase, "target")
	want := []string{
		"--source-address", "0.0.0.0", "--js-runtimes", "node",
		"--extractor-args", "youtubepot-bgutilhttp:base_url=http://bgutil-provider:4416",
		"--extractor-args", "youtube:player_client=mweb,tv,web_safari",
		"-g",
		"-f", "ba/b",
		"--no-playlist",
		"--no-warnings",
		"target",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("player clients args:\n got %q\nwant %q", got, want)
	}
}

func TestNewYtDlpSourceTrimsAndDefaultsBinary(t *testing.T) {
	y := NewYtDlpSource(YtDlpOptions{CookiesFile: " /tmp/c.txt ", CookiesFromBrowser: "  safari ", PotBaseURL: "  http://host:4416/  "})
	if y.binaryPath != "yt-dlp" {
		t.Errorf("expected default binary 'yt-dlp', got %q", y.binaryPath)
	}
	if y.cookiesFile != "/tmp/c.txt" {
		t.Errorf("expected trimmed cookies file, got %q", y.cookiesFile)
	}
	if y.potBaseURL != "http://host:4416" {
		t.Errorf("expected trailing slash + spaces trimmed, got %q", y.potBaseURL)
	}
	if y.cookiesFromBrowser != "safari" {
		t.Errorf("expected trimmed browser, got %q", y.cookiesFromBrowser)
	}
}

func TestYtDlpGetRecommendationsLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live test in short mode")
	}
	y := NewYtDlpSource(YtDlpOptions{BinaryPath: "yt-dlp"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	seed := &Track{
		ID:         "kJQP7kiw5Fk",
		Title:      "Despacito",
		Artist:     "Luis Fonsi",
		WebpageURL: "https://www.youtube.com/watch?v=kJQP7kiw5Fk",
	}

	recs, err := y.GetRecommendations(ctx, seed, 3)
	if err != nil {
		t.Fatalf("unexpected error fetching recommendations: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("expected at least 1 recommendation, got 0")
	}
	for i, r := range recs {
		if r.ID == "" || r.ID == seed.ID {
			t.Errorf("rec[%d] invalid or equals seed ID: %q", i, r.ID)
		}
		if r.Title == "" {
			t.Errorf("rec[%d] empty title", i)
		}
		t.Logf("Rec #%d: [%s] %s (%s)", i+1, r.ID, r.Title, r.Artist)
	}
}
