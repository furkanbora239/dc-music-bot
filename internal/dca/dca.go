// Package dca is a trimmed, vendored copy of github.com/jonas747/dca
// (commit 155f5e5f0cc7, MIT licensed — see LICENSE in this directory).
//
// It has been vendored in order to fix Bug 1: the upstream library builds
// ffmpeg argument lists containing "-vol <int>", an option that was REMOVED
// in ffmpeg 7+/8.x. With ffmpeg 8.x this caused ffmpeg to exit instantly with
// "Unrecognized option 'vol'", producing zero frames and total silence.
//
// The fix (see encode.go) drops "-vol" and instead, only when the requested
// volume differs from the default (256 == 1.0), appends
//   "-af", "volume=<scale>"
// where scale = Volume/256.0.
//
// decode.go has been intentionally omitted (it is unused by dc-music-bot).
package dca

import (
	"log"
	"time"
)

// Define constants
const (
	// The current version of the DCA format
	FormatVersion int8 = 1

	// The current version of the DCA program
	LibraryVersion string = "0.0.5"

	// The URL to the GitHub repository of DCA
	GitHubRepositoryURL string = "https://github.com/jonas747/dca"
)

// OpusReader is implemented by both EncodeSession and DecodeSession and is the
// interface consumed by a StreamingSession.
type OpusReader interface {
	OpusFrame() (frame []byte, err error)
	FrameDuration() time.Duration
}

// Logger allows redirecting dca's internal log output.
var Logger *log.Logger

// logln logs to assigned logger or standard logger
func logln(s ...interface{}) {
	if Logger != nil {
		Logger.Println(s...)
		return
	}

	log.Println(s...)
}
