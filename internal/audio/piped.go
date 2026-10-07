package audio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PipedSource implements Source using the Piped REST API.
type PipedSource struct {
	baseURL    string
	httpClient *http.Client
}

// NewPipedSource creates a new Piped API source client.
func NewPipedSource(baseURL string) *PipedSource {
	return &PipedSource{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *PipedSource) Name() string {
	return "piped"
}

type pipedSearchResponse struct {
	Items []pipedSearchItem `json:"items"`
	Error string            `json:"error"`
}

type pipedSearchItem struct {
	URL          string `json:"url"` // format: "/watch?v=xyz"
	Type         string `json:"type"`
	Title        string `json:"title"`
	Thumbnail    string `json:"thumbnail"`
	UploaderName string `json:"uploaderName"`
	Duration     int64  `json:"duration"` // duration in seconds
}

type pipedStreamResponse struct {
	Title          string             `json:"title"`
	Duration       int64              `json:"duration"`
	ThumbnailURL   string             `json:"thumbnailUrl"`
	Uploader       string             `json:"uploader"`
	AudioStreams   []pipedAudioStream `json:"audioStreams"`
	RelatedStreams []pipedSearchItem  `json:"relatedStreams"`
	Error          string             `json:"error"`
}

type pipedAudioStream struct {
	URL      string `json:"url"`
	Format   string `json:"format"`
	Quality  string `json:"quality"`
	MimeType string `json:"mimeType"`
	Codec    string `json:"codec"`
	Bitrate  int    `json:"bitrate"`
}

// Search searches YouTube Music / Videos via the Piped API.
func (p *PipedSource) Search(ctx context.Context, query string) ([]*Track, error) {
	searchURL := fmt.Sprintf("%s/search?q=%s&filter=music_songs", p.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}
	req.Header.Set("User-Agent", "dc-music-bot/1.0")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("piped search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("piped search returned HTTP %d", resp.StatusCode)
	}

	var result pipedSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search json: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("piped api error: %s", result.Error)
	}

	var tracks []*Track
	for _, item := range result.Items {
		if item.Type != "stream" {
			continue
		}
		videoID := extractVideoID(item.URL)
		if videoID == "" {
			continue
		}

		tracks = append(tracks, &Track{
			ID:         videoID,
			Title:      item.Title,
			Artist:     item.UploaderName,
			Duration:   time.Duration(item.Duration) * time.Second,
			WebpageURL: fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID),
			Thumbnail:  item.Thumbnail,
		})
	}

	if len(tracks) == 0 {
		return nil, errors.New("no tracks found on piped")
	}

	return tracks, nil
}

// GetTrack retrieves track metadata from Piped by video ID or URL.
func (p *PipedSource) GetTrack(ctx context.Context, idOrURL string) (*Track, error) {
	videoID := extractVideoID(idOrURL)
	if videoID == "" {
		return nil, errors.New("invalid youtube video id or url")
	}

	streamURL := fmt.Sprintf("%s/streams/%s", p.baseURL, videoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream request: %w", err)
	}
	req.Header.Set("User-Agent", "dc-music-bot/1.0")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("piped stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("piped stream returned HTTP %d", resp.StatusCode)
	}

	var result pipedStreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode stream json: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("piped api stream error: %s", result.Error)
	}

	track := &Track{
		ID:         videoID,
		Title:      result.Title,
		Artist:     result.Uploader,
		Duration:   time.Duration(result.Duration) * time.Second,
		WebpageURL: fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID),
		Thumbnail:  result.ThumbnailURL,
	}

	directURL := selectBestAudioStream(result.AudioStreams)
	if directURL != "" {
		track.StreamURL = directURL
	}

	return track, nil
}

// GetStreamURL fetches the direct audio stream URL for a given track.
func (p *PipedSource) GetStreamURL(ctx context.Context, track *Track) (string, error) {
	if track.StreamURL != "" {
		return track.StreamURL, nil
	}
	t, err := p.GetTrack(ctx, track.ID)
	if err != nil {
		return "", err
	}
	if t.StreamURL == "" {
		return "", errors.New("no audio streams found in piped response")
	}
	track.StreamURL = t.StreamURL
	return t.StreamURL, nil
}

// GetRecommendations retrieves related tracks from Piped's /streams/{id} endpoint.
func (p *PipedSource) GetRecommendations(ctx context.Context, seed *Track, limit int) ([]*Track, error) {
	if limit <= 0 {
		limit = 10
	}

	videoID := seed.ID
	if videoID == "" && seed.WebpageURL != "" {
		videoID = extractVideoID(seed.WebpageURL)
	}
	if videoID == "" {
		return nil, errors.New("invalid youtube video id for recommendations")
	}

	streamURL := fmt.Sprintf("%s/streams/%s", p.baseURL, videoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create recommendations request: %w", err)
	}
	req.Header.Set("User-Agent", "dc-music-bot/1.0")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("piped recommendations request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("piped stream returned HTTP %d", resp.StatusCode)
	}

	var result pipedStreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode piped stream json: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("piped api stream error: %s", result.Error)
	}

	var tracks []*Track
	for _, item := range result.RelatedStreams {
		if item.Type != "stream" {
			continue
		}
		relID := extractVideoID(item.URL)
		if relID == "" || relID == videoID {
			continue
		}

		tracks = append(tracks, &Track{
			ID:         relID,
			Title:      item.Title,
			Artist:     item.UploaderName,
			Duration:   time.Duration(item.Duration) * time.Second,
			WebpageURL: fmt.Sprintf("https://www.youtube.com/watch?v=%s", relID),
			Thumbnail:  item.Thumbnail,
		})

		if len(tracks) >= limit {
			break
		}
	}

	if len(tracks) == 0 {
		return nil, errors.New("no recommendations found in piped response")
	}

	return tracks, nil
}

func selectBestAudioStream(streams []pipedAudioStream) string {
	if len(streams) == 0 {
		return ""
	}
	var best pipedAudioStream
	for _, s := range streams {
		if strings.Contains(s.MimeType, "audio/webm") || strings.Contains(s.Codec, "opus") {
			if s.Bitrate > best.Bitrate {
				best = s
			}
		}
	}
	if best.URL != "" {
		return best.URL
	}
	// Fallback to highest bitrate of any audio stream
	for _, s := range streams {
		if s.Bitrate > best.Bitrate {
			best = s
		}
	}
	if best.URL != "" {
		return best.URL
	}
	return streams[0].URL
}

func extractVideoID(input string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "/watch?v=") {
		return strings.TrimPrefix(input, "/watch?v=")
	}
	if strings.Contains(input, "youtu.be/") {
		parts := strings.Split(input, "youtu.be/")
		if len(parts) > 1 {
			id := strings.Split(parts[1], "?")[0]
			return strings.Trim(id, "/")
		}
	}
	if strings.Contains(input, "youtube.com/watch") {
		u, err := url.Parse(input)
		if err == nil {
			return u.Query().Get("v")
		}
	}
	// Direct video ID
	if len(input) == 11 && !strings.Contains(input, "/") && !strings.Contains(input, " ") {
		return input
	}
	return input
}
