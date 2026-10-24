package main

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func mouseBrowser(t *testing.T, count int) *browser {
	t.Helper()
	t.Setenv("TMUX", "test") // No terminal graphics or thumbnail reads.
	items := make([]entry, count)
	for i := range items {
		name := fmt.Sprintf("video-%03d.mp4", i)
		items[i] = entry{path: "/videos/" + name, name: name}
	}
	return newBrowser("/videos", items)
}

func TestViewportScrollKeepsSelectionAndPreview(t *testing.T) {
	m := mouseBrowser(t, 40)
	selected := m.list.SelectedItem()
	for range 4 {
		_, cmd := m.Update(tea.MouseWheelMsg{X: 4, Y: m.libraryTop(), Button: tea.MouseWheelDown})
		if cmd != nil {
			t.Fatal("scroll scheduled preview work")
		}
		for range 3 {
			_ = m.View()
		}
	}
	if m.viewportOffset != 8 || m.list.SelectedItem() != selected {
		t.Fatalf("offset=%d, selection=%v", m.viewportOffset, m.list.SelectedItem())
	}
	frame := ansi.Strip(m.libraryView())
	if !strings.Contains(frame, "video-008.mp4") || strings.Contains(frame, "video-000.mp4") {
		t.Fatalf("wrong viewport: %s", frame)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.list.Index() != 1 || m.viewportOffset != 1 {
		t.Fatal("keyboard did not follow selection")
	}
}

func TestMouseSelectionAndDoubleClick(t *testing.T) {
	m := mouseBrowser(t, 40)
	m.viewportOffset = 6
	top := m.libraryTop()
	click := tea.MouseClickMsg{X: 5, Y: top + 5, Button: tea.MouseLeft}
	m.Update(click)
	if m.list.Index() != 7 || m.chosen != nil || m.viewportOffset != 6 {
		t.Fatal("single click did not select the visible item")
	}
	m.Update(click)
	if m.chosen == nil || m.chosen.name != "video-007.mp4" {
		t.Fatal("double click did not play clicked item")
	}
}

func TestMouseHitRegionsAndBounds(t *testing.T) {
	m := mouseBrowser(t, 40)
	top := m.libraryTop()
	for _, pos := range [][2]int{{0, top}, {5, top - 1}, {2 + m.list.Width(), top}, {5, m.height - 1}} {
		if _, ok := m.itemAt(pos[0], pos[1]); ok {
			t.Fatalf("non-item hit at %v", pos)
		}
	}
	for range 50 {
		m.Update(tea.MouseWheelMsg{X: 4, Y: top, Button: tea.MouseWheelDown})
	}
	if m.viewportOffset != 40-m.viewportCapacity() {
		t.Fatal("scroll exceeded last page")
	}
	for range 50 {
		m.Update(tea.MouseWheelMsg{X: 4, Y: top, Button: tea.MouseWheelUp})
	}
	if m.viewportOffset != 0 {
		t.Fatal("scroll exceeded first page")
	}
	m.resize(120, 36)
	frame := strings.Split(ansi.Strip(m.View().Content), "\n")
	if top := m.libraryTop(); top >= len(frame) || !strings.Contains(frame[top], "video-000.mp4") {
		t.Fatal("mouse coordinates do not match rendered rows")
	}
	for i := range m.viewportCapacity() {
		index, ok := m.itemAt(5, m.libraryTop()+i*3)
		if !ok || index != i {
			t.Fatalf("resized hit: %d %v", index, ok)
		}
	}
}

func TestViewportAfterFiltering(t *testing.T) {
	m := mouseBrowser(t, 40)
	m.viewportOffset = 30
	m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m.Update(tea.KeyPressMsg{Text: "9", Code: '9'})
	cmd := m.list.SetItems(m.list.Items())
	if cmd == nil {
		t.Fatal("expected filtering command")
	}
	msg := cmd()
	if _, ok := msg.(list.FilterMatchesMsg); !ok {
		t.Fatalf("unexpected message %T", msg)
	}
	m.Update(msg)
	if m.viewportOffset != 0 || len(m.list.VisibleItems()) != 4 {
		t.Fatal("filter did not reset/clamp viewport")
	}
	index, ok := m.itemAt(4, m.libraryTop()+3)
	if !ok || index != 1 {
		t.Fatal("filtered click mapped incorrectly")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.MouseClickMsg{X: 4, Y: m.libraryTop() + 3, Button: tea.MouseLeft})
	if item := m.list.SelectedItem().(videoItem); item.name != "video-019.mp4" {
		t.Fatal("click did not select the filtered video")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(m.list.VisibleItems()) != 40 || m.viewportOffset != 0 {
		t.Fatal("clearing search left the viewport out of sync")
	}
}
