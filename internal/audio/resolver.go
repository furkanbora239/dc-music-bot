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
		tracks, err := r.piped.Search(ctx, query)
		if err == nil && len(tracks) > 0 {
			return tracks, nil
		}
		slog.Warn("Piped search failed, falling back to yt-dlp", "error", err, "query", query)
		return r.ytdlp.Search(ctx, query)
	}
}

// GetTrack retrieves track metadata by ID or URL.
func (r *Resolver) GetTrack(ctx context.Context, idOrURL string) (*Track, error) {
	switch r.mode {
	case "piped":
		return r.piped.GetTrack(ctx, idOrURL)
	case "ytdlp":
		return r.ytdlp.GetTrack(ctx, idOrURL)
	default: // "hybrid"
		track, err := r.piped.GetTrack(ctx, idOrURL)
		if err == nil {
			return track, nil
		}
		slog.Warn("Piped GetTrack failed, falling back to yt-dlp", "error", err, "idOrURL", idOrURL)
		return r.ytdlp.GetTrack(ctx, idOrURL)
	}
}

// joinStreamErrors composes the user-visible error returned when both the
// yt-dlp and Piped sources fail to resolve a stream URL in hybrid mode. It
// preserves BOTH underlying errors (via two %w verbs) so the root cause from
// either source is visible when the error is formatted with %v or Error().
// fresh distinguishes the ResolveStreamURLFresh wording from ResolveStreamURL.
func joinStreamErrors(fresh bool, ytdlpErr, pipedErr error) error {
	if fresh {
		return fmt.Errorf("failed to resolve fresh audio stream from both yt-dlp and piped (yt-dlp: %w; piped: %w)", ytdlpErr, pipedErr)
	}
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
			return "", joinStreamErrors(false, ytdlpErr, pipedErr)
		}
		return streamURL, nil
	}
}

// ResolveStreamURLFresh forces a fresh resolution of the track's stream URL,
// bypassing (and invalidating) any cached track.StreamURL. It is used by the
// range reader to obtain a brand new direct media URL when a mid-stream request
// fails (the SimpMusic/Media3 re-resolve recovery pattern). Guest (no-login)
// access remains the default; optional yt-dlp cookie options are applied by
// the underlying source only when configured.
func (r *Resolver) ResolveStreamURLFresh(ctx context.Context, track *Track) (string, error) {
	// Drop any cached URL so the underlying source re-resolves a fresh one.
	track.StreamURL = ""

	switch r.mode {
	case "piped":
		return r.piped.GetStreamURL(ctx, track)
	case "ytdlp":
		return r.ytdlp.GetStreamURL(ctx, track)
	default: // "hybrid"
		streamURL, ytdlpErr := r.ytdlp.GetStreamURL(ctx, track)
		if ytdlpErr == nil && streamURL != "" {
			return streamURL, nil
		}
		slog.Warn("yt-dlp fresh stream resolution failed, attempting Piped fallback", "error", ytdlpErr, "track", track.Title)
		streamURL, pipedErr := r.piped.GetStreamURL(ctx, track)
		if pipedErr != nil {
			return "", joinStreamErrors(true, ytdlpErr, pipedErr)
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

