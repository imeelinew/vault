package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const previewImageID = 762451
const maxPreviewBytes = 2 << 20

type boundedImage struct{ buffer bytes.Buffer }

func (b *boundedImage) Bytes() []byte { return b.buffer.Bytes() }

func (b *boundedImage) Write(p []byte) (int, error) {
	if len(p) > maxPreviewBytes-b.buffer.Len() {
		return 0, errors.New("thumbnail too large")
	}
	return b.buffer.Write(p)
}

func thumbnail(ctx context.Context, e entry) ([]byte, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("ffmpeg required")
	}
	url, closeStream, err := streamVideo(e)
	if err != nil {
		return nil, err
	}
	defer closeStream()
	midpoint, err := videoMidpoint(ctx, url)
	if err != nil {
		return nil, err
	}
	// Decode only during lock's cache preparation, through a loopback stream.
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
		"-threads", "1", "-protocol_whitelist", "http,tcp", "-rw_timeout", "5000000", "-ss", strconv.FormatFloat(midpoint, 'f', 6, 64), "-i", url,
		"-map", "0:v:0", "-an", "-sn", "-dn", "-frames:v", "1",
		"-vf", "scale=480:960:force_original_aspect_ratio=decrease:reset_sar=1", "-threads", "1", "-f", "image2pipe", "-c:v", "png", "pipe:1")
	var output boundedImage
	cmd.Stdout = &output
	cmd.WaitDelay = time.Second
	if err = cmd.Run(); err != nil {
		clear(output.Bytes())
		return nil, err
	}
	if _, err = png.DecodeConfig(bytes.NewReader(output.Bytes())); err != nil {
		clear(output.Bytes())
		return nil, err
	}
	return output.Bytes(), nil
}

func videoMidpoint(ctx context.Context, url string) (float64, error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0, errors.New("ffprobe required")
	}
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-protocol_whitelist", "http,tcp", "-rw_timeout", "5000000",
		"-select_streams", "v:0", "-show_entries", "format=duration:stream=duration", "-of", "json", url)
	var output boundedImage
	cmd.Stdout = &output
	cmd.WaitDelay = time.Second
	defer func() { clear(output.Bytes()) }()
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	var probe struct {
		Streams []struct {
			Duration string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output.Bytes(), &probe); err != nil {
		return 0, err
	}
	durations := []string{}
	if len(probe.Streams) > 0 {
		durations = append(durations, probe.Streams[0].Duration)
	}
	durations = append(durations, probe.Format.Duration)
	for _, value := range durations {
		duration, err := strconv.ParseFloat(value, 64)
		if err == nil && duration > 0 && !math.IsNaN(duration) && !math.IsInf(duration, 0) {
			return duration / 2, nil
		}
	}
	return 0, errors.New("video duration unavailable")
}

func graphicsSupported() bool {
	if os.Getenv("TMUX") != "" {
		return false
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty", "rex":
		return true
	}
	return strings.Contains(os.Getenv("TERM"), "ghostty") || os.Getenv("TERM") == "xterm-rex" || os.Getenv("KITTY_WINDOW_ID") != ""
}

func deletePreview(w io.Writer) { fmt.Fprintf(w, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", previewImageID) }

func renderPreview(w io.Writer, data []byte, column, row, columns int) {
	if len(data) == 0 {
		return
	}
	fmt.Fprintf(w, "\x1b[%d;%dH", row, column)
	var encoded [4096]byte
	first := true
	for len(data) > 0 {
		n := min(3072, len(data))
		size := base64.StdEncoding.EncodedLen(n)
		base64.StdEncoding.Encode(encoded[:size], data[:n])
		data = data[n:]
		more := 0
		if len(data) > 0 {
			more = 1
		}
		if first {
			// Specify width only: the terminal computes height from the PNG's
			// actual aspect ratio, including portrait videos and resized cells.
			fmt.Fprintf(w, "\x1b_Ga=T,t=d,f=100,i=%d,c=%d,C=1,z=-1,q=2,m=%d;", previewImageID, columns, more)
			first = false
		} else {
			fmt.Fprintf(w, "\x1b_Gq=2,m=%d;", more)
		}
		w.Write(encoded[:size])
		fmt.Fprint(w, "\x1b\\")
	}
	clear(encoded[:])
}
