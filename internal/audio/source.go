package audio

import (
	"context"
	"fmt"
	"time"
)

// Track represents a playable audio track.
type Track struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Artist      string        `json:"artist"`
	Duration    time.Duration `json:"duration"`
	StreamURL   string        `json:"stream_url"`
	WebpageURL  string        `json:"webpage_url"`
	Thumbnail   string        `json:"thumbnail"`
	RequestedBy string        `json:"requested_by"`
}

// FormatDuration returns a friendly mm:ss or hh:mm:ss representation of the track duration.
func (t *Track) FormatDuration() string {
	if t.Duration <= 0 {
		return "Live / Unknown"
	}
	hours := int(t.Duration.Hours())
	mins := int(t.Duration.Minutes()) % 60
	secs := int(t.Duration.Seconds()) % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

// Source defines the contract for audio metadata and stream extractors.
type Source interface {
	Name() string
	Search(ctx context.Context, query string) ([]*Track, error)
	GetTrack(ctx context.Context, idOrURL string) (*Track, error)
	GetStreamURL(ctx context.Context, track *Track) (string, error)
}
