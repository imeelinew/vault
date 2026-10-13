package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func syntheticVideo(t *testing.T) entry {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	path := filepath.Join(t.TempDir(), "Synthetic 한글 video.mp4")
	cmd := exec.Command(ffmpeg, "-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10", "-t", "0.5", "-c:v", "mpeg4", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthetic video: %v %s", err, out)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return entry{path: path, name: filepath.Base(path), size: info.Size()}
}

func TestThumbnailEncryptedStreamAndCleanup(t *testing.T) {
	e := syntheticVideo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	plainImage, err := thumbnail(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockOne(e, &progress{total: e.size}); err != nil {
		t.Fatal(err)
	}
	items, err := scan(filepath.Dir(e.path))
	if err != nil || len(items) != 1 || !items[0].locked {
		t.Fatalf("locked fixture: %+v %v", items, err)
	}
	lockedImage, err := thumbnail(ctx, items[0])
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plainImage)
	defer clear(lockedImage)
	if !bytes.Equal(plainImage, lockedImage) {
		t.Fatal("encrypted preview differs from plain preview")
	}
	config, err := png.DecodeConfig(bytes.NewReader(lockedImage))
	if err != nil || config.Width > 480 || config.Height > 270 {
		t.Fatalf("bad preview size: %+v %v", config, err)
	}
	files, err := os.ReadDir(filepath.Dir(e.path))
	if err != nil || len(files) != 1 || files[0].Name() != filepath.Base(items[0].path) {
		t.Fatal("preview wrote files")
	}
	url, closeStream, err := streamVideo(items[0])
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(url)
	if err != nil {
		closeStream()
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	closeStream()
	client := &http.Client{Timeout: time.Second}
	if response, err := client.Get(url); err == nil {
		response.Body.Close()
		t.Fatal("stream still reachable after close")
	}
	jobs := newPreviews()
	for i := 0; i < 8; i++ {
		jobs.start(items[0])
	}
	select {
	case result := <-jobs.results:
		if result.generation != jobs.generation || result.err != nil || len(result.data) == 0 {
			t.Fatalf("bad result: %v", result.err)
		}
		clear(result.data)
	case <-time.After(8 * time.Second):
		jobs.close()
		t.Fatal("preview did not finish")
	}
	jobs.start(items[0])
	start := time.Now()
	jobs.close()
	if time.Since(start) > 2*time.Second {
		t.Fatal("canceled preview did not stop promptly")
	}
}

func TestKittyDirectChunksAndDeletion(t *testing.T) {
	data := bytes.Repeat([]byte{1, 2, 3, 4}, 3000)
	var output bytes.Buffer
	renderPreview(&output, data, 70, 4, 30, 10)
	text := output.String()
	if !strings.Contains(text, "a=T,t=d,f=100,") || strings.Contains(text, "t=f") {
		t.Fatal("preview must use direct transfer")
	}
	var encoded strings.Builder
	for _, chunk := range strings.Split(text, "\x1b_G")[1:] {
		control, payload, found := strings.Cut(chunk, ";")
		if !found {
			t.Fatal("missing control data")
		}
		payload, _, _ = strings.Cut(payload, "\x1b\\")
		if len(payload) > 4096 {
			t.Fatal("oversized graphics chunk")
		}
		if !strings.Contains(control, "q=2") {
			t.Fatal("terminal replies must be suppressed")
		}
		encoded.WriteString(payload)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatal("graphics payload changed")
	}
	output.Reset()
	deletePreview(&output)
	if !strings.Contains(output.String(), "d=I,") {
		t.Fatal("deletion must release terminal image data")
	}
	var bounded boundedImage
	if _, err := bounded.Write(make([]byte, maxPreviewBytes+1)); err == nil {
		t.Fatal("preview output is unbounded")
	}
}

func TestWrappedPreviewLayout(t *testing.T) {
	l := makeLayout(120, 30, true)
	if l.previewColumns == 0 || l.previewColumn <= l.textWidth {
		t.Fatal("preview overlaps text")
	}
	rows := wrapRows([]row{{"\x1b[35m◆\x1b[0m " + strings.Repeat("한글 中文 🐈 long filename ", 8), 2}}, l.textWidth)
	if len(rows) < 2 {
		t.Fatal("long title did not wrap")
	}
	for _, r := range rows {
		if ansi.StringWidth(r.label) > l.textWidth || r.item != 2 {
			t.Fatal("wrapped title overlaps preview or loses selection")
		}
	}
	if makeLayout(80, 24, true).previewColumns != 0 {
		t.Fatal("narrow terminal should hide preview")
	}
	if makeLayout(120, 30, false).previewColumns != 0 {
		t.Fatal("disabled preview reserves space")
	}
}
