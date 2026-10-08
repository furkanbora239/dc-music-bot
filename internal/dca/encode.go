package dca

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jonas747/ogg"
)

// AudioApplication is an application profile for opus encoding
type AudioApplication string

var (
	// AudioApplicationVoip favors improved speech intelligibility.
	AudioApplicationVoip AudioApplication = "voip"
	// AudioApplicationAudio favors faithfulness to the input.
	AudioApplicationAudio AudioApplication = "audio"
	// AudioApplicationLowDelay restricts to only the lowest delay modes.
	AudioApplicationLowDelay AudioApplication = "lowdelay"
)

var (
	// ErrBadFrame is returned when a frame is malformed.
	ErrBadFrame = errors.New("bad frame")
)

// EncodeOptions is a set of options for encoding dca
type EncodeOptions struct {
	Volume           int              // change audio volume (256=normal)
	Channels         int              // audio channels
	FrameRate        int              // audio sampling rate (ex 48000)
	FrameDuration    int              // audio frame duration can be 20, 40, or 60 (ms)
	Bitrate          int              // audio encoding bitrate in kb/s can be 8 - 128
	PacketLoss       int              // expected packet loss percentage
	RawOutput        bool             // Raw opus output (no metadata or magic bytes)
	Application      AudioApplication // Audio application
	CompressionLevel int              // Compression level, higher is better qualiy but slower encoding (0 - 10)
	BufferedFrames   int              // How big the frame buffer should be
	VBR              bool             // Wether vbr is used or not (variable bitrate)
	StartTime        int              // Start Time of the input stream in seconds

	// The ffmpeg audio filters to use, see https://ffmpeg.org/ffmpeg-filters.html#Audio-Filters for more info
	// Leave empty to use no filters.
	AudioFilter string
}

// EncodeArgs builds the ffmpeg argument list used to transcode the input file
// (or "pipe:0" for stdin) into raw Ogg Opus packets on stdout.
//
// BUG 1 FIX: upstream dca emitted "-vol <int>" here. ffmpeg 7+/8.x REMOVED the
// "-vol" option, so ffmpeg exited instantly with "Unrecognized option 'vol'"
// and produced zero frames (total silence). We instead scale volume with the
// modern "-af volume=<scale>" audio filter, where scale = Volume/256.0
// (256 == 1.0, i.e. unchanged). The filter is only emitted when the volume
// differs from the default, keeping the arg list as close to upstream as
// possible. If an explicit AudioFilter is also set, the volume filter is
// prepended so both apply (ffmpeg accepts a single -af with comma-separated
// filters).
func EncodeArgs(inFile string, o *EncodeOptions) []string {
	vbrStr := "on"
	if !o.VBR {
		vbrStr = "off"
	}

	var args []string
	if strings.HasPrefix(inFile, "http://") || strings.HasPrefix(inFile, "https://") {
		args = append(args,
			"-reconnect", "1",
			"-reconnect_at_eof", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "5",
		)
	}

	args = append(args,
		"-nostats",
		"-loglevel", "error",
		"-i", inFile,
		"-map", "0:a",
		"-acodec", "libopus",
		"-f", "ogg",
		"-vbr", vbrStr,
		"-compression_level", strconv.Itoa(o.CompressionLevel),
		"-ar", strconv.Itoa(o.FrameRate),
		"-ac", strconv.Itoa(o.Channels),
		"-b:a", strconv.Itoa(o.Bitrate*1000),
		"-application", string(o.Application),
		"-frame_duration", strconv.Itoa(o.FrameDuration),
		"-packet_loss", strconv.Itoa(o.PacketLoss),
		"-threads", "1",
		"-ss", strconv.Itoa(o.StartTime),
	)

	af := o.AudioFilter
	if o.Volume != 256 {
		vol := fmt.Sprintf("volume=%.3f", float64(o.Volume)/256.0)
		if af != "" {
			af = vol + "," + af
		} else {
			af = vol
		}
	}
	if af != "" {
		args = append(args, "-af", af)
	}

	args = append(args, "pipe:1")
	return args
}

// validate returns an error if the options are not correct
func (opts *EncodeOptions) validate() error {
	if opts.Volume < 0 || opts.Volume > 512 {
		return errors.New("out of bounds volume (0-512)")
	}

	if opts.FrameDuration != 20 && opts.FrameDuration != 40 && opts.FrameDuration != 60 {
		return errors.New("invalid FrameDuration")
	}

	if opts.PacketLoss < 0 || opts.PacketLoss > 100 {
		return errors.New("invalid packet loss percentage")
	}

	if opts.Application != AudioApplicationAudio && opts.Application != AudioApplicationVoip && opts.Application != AudioApplicationLowDelay {
		return errors.New("invalid audio application")
	}

	if opts.CompressionLevel < 0 || opts.CompressionLevel > 10 {
		return errors.New("compression level out of bounds (0-10)")
	}

	return nil
}

// StdEncodeOptions is the standard options for encoding
var StdEncodeOptions = &EncodeOptions{
	Volume:           256,
	Channels:         2,
	FrameRate:        48000,
	FrameDuration:    20,
	Bitrate:          64,
	Application:      AudioApplicationAudio,
	CompressionLevel: 5,
	PacketLoss:       1,
	BufferedFrames:   100, // At 20ms frames that's 2s
	VBR:              true,
	StartTime:        0,
}

// Frame is a single frame read from the encoder. metaData frames only appear
// when EncodeOptions.RawOutput is false; dc-music-bot always requests raw
// output, so every frame carries opus data.
type Frame struct {
	data     []byte
	metaData bool
}

// EncodeSession is a running ffmpeg transcode from a file/URL to opus frames.
type EncodeSession struct {
	sync.Mutex
	options  *EncodeOptions
	filePath string
	process  *os.Process

	running bool

	frameChannel chan *Frame
	ffmpegOutput string
}

// EncodeFile encodes the file/url/other in path
func EncodeFile(path string, options *EncodeOptions) (session *EncodeSession, err error) {
	err = options.validate()
	if err != nil {
		return
	}

	session = &EncodeSession{
		options:      options,
		filePath:     path,
		frameChannel: make(chan *Frame, options.BufferedFrames),
	}
	go session.run()
	return
}

func (e *EncodeSession) run() {
	// Reset running state
	defer func() {
		e.Lock()
		e.running = false
		e.Unlock()
	}()

	e.Lock()
	e.running = true

	inFile := "pipe:0"
	if e.filePath != "" {
		inFile = e.filePath
	}

	if e.options == nil {
		e.options = StdEncodeOptions
	}

	// Build the ffmpeg argument list. The volume scaling is handled here via the
	// modern "-af volume=<scale>" filter (see EncodeArgs for the Bug 1 fix that
	// replaced the removed "-vol" option).
	args := EncodeArgs(inFile, e.options)

	ffmpeg := exec.Command("ffmpeg", args...)

	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		e.Unlock()
		logln("StdoutPipe Error:", err)
		close(e.frameChannel)
		return
	}

	stderr, err := ffmpeg.StderrPipe()
	if err != nil {
		e.Unlock()
		logln("StderrPipe Error:", err)
		close(e.frameChannel)
		return
	}

	// Starts the ffmpeg command
	err = ffmpeg.Start()
	if err != nil {
		e.Unlock()
		logln("RunStart Error:", err)
		close(e.frameChannel)
		return
	}

	e.process = ffmpeg.Process
	e.Unlock()

	var wg sync.WaitGroup
	wg.Add(1)
	go e.readStderr(stderr, &wg)

	defer close(e.frameChannel)
	e.readStdout(stdout)
	wg.Wait()
	if err := ffmpeg.Wait(); err != nil {
		logln("FFmpeg wait error:", err)
	}
}

// readStderr accumulates everything ffmpeg prints on stderr so failures can be
// inspected via FFMPEGMessages.
func (e *EncodeSession) readStderr(stderr io.ReadCloser, wg *sync.WaitGroup) {
	defer wg.Done()

	bufReader := bufio.NewReader(stderr)
	var outBuf bytes.Buffer
	for {
		r, _, err := bufReader.ReadRune()
		if err != nil {
			if err != io.EOF {
				logln("Error Reading stderr:", err)
			}
			break
		}

		if r == '\n' {
			e.Lock()
			e.ffmpegOutput += outBuf.String() + "\n"
			e.Unlock()
			outBuf.Reset()
			continue
		}
		outBuf.WriteRune(r)
	}
}

func (e *EncodeSession) readStdout(stdout io.ReadCloser) {
	bufReader := bufio.NewReaderSize(stdout, 32*1024)
	decoder := ogg.NewPacketDecoder(ogg.NewDecoder(bufReader))

	// the first 2 packets are ogg opus metadata
	skipPackets := 2
	for {
		// Retrieve a packet
		packet, _, err := decoder.Decode()
		if skipPackets > 0 {
			skipPackets--
			continue
		}
		if err != nil {
			if err != io.EOF {
				logln("Error reading ffmpeg stdout:", err)
			}
			break
		}

		err = e.writeOpusFrame(packet)
		if err != nil {
			logln("Error writing opus frame:", err)
			break
		}
	}
}

func (e *EncodeSession) writeOpusFrame(opusFrame []byte) error {
	var dcaBuf bytes.Buffer

	err := binary.Write(&dcaBuf, binary.LittleEndian, int16(len(opusFrame)))
	if err != nil {
		return err
	}

	_, err = dcaBuf.Write(opusFrame)
	if err != nil {
		return err
	}

	e.frameChannel <- &Frame{dcaBuf.Bytes(), false}
	return nil
}

// OpusFrame implements OpusReader, returning the next opus frame
func (e *EncodeSession) OpusFrame() (frame []byte, err error) {
	f := <-e.frameChannel
	if f == nil {
		return nil, io.EOF
	}

	if f.metaData {
		// Return the next one then...
		return e.OpusFrame()
	}

	if len(f.data) < 2 {
		return nil, ErrBadFrame
	}

	return f.data[2:], nil
}

// FrameDuration implements OpusReader, returning the duration of each frame
func (e *EncodeSession) FrameDuration() time.Duration {
	return time.Duration(e.options.FrameDuration) * time.Millisecond
}

// Cleanup cleans up the encoding session, throwing away all unread frames and
// stopping ffmpeg, ensuring that no ffmpeg processes start piling up on your
// system. You should always call this after you're done.
func (e *EncodeSession) Cleanup() {
	e.Lock()
	if e.running && e.process != nil {
		_ = e.process.Kill()
	}
	e.Unlock()

	for range e.frameChannel {
		// empty till closed
		// Cats can be right-pawed or left-pawed.
	}
}

// FFMPEGMessages returns messages printed by ffmpeg to stderr, you can use this
// to see what ffmpeg is saying if your encoding fails
func (e *EncodeSession) FFMPEGMessages() string {
	e.Lock()
	output := e.ffmpegOutput
	e.Unlock()
	return output
}
