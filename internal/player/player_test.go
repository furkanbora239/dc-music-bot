package player

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestStreamErrorHint verifies that the user-facing bot-wall hint is only
// appended for YouTube "sign in to confirm" errors (matched
// case-insensitively) and otherwise stays empty.
func TestStreamErrorHint(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantSub string // "" means the hint must be empty
	}{
		{
			name:    "nil error yields no hint",
			err:     nil,
			wantSub: "",
		},
		{
			name:    "http 500 yields no hint",
			err:     errors.New("stream returned HTTP 500"),
			wantSub: "",
		},
		{
			name:    "yt-dlp bot wall yields hint",
			err:     errors.New("ERROR: Sign in to confirm you're not a bot. Use --cookies"),
			wantSub: "YTDLP_COOKIES_FILE",
		},
		{
			name:    "bot wall is matched case-insensitively",
			err:     errors.New("SIGN IN TO CONFIRM you're not a bot"),
			wantSub: "YTDLP_COOKIES_FILE",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := streamErrorHint(tc.err)
			if tc.wantSub == "" {
				if got != "" {
					t.Errorf("expected empty hint, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("hint %q does not contain %q", got, tc.wantSub)
			}
		})
	}
}

func TestPlayerAutoplayToggle(t *testing.T) {
	p := &Player{}
	if p.Autoplay() {
		t.Fatal("expected default autoplay false")
	}

	p.SetAutoplay(true)
	if !p.Autoplay() {
		t.Fatal("expected autoplay true after SetAutoplay(true)")
	}

	res := p.ToggleAutoplay()
	if res || p.Autoplay() {
		t.Fatal("expected autoplay false after ToggleAutoplay()")
	}

	res = p.ToggleAutoplay()
	if !res || !p.Autoplay() {
		t.Fatal("expected autoplay true after ToggleAutoplay()")
	}
}

func TestPlayerHistory(t *testing.T) {
	p := &Player{}
	p.recordHistory("id1")
	p.recordHistory("id2")

	if !p.isInHistory("id1") {
		t.Fatal("expected id1 in history")
	}
	if !p.isInHistory("id2") {
		t.Fatal("expected id2 in history")
	}
	if p.isInHistory("id3") {
		t.Fatal("id3 should not be in history")
	}

	// Test max history cap
	for i := 0; i < 60; i++ {
		p.recordHistory(string(rune('a' + (i % 26))))
	}
	p.mu.RLock()
	histLen := len(p.playedHistory)
	p.mu.RUnlock()
	if histLen > 50 {
		t.Fatalf("expected max 50 items in history, got %d", histLen)
	}
}

func TestEmptyTimer_Cancel(t *testing.T) {
	p := &Player{
		guildID: "g1",
	}

	p.StartEmptyTimer(1 * time.Second)
	p.mu.RLock()
	hasTimer := p.emptyTimer != nil
	p.mu.RUnlock()
	if !hasTimer {
		t.Fatal("expected emptyTimer to be active")
	}

	// Calling StartEmptyTimer again shouldn't crash or replace
	p.StartEmptyTimer(1 * time.Second)

	p.CancelEmptyTimer()
	p.mu.RLock()
	hasTimer = p.emptyTimer != nil
	p.mu.RUnlock()
	if hasTimer {
		t.Fatal("expected emptyTimer to be nil after Cancel")
	}
}

func TestEmptyTimer_Expires(t *testing.T) {
	var disconnected atomic.Bool
	p := &Player{
		guildID: "g1",
		disconnectFn: func(g string) {
			disconnected.Store(true)
		},
	}

	p.StartEmptyTimer(20 * time.Millisecond)

	time.Sleep(50 * time.Millisecond)

	p.mu.RLock()
	isClosed := p.closed
	p.mu.RUnlock()

	if !isClosed {
		t.Fatal("expected player to be closed after empty timer expired")
	}
	if !disconnected.Load() {
		t.Fatal("expected disconnectFn to be called after empty timer expired")
	}
}
