package audio

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// closedRangeRE matches a proper CLOSED range header: "bytes=a-b" with both
// ends present (never open-ended "bytes=0-").
var closedRangeRE = regexp.MustCompile(`^bytes=\d+-\d+$`)

// parseClosedRange extracts the inclusive [a,b] window from a "bytes=a-b" header.
func parseClosedRange(t *testing.T, rng string) (int64, int64) {
	t.Helper()
	if !closedRangeRE.MatchString(rng) {
		t.Fatalf("range header not a closed range: %q", rng)
	}
	body := strings.TrimPrefix(rng, "bytes=")
	dash := strings.IndexByte(body, '-')
	a, err := strconv.ParseInt(body[:dash], 10, 64)
	if err != nil {
		t.Fatalf("bad start in range %q: %v", rng, err)
	}
	b, err := strconv.ParseInt(body[dash+1:], 10, 64)
	if err != nil {
		t.Fatalf("bad end in range %q: %v", rng, err)
	}
	return a, b
}

// serveChunk writes bytes [a,b] of payload as a 206 with a correct Content-Range.
func serveChunk(w http.ResponseWriter, payload []byte, a, b int64) {
	if b >= int64(len(payload)) {
		b = int64(len(payload)) - 1
	}
	if a > b {
		a = b + 1
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, len(payload)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(payload[a : b+1])
}

// TestRangeReaderRejectsFullGetButServesClosedRanges verifies that the reader
// always issues CLOSED range requests (never an open-ended/absent Range) and
// still reassembles the complete byte stream even though the server rejects
// full GETs with 403.
func TestRangeReaderRejectsFullGetButServesClosedRanges(t *testing.T) {
	payload := make([]byte, 20*1024)
	rng := rand.New(rand.NewSource(1))
	_, _ = rng.Read(payload)

	sawNonClosed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rngHdr := r.Header.Get("Range")
		if !closedRangeRE.MatchString(rngHdr) {
			// Simulate googlevideo: full GET / open-ended range -> 403.
			sawNonClosed = true
			w.WriteHeader(http.StatusForbidden)
			return
		}
		a, b := parseClosedRange(t, rngHdr)
		serveChunk(w, payload, a, b)
	}))
	defer srv.Close()

	cfg := RangeConfig{
		ChunkSize:          1024,
		MaxRetriesPerChunk: 4,
		BackoffBase:         time.Millisecond,
		HTTPClient:         srv.Client(),
	}
	resolver := func(context.Context) (string, error) { return srv.URL, nil }

	reader := NewRangeReader(context.Background(), resolver, cfg)
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if sawNonClosed {
		t.Fatalf("reader sent a non-closed (full/open-ended) Range request")
	}
	if len(got) != len(payload) {
		t.Fatalf("reassembled length = %d, want %d", len(got), len(payload))
	}
	if string(got) != string(payload) {
		t.Fatalf("reassembled bytes mismatch")
	}
}

// TestRangeReaderSequentialMultiChunk verifies that sequential multi-chunk
// reads (with ChunkSize smaller than the payload) reassemble the exact bytes.
func TestRangeReaderSequentialMultiChunk(t *testing.T) {
	payload := make([]byte, 5*1024)
	rng := rand.New(rand.NewSource(2))
	_, _ = rng.Read(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rngHdr := r.Header.Get("Range")
		if !closedRangeRE.MatchString(rngHdr) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		a, b := parseClosedRange(t, rngHdr)
		serveChunk(w, payload, a, b)
	}))
	defer srv.Close()

	cfg := RangeConfig{
		ChunkSize:          1024, // smaller than the 5KiB payload -> multiple chunks
		MaxRetriesPerChunk: 4,
		BackoffBase:        time.Millisecond,
		HTTPClient:         srv.Client(),
	}
	resolver := func(context.Context) (string, error) { return srv.URL, nil }

	reader := NewRangeReader(context.Background(), resolver, cfg)
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(got) != len(payload) {
		t.Fatalf("reassembled length = %d, want %d", len(got), len(payload))
	}
	if string(got) != string(payload) {
		t.Fatalf("reassembled bytes mismatch")
	}
}

// TestRangeReaderMidStreamFailureRecovery is the key regression test for bug (A):
// the first URL serves chunk 0, then 403s at the next offset. The resolver must
// be called AGAIN and return a second URL that serves from the same offset, and
// the final stream must equal the original bytes.
func TestRangeReaderMidStreamFailureRecovery(t *testing.T) {
	payload := make([]byte, 5*1024)
	rng := rand.New(rand.NewSource(3))
	_, _ = rng.Read(payload)

	const chunk = int64(1024)
	url1 := "/url1"
	url2 := "/url2"

	var resolveCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rngHdr := r.Header.Get("Range")
		if !closedRangeRE.MatchString(rngHdr) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		a, b := parseClosedRange(t, rngHdr)
		switch r.URL.Path {
		case url1:
			// Serves only the very first chunk, then 403s on any later offset.
			if a >= chunk {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			serveChunk(w, payload, a, b)
		case url2:
			// Fully serves everything.
			serveChunk(w, payload, a, b)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	resolver := func(context.Context) (string, error) {
		resolveCalls++
		if resolveCalls == 1 {
			return srv.URL + url1, nil
		}
		return srv.URL + url2, nil
	}

	cfg := RangeConfig{
		ChunkSize:          chunk,
		MaxRetriesPerChunk: 4,
		BackoffBase:        time.Millisecond,
		HTTPClient:         srv.Client(),
	}

	reader := NewRangeReader(context.Background(), resolver, cfg)
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if resolveCalls < 2 {
		t.Fatalf("resolver was called %d times, expected >= 2 (must re-resolve after mid-stream 403)", resolveCalls)
	}
	if len(got) != len(payload) {
		t.Fatalf("reassembled length = %d, want %d", len(got), len(payload))
	}
	if string(got) != string(payload) {
		t.Fatalf("reassembled bytes mismatch after mid-stream recovery")
	}
}

// TestRangeReaderTruncatedBodyRecovery verifies that a truncated body (fewer
// bytes than Content-Range advertises) triggers a re-resolve + same-offset
// retry and still yields the correct bytes.
func TestRangeReaderTruncatedBodyRecovery(t *testing.T) {
	payload := make([]byte, 5*1024)
	rng := rand.New(rand.NewSource(4))
	_, _ = rng.Read(payload)

	const chunk = int64(1024)
	var truncCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rngHdr := r.Header.Get("Range")
		if !closedRangeRE.MatchString(rngHdr) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		a, b := parseClosedRange(t, rngHdr)
		truncCalls++
		if truncCalls == 1 {
			// Advertise the full window but send only half the bytes.
			half := (b - a + 1) / 2
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[a : a+half])
			return
		}
		// Subsequent attempts serve in full.
		serveChunk(w, payload, a, b)
	}))
	defer srv.Close()

	cfg := RangeConfig{
		ChunkSize:          chunk,
		MaxRetriesPerChunk: 4,
		BackoffBase:        time.Millisecond,
		HTTPClient:         srv.Client(),
	}
	resolver := func(context.Context) (string, error) { return srv.URL, nil }

	reader := NewRangeReader(context.Background(), resolver, cfg)
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(got) != len(payload) {
		t.Fatalf("reassembled length = %d, want %d", len(got), len(payload))
	}
	if string(got) != string(payload) {
		t.Fatalf("reassembled bytes mismatch after truncated-body recovery")
	}
}

// TestRangeReaderRetryExhaustion verifies that when the server always 403s, the
// reader burns its retry budget and returns a non-nil error mentioning the
// status code.
func TestRangeReaderRetryExhaustion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := RangeConfig{
		ChunkSize:          1024,
		MaxRetriesPerChunk: 3,
		BackoffBase:        time.Millisecond,
		HTTPClient:         srv.Client(),
	}
	resolver := func(context.Context) (string, error) { return srv.URL, nil }

	reader := NewRangeReader(context.Background(), resolver, cfg)
	defer reader.Close()

	buf := make([]byte, 64)
	_, err := reader.Read(buf)
	if err == nil {
		t.Fatalf("expected error from exhausted retries, got nil")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(http.StatusForbidden)) {
		t.Fatalf("error %q does not mention status %d", err.Error(), http.StatusForbidden)
	}
}

// TestRangeReaderContextCancellation verifies that a cancelled context causes a
// prompt (non-hanging) Read failure, and that Read after Close errors fast.
func TestRangeReaderContextCancellation(t *testing.T) {
	// A server that would otherwise hang: only responds after a long delay.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusForbidden)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	cfg := RangeConfig{
		ChunkSize:          1024,
		MaxRetriesPerChunk: 4,
		BackoffBase:        time.Millisecond,
		HTTPClient:         srv.Client(),
	}
	resolver := func(context.Context) (string, error) { return srv.URL, nil }

	// (1) Read with a cancelled context returns promptly.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	r1 := NewRangeReader(cancelledCtx, resolver, cfg)

	start := time.Now()
	buf := make([]byte, 64)
	_, err := r1.Read(buf)
	if err == nil {
		t.Fatalf("expected cancelled-context error, got nil")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Read did not return promptly on cancelled context: %v", time.Since(start))
	}

	// (2) Read after Close errors fast.
	r2 := NewRangeReader(context.Background(), resolver, cfg)
	if err := r2.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	_, err = r2.Read(buf)
	if err == nil {
		t.Fatalf("expected error reading after Close, got nil")
	}
}

// TestParseContentRangeTotal covers the unit cases for parseContentRangeTotal.
func TestParseContentRangeTotal(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    int64
		wantErr bool
	}{
		{"bytes a-b/total", "bytes 0-1023/5120", 5120, false},
		{"bytes a-b/*", "bytes 0-1023/*", -1, false},
		{"with leading unit space", "bytes 100-199/200", 200, false},
		{"malformed no slash", "bytes 0-1023", 0, true},
		{"empty", "", 0, true},
		{"malformed total", "bytes 0-1023/abc", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseContentRangeTotal(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil (total=%d)", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("parseContentRangeTotal(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
