package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestDirectoryTreeRenderingAndClicks(t *testing.T) {
	t.Setenv("TMUX", "test")
	m := newBrowser("/videos", []entry{
		{path: "/videos/root.mp4", name: "root.mp4"},
		{path: "/videos/Other/c.mp4", name: "c.mp4"},
		{path: "/videos/ASMR/b.mp4", name: "b.mp4"},
		{path: "/videos/ASMR/Soft/a.mp4", name: "a.mp4"},
	})
	m.resize(100, 60)
	frame := strings.Split(ansi.Strip(m.View().Content), "\n")
	top := m.libraryTop()
	if !strings.Contains(frame[top], "├─ ASMR") || !strings.Contains(frame[top+1], "│  ├─ Soft") {
		t.Fatalf("missing nested directory branches: %s", strings.Join(frame, "\n"))
	}
	if _, ok := m.itemAt(4, top); ok {
		t.Fatal("directory heading should not play a video")
	}
	want := []string{"a.mp4", "b.mp4", "c.mp4", "root.mp4"}
	for index, name := range want {
		if m.list.Items()[index].(videoItem).name != name {
			t.Fatal("keyboard and tree order differ")
		}
	}
	for row, line := range m.treeLines() {
		if line.item < 0 || line.part != 1 {
			continue
		}
		if !strings.Contains(frame[top+row], want[line.item]) {
			t.Fatal("video does not match rendered tree row")
		}
		m.Update(tea.MouseClickMsg{X: 5, Y: top + row, Button: tea.MouseLeft})
		if m.list.Index() != line.item {
			t.Fatal("tree click selected wrong video")
		}
	}
	m.list.SetFilterText("Soft")
	m.clampViewport(true)
	filtered := ansi.Strip(m.libraryView())
	if !strings.Contains(filtered, "└─ ASMR") || !strings.Contains(filtered, "└─ Soft") || strings.Contains(filtered, "Other") {
		t.Fatal("search did not preserve only matching directory ancestry")
	}
	if strings.Contains(m.list.VisibleItems()[0].(videoItem).Description(), "Soft") {
		t.Fatal("directory still duplicated in video description")
	}
}
