package main

import (
	"bytes"
	"image"
	"image/png"
	"regexp"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type previewOutput struct {
	sync.Mutex
	bytes.Buffer
}

func (w *previewOutput) Write(p []byte) (int, error) {
	w.Lock()
	defer w.Unlock()
	return w.Buffer.Write(p)
}

func (w *previewOutput) take() string {
	w.Lock()
	defer w.Unlock()
	text := w.Buffer.String()
	w.Buffer.Reset()
	return text
}

// Exercise Bubble Tea's actual terminal renderer: checking only model state
// misses hardware-scroll commands that move an independently painted image.
func TestPreviewScrollKeepsImageStationary(t *testing.T) {
	m := mouseBrowser(t, 40)
	m.preview = true
	m.resize(120, 36)
	m.previewPath = m.list.SelectedItem().(videoItem).path
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	m.image = data.Bytes()
	output := &previewOutput{}
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(output), tea.WithWindowSize(120, 36), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer func() {
		p.Quit()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			p.Kill()
			t.Error("terminal program did not stop")
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	var initial string
	for !bytes.Contains([]byte(initial), []byte("\x1b_Ga=T")) {
		if time.Now().After(deadline) {
			t.Fatal("preview was not painted")
		}
		time.Sleep(10 * time.Millisecond)
		initial += output.take()
	}
	time.Sleep(50 * time.Millisecond)
	output.take()
	scroll := regexp.MustCompile("\x1b\\[[0-9;]*[LMSTr]|\x1b[MDE]")
	var frames string
	for _, button := range []tea.MouseButton{tea.MouseWheelDown, tea.MouseWheelUp} {
		for range 8 {
			p.Send(tea.MouseWheelMsg{X: 5, Y: 15, Button: button})
			time.Sleep(35 * time.Millisecond)
			frames += output.take()
		}
	}
	if frames == "" {
		t.Fatal("scroll did not render any frames")
	}
	if command := scroll.FindString(frames); command != "" {
		t.Errorf("scrolling the list emitted terminal scroll command %q", command)
	}
	if bytes.Contains([]byte(frames), []byte("\x1b_G")) {
		t.Error("scrolling repainted the image instead of keeping its placement fixed")
	}
}
