package audio

import (
	"errors"
	"strings"
	"testing"
)

// TestJoinStreamErrors verifies that the composed hybrid-mode error surfaces
// BOTH the yt-dlp and Piped underlying error texts, so the user-visible
// message is no longer hiding the yt-dlp root cause.
func TestJoinStreamErrors(t *testing.T) {
	ytdlpErr := errors.New("ERROR: Sign in to confirm you're not a bot")
	pipedErr := errors.New("piped stream returned HTTP 500")

	err := joinStreamErrors(ytdlpErr, pipedErr)
	if err == nil {
		t.Fatal("expected a non-nil composed error")
	}
	msg := err.Error()

	wants := []string{
		"failed to resolve audio stream from both yt-dlp and piped",
		ytdlpErr.Error(),
		pipedErr.Error(),
	}
	for _, want := range wants {
		if !strings.Contains(msg, want) {
			t.Errorf("composed error %q does not contain %q", msg, want)
		}
	}
}
