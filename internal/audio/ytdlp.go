package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// YtDlpSource implements Source using the local yt-dlp binary.
type YtDlpSource struct {
	binaryPath         string
	cookiesFile        string
	cookiesFromBrowser string
}

// NewYtDlpSource creates a new yt-dlp source. cookiesFile and
// cookiesFromBrowser are the optional yt-dlp cookie rescue options (see
// config.YTDLP_COOKIES_FILE / YTDLP_COOKIES_FROM_BROWSER); both empty keeps
// extraction in guest (no-login) mode. If both are provided, cookiesFile wins.
func NewYtDlpSource(binaryPath, cookiesFile, cookiesFromBrowser string) *YtDlpSource {
	if binaryPath == "" {
		binaryPath = "yt-dlp"
	}
	return &YtDlpSource{
		binaryPath:         binaryPath,
		cookiesFile:        strings.TrimSpace(cookiesFile),
		cookiesFromBrowser: strings.TrimSpace(cookiesFromBrowser),
	}
}

// buildArgs assembles the full yt-dlp argument list for a command whose fixed
// flags are given in base, inserting the configured cookie options (if any)
// just before the final target. When no cookie options are set it returns
// exactly base followed by target, so guest-mode args are identical to before.
// The cookies file always takes precedence over the browser option.
func (y *YtDlpSource) buildArgs(base []string, target string) []string {
	args := make([]string, 0, len(base)+3)
	args = append(args, base...)
	switch {
	case y.cookiesFile != "":
		args = append(args, "--cookies", y.cookiesFile)
	case y.cookiesFromBrowser != "":
		args = append(args, "--cookies-from-browser", y.cookiesFromBrowser)
	}
	args = append(args, target)
	return args
}

func (y *YtDlpSource) Name() string {
	return "yt-dlp"
}

type ytdlpThumbnail struct {
	URL string `json:"url"`
}

type ytdlpJSON struct {
	ID         string           `json:"id"`
	Title      string           `json:"title"`
	Uploader   string           `json:"uploader"`
	Channel    string           `json:"channel"`
	Duration   float64          `json:"duration"`
	WebpageURL string           `json:"webpage_url"`
	Thumbnail  string           `json:"thumbnail"`
	Thumbnails []ytdlpThumbnail `json:"thumbnails"`
	URL        string           `json:"url"` // direct stream url if extracted
}

// Search searches for tracks using yt-dlp's built-in search.
func (y *YtDlpSource) Search(ctx context.Context, query string) ([]*Track, error) {
	searchTerm := query
	if !strings.HasPrefix(query, "http://") && !strings.HasPrefix(query, "https://") {
		searchTerm = fmt.Sprintf("ytsearch1:%s", query)
	}

	isPlaylist := strings.Contains(query, "list=") || strings.Contains(query, "playlist")

	base := []string{
		"--js-runtimes", "node",
		"--dump-json",
		"--skip-download",
		"--no-warnings",
	}

	if isPlaylist {
		base = append(base, "--flat-playlist", "--playlist-end", "100")
	} else {
		base = append(base, "--no-playlist")
	}

	cmd := exec.CommandContext(ctx, y.binaryPath, y.buildArgs(base, searchTerm)...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp search error (%w): %s", err, stderr.String())
	}

	var tracks []*Track
	decoder := json.NewDecoder(&stdout)
	for decoder.More() {
		var item ytdlpJSON
		if err := decoder.Decode(&item); err != nil {
			break
		}
		artist := item.Uploader
		if artist == "" {
			artist = item.Channel
		}
		webURL := item.WebpageURL
		if webURL == "" {
			webURL = fmt.Sprintf("https://www.youtube.com/watch?v=%s", item.ID)
		}

		thumb := item.Thumbnail
		if thumb == "" && len(item.Thumbnails) > 0 {
			thumb = item.Thumbnails[len(item.Thumbnails)-1].URL
		}

		tracks = append(tracks, &Track{
			ID:         item.ID,
			Title:      item.Title,
			Artist:     artist,
			Duration:   time.Duration(item.Duration * float64(time.Second)),
			WebpageURL: webURL,
			Thumbnail:  thumb,
			StreamURL:  item.URL,
		})
	}

	if len(tracks) == 0 {
		return nil, errors.New("no tracks found by yt-dlp")
	}

	return tracks, nil
}

// GetTrack retrieves metadata for a track or URL using yt-dlp.
func (y *YtDlpSource) GetTrack(ctx context.Context, idOrURL string) (*Track, error) {
	target := idOrURL
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = fmt.Sprintf("https://www.youtube.com/watch?v=%s", idOrURL)
	}

	tracks, err := y.Search(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, errors.New("track not found")
	}
	return tracks[0], nil
}

// GetStreamURL resolves the direct audio stream URL using yt-dlp -g -f ba/b.
func (y *YtDlpSource) GetStreamURL(ctx context.Context, track *Track) (string, error) {
	if track.StreamURL != "" {
		return track.StreamURL, nil
	}

	target := track.WebpageURL
	if target == "" {
		target = fmt.Sprintf("https://www.youtube.com/watch?v=%s", track.ID)
	}

	base := []string{
		"--js-runtimes", "node",
		"-g",
		"-f", "ba/b",
		"--no-playlist",
		"--no-warnings",
	}

	cmd := exec.CommandContext(ctx, y.binaryPath, y.buildArgs(base, target)...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp stream resolution failed (%w): %s", err, stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", errors.New("yt-dlp returned empty stream url")
	}

	directURL := lines[0]
	track.StreamURL = directURL
	return directURL, nil
}

// GetRecommendations retrieves recommended tracks based on a seed track.
// It prioritizes YouTube's radio mix playlist (list=RD<video_id>) using flat playlist extraction.
func (y *YtDlpSource) GetRecommendations(ctx context.Context, seed *Track, limit int) ([]*Track, error) {
	if limit <= 0 {
		limit = 10
	}

	videoID := seed.ID
	if videoID == "" && seed.WebpageURL != "" {
		videoID = extractVideoID(seed.WebpageURL)
	}

	var targets []string
	if videoID != "" {
		targets = append(targets, fmt.Sprintf("https://www.youtube.com/watch?v=%s&list=RD%s", videoID, videoID))
	}
	if seed.Title != "" {
		query := seed.Title
		if seed.Artist != "" {
			query = fmt.Sprintf("%s - %s", seed.Artist, seed.Title)
		}
		targets = append(targets, fmt.Sprintf("ytsearch%d:%s", limit+2, query))
	}

	if len(targets) == 0 {
		return nil, errors.New("cannot get recommendations without track ID or title")
	}

	var lastErr error
	for _, target := range targets {
		base := []string{
			"--js-runtimes", "node",
			"--dump-json",
			"--flat-playlist",
			"--playlist-end", strconv.Itoa(limit + 2),
			"--skip-download",
			"--no-warnings",
		}

		cmd := exec.CommandContext(ctx, y.binaryPath, y.buildArgs(base, target)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			lastErr = fmt.Errorf("yt-dlp recommendations error (%w): %s", err, stderr.String())
			continue
		}

		var tracks []*Track
		decoder := json.NewDecoder(&stdout)
		for decoder.More() {
			var item ytdlpJSON
			if err := decoder.Decode(&item); err != nil {
				break
			}
			if item.ID == "" || (videoID != "" && item.ID == videoID) {
				continue
			}

			artist := item.Uploader
			if artist == "" {
				artist = item.Channel
			}
			webURL := item.WebpageURL
			if webURL == "" && item.ID != "" {
				webURL = fmt.Sprintf("https://www.youtube.com/watch?v=%s", item.ID)
			}

			thumb := item.Thumbnail
			if thumb == "" && len(item.Thumbnails) > 0 {
				thumb = item.Thumbnails[len(item.Thumbnails)-1].URL
			}

			tracks = append(tracks, &Track{
				ID:         item.ID,
				Title:      item.Title,
				Artist:     artist,
				Duration:   time.Duration(item.Duration * float64(time.Second)),
				WebpageURL: webURL,
				Thumbnail:  thumb,
				StreamURL:  item.URL,
			})

			if len(tracks) >= limit {
				break
			}
		}

		if len(tracks) > 0 {
			return tracks, nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("no recommendations found by yt-dlp")
}

