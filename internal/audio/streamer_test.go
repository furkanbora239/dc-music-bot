package audio

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	dca "github.com/user/dc-music-bot/internal/dca"
)

// haveFFmpeg reports whether the ffmpeg binary is available on PATH.
func haveFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// generateTestAudio synthesizes a short webm clip with ffmpeg and returns its
// path. The test is skipped if ffmpeg is not available or fails to produce the
// file, so the suite stays green in environments without ffmpeg.
func generateTestAudio(t *testing.T) string {
	t.Helper()
	if !haveFFmpeg() {
		t.Skip("ffmpeg not on PATH; skipping encoder-dependent test")
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "test.webm")

	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-t", "2",
		"-f", "webm", "-c:a", "libopus",
		out,
	)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg failed to synthesize test audio: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Skipf("ffmpeg did not produce %s: %v", out, err)
	}
	return out
}

// TestEncoderProducesOpusFrames is an encoder-level proof (BUG 1 fix) that the
// vendored ffmpeg invocation actually emits real, non-empty Opus frames instead
// of dying on the removed "-vol" option. It runs the vendored encoder over a
// synthesized clip and asserts it yields >= 20 frames, each non-empty.
func TestEncoderProducesOpusFrames(t *testing.T) {
	path := generateTestAudio(t)

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open test audio: %v", err)
	}
	defer f.Close()

	opts := *dca.StdEncodeOptions
	opts.RawOutput = true
	opts.Bitrate = 96
	opts.Application = dca.AudioApplicationAudio

	enc, err := dca.EncodeMem(f, &opts)
	if err != nil {
		t.Fatalf("EncodeMem: %v", err)
	}
	defer enc.Cleanup()

	const minFrames = 20
	var frames int
	for {
		frame, err := enc.OpusFrame()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("OpusFrame: %v", err)
		}
		if len(frame) == 0 {
			t.Fatalf("empty opus frame at index %d", frames)
		}
		// Opus TOC byte (first byte) must have a valid configuration; a zero
		// TOC is invalid, so this is a cheap sanity check on frame integrity.
		if frame[0] == 0 {
			t.Fatalf("suspicious zero TOC byte in frame %d", frames)
		}
		frames++
	}

	if frames < minFrames {
		t.Fatalf("expected >= %d opus frames, got %d", minFrames, frames)
	}
	t.Logf("encoder produced %d non-empty opus frames", frames)
}

// TestVolumeMapping asserts the BUG 1 fix at the argument level: a default
// volume of 256 must NOT emit a volume filter (and must never emit the removed
// "-vol" option), while a non-default volume of 128 must emit "-af volume=0.500".
func TestVolumeMapping(t *testing.T) {
	// Default volume: no volume filter, and crucially no removed "-vol".
	optsDefault := *dca.StdEncodeOptions
	if optsDefault.Volume != 256 {
		t.Fatalf("unexpected default volume %d", optsDefault.Volume)
	}
	argsDefault := dca.EncodeArgs("pipe:0", &optsDefault)
	if containsArg(argsDefault, "-vol") {
		t.Fatalf("default volume still emits removed -vol option: %v", argsDefault)
	}
	if hasVolumeFilter(argsDefault) {
		t.Fatalf("default volume should not emit a volume filter: %v", argsDefault)
	}

	// Half volume (128): emit -af volume=0.500, and never -vol.
	optsHalf := *dca.StdEncodeOptions
	optsHalf.Volume = 128
	argsHalf := dca.EncodeArgs("pipe:0", &optsHalf)
	if containsArg(argsHalf, "-vol") {
		t.Fatalf("half volume emits removed -vol option: %v", argsHalf)
	}
	if !hasVolumeFilter(argsHalf) {
		t.Fatalf("half volume should emit a volume filter: %v", argsHalf)
	}
	if !containsArg(argsHalf, "volume=0.500") {
		t.Fatalf("half volume filter scale wrong, want volume=0.500 in %v", argsHalf)
	}

	// Negative control: full explicit AudioFilter must still be preserved and
	// merged AFTER the volume filter (comma separated, single -af).
	optsMixed := *dca.StdEncodeOptions
	optsMixed.Volume = 128
	optsMixed.AudioFilter = "highpass=f=200"
	argsMixed := dca.EncodeArgs("pipe:0", &optsMixed)
	if !containsArg(argsMixed, "volume=0.500,highpass=f=200") {
		t.Fatalf("mixed filter not merged correctly: %v", argsMixed)
	}
	t.Logf("default args: %v", argsDefault)
	t.Logf("half-volume args: %v", argsHalf)
}

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

// hasVolumeFilter reports whether the arg list contains a "-af" entry whose
// value includes an Opus volume filter.
func hasVolumeFilter(args []string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-af" && strings.Contains(args[i+1], "volume=") {
			return true
		}
	}
	return false
}

// newFakeVoiceConnection returns a VoiceConnection whose OpusSend channel is
// buffered and continuously drained so the streaming session never blocks, and
// whose Speaking() is a safe no-op (no websocket is attached).
func newFakeVoiceConnection() *discordgo.VoiceConnection {
	vc := &discordgo.VoiceConnection{
		OpusSend: make(chan []byte, 1<<16),
		// Speaking() locks Cond before checking wsConn; provide a valid Cond so
		// the no-op Speaking(true/false) calls in PlayStream don't nil-deref.
		Cond: sync.NewCond(&sync.Mutex{}),
	}
	go func() {
		for range vc.OpusSend {
		}
	}()
	return vc
}

// TestStreamSessionCompletionDeliveredOnce verifies BUG 2's fix: the completion
// error is delivered to Done() exactly once, Wait() maps io.EOF to nil, and
// Stop() is idempotent (calling it twice must not panic).
func TestStreamSessionCompletionDeliveredOnce(t *testing.T) {
	path := generateTestAudio(t)

	// Serve the clip via a local HTTP server that supports Range requests
	// (http.ServeContent handles them), so the existing range reader path is
	// exercised end-to-end through PlayStream.
	src, err := os.Open(path)
	if err != nil {
		t.Fatalf("open test audio: %v", err)
	}
	defer src.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "audio.webm", time.Time{}, src)
	}))
	defer srv.Close()

	resolver := func(ctx context.Context) (string, error) {
		return srv.URL, nil
	}

	vc := newFakeVoiceConnection()

	// --- Case A: Done() delivers the completion exactly once. ---
	sessA, err := PlayStream(context.Background(), resolver, vc, 100)
	if err != nil {
		t.Fatalf("PlayStream: %v", err)
	}

	type result struct {
		err error
		ok  bool
	}
	got := make(chan result, 1)
	go func() {
		r := result{}
		select {
		case r.err = <-sessA.Done():
			r.ok = true
		case <-time.After(15 * time.Second):
		}
		got <- r
	}()

	r := <-got
	if !r.ok {
		t.Fatalf("Done() never delivered a completion error (player would hang)")
	}
	// Exactly once: there must be no additional buffered value on Done().
	select {
	case extra := <-sessA.Done():
		t.Fatalf("Done() delivered a second value (double-consumer bug remains): %v", extra)
	default:
	}

	// Stop must be idempotent: calling it twice must not panic.
	sessA.Stop()
	sessA.Stop()

	// --- Case B: Wait() returns nil on normal completion (io.EOF -> nil). ---
	sessB, err := PlayStream(context.Background(), resolver, vc, 100)
	if err != nil {
		t.Fatalf("PlayStream: %v", err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- sessB.Wait() }()
	select {
	case we := <-waitErr:
		if we != nil {
			t.Fatalf("Wait() expected nil on normal completion, got %v", we)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("Wait() blocked (player hang bug)")
	}
	sessB.Stop()
	sessB.Stop()
	t.Logf("completion delivered exactly once; Stop idempotent")
}
