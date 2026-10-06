package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// URLResolver returns a FRESH direct media URL for the current track.
// It is expected to bypass any cached URL and re-resolve a new one (guest access only).
type URLResolver func(ctx context.Context) (string, error)

// RangeConfig configures a RangeReader.
type RangeConfig struct {
	// ChunkSize is the maximum size of a single CLOSED range window.
	// Default 512KiB, hard-capped at MaxChunkSize (1MiB).
	ChunkSize int64
	// MaxRetriesPerChunk is the number of recovery attempts for a single offset
	// before giving up (default 4).
	MaxRetriesPerChunk int
	// BackoffBase is the base delay for exponential backoff (default 3s).
	BackoffBase time.Duration
	// HTTPClient is the client used for range requests. Defaults to http.DefaultClient.
	HTTPClient *http.Client
	// UserAgent sent on each request.
	UserAgent string
}

// Default range reader settings.
const (
	DefaultChunkSize          int64         = 512 * 1024
	MaxChunkSize              int64         = 1 * 1024 * 1024
	DefaultMaxRetriesPerChunk int           = 4
	DefaultBackoffBase        time.Duration = 3 * time.Second
)

// Validate applies defaults and normalizes an invalid configuration.
// ChunkSize is defaulted when <= 0 and clamped to MaxChunkSize when too large.
// It returns an error only for a chunk size that is invalid and cannot be
// safely clamped (which currently cannot happen after clamping).
func (c *RangeConfig) Validate() error {
	if c.ChunkSize <= 0 {
		c.ChunkSize = DefaultChunkSize
	}
	if c.ChunkSize > MaxChunkSize {
		slog.Warn("rangereader: ChunkSize exceeds hard cap, clamping",
			"requested", c.ChunkSize, "cap", MaxChunkSize)
		c.ChunkSize = MaxChunkSize
	}
	if c.MaxRetriesPerChunk <= 0 {
		c.MaxRetriesPerChunk = DefaultMaxRetriesPerChunk
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = DefaultBackoffBase
	}
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
	if c.UserAgent == "" {
		c.UserAgent = "dc-music-bot/1.0"
	}
	return nil
}

// rangeReader is an io.ReadCloser that streams media from googlevideo-style
// URLs using sequential CLOSED Range windows only. It never issues an
// open-ended "bytes=0-" request (which googlevideo rejects with 403).
//
// On a mid-stream 403/416/429/5xx, a truncated body, or a transport error the
// reader closes the response, backs off exponentially, asks the resolver for a
// FRESH URL, and retries the SAME offset. Exhausting retries returns a fatal
// error including the last HTTP status. Context cancellation aborts promptly
// and is always fatal (no retries); a broken downstream pipe (ffmpeg killed via
// StreamSession.Stop) surfaces as context cancellation.
type rangeReader struct {
	ctx      context.Context
	resolver URLResolver
	cfg      RangeConfig

	url   string // current direct media URL (refreshed on retry)
	resp  *http.Response
	cur   io.Reader // current chunk body
	total int64     // total size, or -1 if unknown
	// offset is the absolute byte position of the next byte to be returned.
	offset int64
	// startOffset is the offset at which the current chunk began.
	startOffset int64
	// bytesInChunk counts bytes returned from the current chunk.
	bytesInChunk int64
	// retries counts recovery attempts for the current offset.
	retries int
	// lastStatus records the most recent HTTP status code.
	lastStatus int
	closed     bool
}

// NewRangeReader returns an io.ReadCloser that streams from the URL provided by
// resolver using HTTP Range requests. The context is used to abort promptly
// (e.g. when the downstream consumer stops); cancellation is always fatal.
func NewRangeReader(ctx context.Context, resolver URLResolver, cfg RangeConfig) io.ReadCloser {
	if ctx == nil {
		ctx = context.Background()
	}
	if resolver == nil {
		resolver = func(context.Context) (string, error) {
			return "", errors.New("rangeReader: nil resolver")
		}
	}
	_ = cfg.Validate()
	return &rangeReader{
		ctx:      ctx,
		resolver: resolver,
		cfg:      cfg,
		total:    -1,
	}
}

// Read implements io.Reader. It pulls the next chunk (or resumes the current
// one) as needed and returns bytes from googlevideo-style range responses.
func (r *rangeReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, errors.New("rangeReader: read on closed reader")
	}
	if err := r.ctx.Err(); err != nil {
		// Context cancelled (e.g. downstream stopped): abort without retry.
		return 0, err
	}

	for {
		if r.cur == nil {
			if err := r.openChunk(); err != nil {
				return 0, err
			}
			continue
		}

		n, err := r.cur.Read(p)
		if n > 0 {
			r.offset += int64(n)
			r.bytesInChunk += int64(n)
			return n, nil
		}

		// n == 0: finalize the current chunk body.
		r.closeCurrent()

		if err == io.EOF {
			// Fully consumed the current chunk body.
			if r.total < 0 {
				// Unknown total (e.g. server returned 200): this is the end.
				return 0, io.EOF
			}
			expected := r.expectedChunkBytes()
			if r.bytesInChunk < expected {
				// Truncated body: recovery is required.
				if !r.shouldRetry() {
					return 0, fmt.Errorf("rangeReader: truncated response at offset %d (got %d/%d bytes, last status %d)",
						r.offset, r.bytesInChunk, expected, r.lastStatus)
				}
				r.retryAtSameOffset()
				continue
			}
			// Chunk fully read.
			if r.offset >= r.total {
				return 0, io.EOF
			}
			// Advance to the next chunk; reset per-chunk retry budget.
			r.retries = 0
			continue
		}

		// Non-EOF error while reading the body.
		if r.ctx.Err() != nil {
			return 0, r.ctx.Err()
		}
		if !r.shouldRetry() {
			return 0, fmt.Errorf("rangeReader: body read error at offset %d (last status %d): %w",
				r.offset, r.lastStatus, err)
		}
		r.retryAtSameOffset()
	}
}

// Close implements io.Closer. It always closes any open response body and marks
// the reader closed so further reads fail fast.
func (r *rangeReader) Close() error {
	r.closed = true
	r.closeCurrent()
	return nil
}

// expectedChunkBytes returns the number of bytes the current chunk should
// contain, based on ChunkSize and the known total size.
func (r *rangeReader) expectedChunkBytes() int64 {
	window := r.cfg.ChunkSize
	if r.total >= 0 {
		rem := r.total - r.startOffset
		if rem < window {
			window = rem
		}
	}
	return window
}

// retryAtSameOffset drops the current body/URL so the next openChunk re-resolves
// a fresh URL and retries at the same offset.
func (r *rangeReader) retryAtSameOffset() {
	r.cur = nil
	r.bytesInChunk = 0
	r.url = "" // force a fresh URL on next openChunk
}

// closeCurrent closes the active response body, if any.
func (r *rangeReader) closeCurrent() {
	if r.resp != nil {
		_ = r.resp.Body.Close()
		r.resp = nil
	}
	// NOTE: r.bytesInChunk is intentionally NOT reset here. It records how many
	// bytes the current chunk actually delivered and is inspected by Read's
	// truncated-body check immediately after closeCurrent. It is reset by
	// openChunk (on success) and retryAtSameOffset (on recovery), which is the
	// correct point to start a fresh per-chunk count.
	r.cur = nil
}

// openChunk issues a single CLOSED range request for the current offset and
// prepares r.cur for reading. It performs its own bounded retry loop for
// transport errors and non-206/non-200 status codes, re-resolving a fresh URL
// on each attempt.
func (r *rangeReader) openChunk() error {
	if err := r.ctx.Err(); err != nil {
		return err
	}

	end := r.offset + r.cfg.ChunkSize - 1
	if r.total >= 0 {
		last := r.total - 1
		if end > last {
			end = last
		}
	}

	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}

		// Resolve a FRESH URL at the top of every retry iteration so that
		// recovery from a mid-stream failure always re-fetches a new URL.
		if r.url == "" {
			fresh, err := r.resolver(r.ctx)
			if err != nil {
				return fmt.Errorf("rangeReader: resolver failed: %w", err)
			}
			if fresh == "" {
				return errors.New("rangeReader: resolver returned empty URL")
			}
			r.url = fresh
		}

		req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
		if err != nil {
			return fmt.Errorf("rangeReader: failed to build request: %w", err)
		}
		req.Header.Set("User-Agent", r.cfg.UserAgent)
		// CLOSED range only. Never open-ended ("bytes=0-").
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.offset, end))

		resp, err := r.cfg.HTTPClient.Do(req)
		if err != nil {
			// Transport error: recoverable.
			if !r.shouldRetry() {
				return fmt.Errorf("rangeReader: transport error at offset %d: %w", r.offset, err)
			}
			r.url = "" // fresh URL on retry
			continue
		}
		r.lastStatus = resp.StatusCode

		if resp.StatusCode == http.StatusOK {
			// Server ignored the Range header; treat the body as the full stream.
			r.total = -1
			r.startOffset = r.offset
			r.bytesInChunk = 0
			r.resp = resp
			r.cur = resp.Body
			return nil
		}
		if resp.StatusCode != http.StatusPartialContent {
			// 403/416/429/5xx etc: recoverable via fresh URL + same offset.
			_ = resp.Body.Close()
			if !r.shouldRetry() {
				return fmt.Errorf("rangeReader: unexpected status %d at offset %d", resp.StatusCode, r.offset)
			}
			r.url = "" // fresh URL on retry
			continue
		}

		// 206 Partial Content.
		if total, perr := parseContentRangeTotal(resp.Header.Get("Content-Range")); perr == nil && total >= 0 {
			r.total = total
		}
		r.startOffset = r.offset
		r.bytesInChunk = 0
		r.resp = resp
		r.cur = resp.Body
		return nil
	}
}

// shouldRetry increments the retry budget and, if attempts remain, sleeps for
// an exponentially increasing backoff (respecting context cancellation).
// Returns false (no more retries) when the budget is exhausted or the context
// is cancelled.
func (r *rangeReader) shouldRetry() bool {
	if r.ctx.Err() != nil {
		return false
	}
	if r.retries >= r.cfg.MaxRetriesPerChunk {
		return false
	}
	r.retries++

	backoff := r.cfg.BackoffBase * time.Duration(1<<(uint(r.retries-1)))
	if backoff <= 0 {
		backoff = time.Millisecond
	}
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-timer.C:
		return r.ctx.Err() == nil
	case <-r.ctx.Done():
		return false
	}
}

// parseContentRangeTotal extracts the total size from a Content-Range header of
// the form "bytes a-b/total" or "bytes a-b/*". It returns (-1, nil) when the
// total is unknown ("*") and an error for malformed input.
func parseContentRangeTotal(h string) (int64, error) {
	if h == "" {
		return -1, errors.New("empty Content-Range")
	}
	if idx := strings.IndexByte(h, ' '); idx >= 0 {
		h = h[idx+1:]
	}
	slash := strings.IndexByte(h, '/')
	if slash < 0 {
		return -1, fmt.Errorf("malformed Content-Range %q", h)
	}
	totalStr := strings.TrimSpace(h[slash+1:])
	if totalStr == "*" || totalStr == "" {
		return -1, nil
	}
	total, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return -1, err
	}
	return total, nil
}
