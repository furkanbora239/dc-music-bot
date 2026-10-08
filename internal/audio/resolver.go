package audio

import (
	"context"
	"fmt"
	"log/slog"
)

// Resolver manages audio sources and implements fallback strategies.
type Resolver struct {
	mode  string
	piped *PipedSource
	ytdlp *YtDlpSource
}

// NewResolver constructs an audio source resolver based on configuration.
// cookiesFile and cookiesFromBrowser are the optional yt-dlp cookie rescue
// options (empty => guest mode); they are threaded through to the yt-dlp
// source. The Piped source is unaffected.
func NewResolver(mode, pipedURL, cookiesFile, cookiesFromBrowser string) *Resolver {
	return &Resolver{
		mode:  mode,
		piped: NewPipedSource(pipedURL),
		ytdlp: NewYtDlpSource("yt-dlp", cookiesFile, cookiesFromBrowser),
	}
}

// Search searches for tracks using the configured source strategy.
func (r *Resolver) Search(ctx context.Context, query string) ([]*Track, error) {
	switch r.mode {
	case "piped":
		return r.piped.Search(ctx, query)
	case "ytdlp":
		return r.ytdlp.Search(ctx, query)
	default: // "hybrid"
		tracks, err := r.ytdlp.Search(ctx, query)
		if err == nil && len(tracks) > 0 {
			return tracks, nil
		}
		slog.Warn("yt-dlp search failed, falling back to Piped", "error", err, "query", query)
		return r.piped.Search(ctx, query)
	}
}

// joinStreamErrors composes the user-visible error returned when both the
// yt-dlp and Piped sources fail to resolve a stream URL in hybrid mode. It
// preserves BOTH underlying errors (via two %w verbs) so the root cause from
// either source is visible when the error is formatted with %v or Error().
func joinStreamErrors(ytdlpErr, pipedErr error) error {
	return fmt.Errorf("failed to resolve audio stream from both yt-dlp and piped (yt-dlp: %w; piped: %w)", ytdlpErr, pipedErr)
}

// ResolveStreamURL ensures the track has a valid playable stream URL.
func (r *Resolver) ResolveStreamURL(ctx context.Context, track *Track) (string, error) {
	if track.StreamURL != "" {
		return track.StreamURL, nil
	}

	switch r.mode {
	case "piped":
		return r.piped.GetStreamURL(ctx, track)
	case "ytdlp":
		return r.ytdlp.GetStreamURL(ctx, track)
	default: // "hybrid"
		// In hybrid mode, yt-dlp is locally run and fast (~2s) with zero proxy/500 errors.
		streamURL, ytdlpErr := r.ytdlp.GetStreamURL(ctx, track)
		if ytdlpErr == nil && streamURL != "" {
			return streamURL, nil
		}
		slog.Warn("yt-dlp stream resolution failed, attempting Piped stream fallback", "error", ytdlpErr, "track", track.Title)
		streamURL, pipedErr := r.piped.GetStreamURL(ctx, track)
		if pipedErr != nil {
			return "", joinStreamErrors(ytdlpErr, pipedErr)
		}
		return streamURL, nil
	}
}

// GetRecommendations retrieves recommended tracks based on a seed track.
func (r *Resolver) GetRecommendations(ctx context.Context, seed *Track, limit int) ([]*Track, error) {
	switch r.mode {
	case "piped":
		return r.piped.GetRecommendations(ctx, seed, limit)
	case "ytdlp":
		return r.ytdlp.GetRecommendations(ctx, seed, limit)
	default: // "hybrid"
		tracks, err := r.ytdlp.GetRecommendations(ctx, seed, limit)
		if err == nil && len(tracks) > 0 {
			return tracks, nil
		}
		slog.Warn("yt-dlp recommendations failed, attempting Piped fallback", "error", err, "track", seed.Title)
		return r.piped.GetRecommendations(ctx, seed, limit)
	}
}

