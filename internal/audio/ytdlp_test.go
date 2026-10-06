package audio

import (
	"reflect"
	"testing"
)

// The fixed base flag slices mirror exactly what Search and GetStreamURL pass
// today, so the guest-mode expectations are literal snapshots of the current
// command line.
var (
	searchBase = []string{
		"--js-runtimes", "node",
		"--dump-json",
		"--no-playlist",
		"--skip-download",
		"--no-warnings",
	}
	streamBase = []string{
		"--js-runtimes", "node",
		"-g",
		"-f", "ba/b",
		"--no-playlist",
		"--no-warnings",
	}
)

func TestBuildArgsGuestIdenticalToLegacy(t *testing.T) {
	y := NewYtDlpSource("yt-dlp", "", "")

	got := y.buildArgs(searchBase, "ytsearch1:test")
	want := []string{
		"--js-runtimes", "node",
		"--dump-json",
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
		"--js-runtimes", "node",
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
	y := NewYtDlpSource("yt-dlp", "/secrets/cookies.txt", "")

	got := y.buildArgs(streamBase, "target")
	want := []string{
		"--js-runtimes", "node",
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
	y := NewYtDlpSource("yt-dlp", "", "firefox")

	got := y.buildArgs(searchBase, "target")
	want := []string{
		"--js-runtimes", "node",
		"--dump-json",
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
	y := NewYtDlpSource("yt-dlp", "/secrets/cookies.txt", "chrome")

	got := y.buildArgs(streamBase, "target")
	want := []string{
		"--js-runtimes", "node",
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

func TestNewYtDlpSourceTrimsAndDefaultsBinary(t *testing.T) {
	y := NewYtDlpSource("", " /tmp/c.txt ", "  safari ")
	if y.binaryPath != "yt-dlp" {
		t.Errorf("expected default binary 'yt-dlp', got %q", y.binaryPath)
	}
	if y.cookiesFile != "/tmp/c.txt" {
		t.Errorf("expected trimmed cookies file, got %q", y.cookiesFile)
	}
	if y.cookiesFromBrowser != "safari" {
		t.Errorf("expected trimmed browser, got %q", y.cookiesFromBrowser)
	}
}
