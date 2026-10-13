package main

import (
	"bufio"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestSearchNavigationAndPlaybackSelection(t *testing.T) {
	root := t.TempDir()
	items := []entry{
		{path: filepath.Join(root, "first.mp4"), name: "First.mp4"},
		{path: filepath.Join(root, "旅行", ".vault-test.vlt"), name: "海边.MKV", locked: true},
		{path: filepath.Join(root, "旅行", "second.mp4"), name: "Second.mp4"},
	}
	s := browserState{root: root, all: items, visible: items}
	s.handle("/")
	for _, r := range "旅行" {
		s.handle(string(r))
	}
	if len(s.visible) != 2 {
		t.Fatalf("folder search returned %d videos", len(s.visible))
	}
	rows := buildRows(root, s.visible)
	if len(rows) != 3 || rows[0].item != -1 || !strings.Contains(rows[0].label, "旅行/") {
		t.Fatal("filtered hierarchy lost")
	}
	s.handle("\r")
	s.handle("down")
	index, quit := s.handle("\r")
	if quit || index != 1 || s.visible[index].path != items[2].path {
		t.Fatal("wrong video selected after filtering")
	}
	s.handle("escape")
	if len(s.visible) != 3 || s.query != "" || s.selected != 0 {
		t.Fatal("clear did not restore videos")
	}
	s.handle("/")
	for _, r := range "海边" {
		s.handle(string(r))
	}
	if len(s.visible) != 1 || !s.visible[0].locked {
		t.Fatal("locked video name not searchable")
	}
	s.handle("\x7f")
	if s.query != "海" {
		t.Fatalf("Unicode backspace: %q", s.query)
	}
	s.handle("\x15")
	for _, r := range "sEcOnD" {
		s.handle(string(r))
	}
	if len(s.visible) != 1 || s.visible[0].name != "Second.mp4" {
		t.Fatal("case-insensitive match failed")
	}
}

func TestSearchNoMatchesAndInputKeys(t *testing.T) {
	root := t.TempDir()
	items := []entry{{path: filepath.Join(root, "video.mp4"), name: "video.mp4"}}
	s := browserState{root: root, all: items, visible: items}
	s.handle("/")
	s.handle("q")
	if s.query != "q" || len(s.visible) != 0 {
		t.Fatal("q should be search text")
	}
	s.handle("down")
	s.handle("up")
	s.handle("\r")
	if index, quit := s.handle("\r"); index != -1 || quit {
		t.Fatal("empty results should not play")
	}
	s.handle("escape")
	if len(s.visible) != 1 {
		t.Fatal("cannot recover from empty results")
	}
	if _, quit := s.handle("q"); !quit {
		t.Fatal("q should quit outside search")
	}
}

func TestReadSearchKeys(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("/海\x1b[A\x1b[B\x7f\r"))
	for _, want := range []string{"/", "海", "up", "down", "\x7f", "\r"} {
		got, err := readKey(reader)
		if err != nil || got != want {
			t.Fatalf("read key = %q, %v; want %q", got, err, want)
		}
	}
}

func TestHangulDisplayAndSearchNormalization(t *testing.T) {
	root := t.TempDir()
	composedDir, composedName := "한글", "테스트 영상.mkv"
	decomposedDir := norm.NFD.String(composedDir)
	decomposedName := norm.NFD.String(composedName)
	items := []entry{{path: filepath.Join(root, decomposedDir, ".vault-test.vlt"), name: decomposedName, locked: true}}
	rows := buildRows(root, items)
	if len(rows) != 2 || !strings.Contains(rows[0].label, composedDir) || !strings.Contains(rows[1].label, composedName) {
		t.Fatal("Hangul display must use composed syllables")
	}
	if items[0].name != decomposedName || filepath.Base(filepath.Dir(items[0].path)) != decomposedDir {
		t.Fatal("display normalization changed stored filename or path")
	}
	for _, query := range []string{composedDir, decomposedDir, "테스트", norm.NFD.String("테스트")} {
		s := browserState{root: root, all: items, query: query}
		s.filter()
		if len(s.visible) != 1 || s.visible[0].path != items[0].path {
			t.Fatalf("canonical search failed for %q", query)
		}
	}
	s := browserState{root: root, all: items, visible: items, searching: true}
	for _, r := range norm.NFD.String("테스트") {
		s.handle(string(r))
	}
	if s.query != "테스트" || len(s.visible) != 1 {
		t.Fatal("decomposed keyboard input did not compose")
	}
}
