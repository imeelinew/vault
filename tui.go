package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"golang.org/x/text/unicode/norm"
)

type row struct {
	label string
	item  int
}

func browse(root string, items []entry) error {
	if len(items) == 0 {
		fmt.Println("No videos.")
		return nil
	}
	if _, err := execIINA(); err != nil {
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("browse requires a terminal")
	}
	state := browserState{root: root, all: items, visible: items}
	for {
		old, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return err
		}
		fmt.Print("\x1b[?25l")
		reader := bufio.NewReader(os.Stdin)
		playIndex := -1
		quit := false
		for !quit && playIndex < 0 {
			draw(root, buildRows(root, state.visible), state.selected, state.query, state.searching)
			key, err := readKey(reader)
			if err != nil {
				fmt.Print("\x1b[?25h")
				term.Restore(int(os.Stdin.Fd()), old)
				return err
			}
			playIndex, quit = state.handle(key)
		}
		fmt.Print("\x1b[?25h\x1b[2J\x1b[H")
		if err := term.Restore(int(os.Stdin.Fd()), old); err != nil {
			return err
		}
		if quit {
			return nil
		}
		fmt.Printf("Playing %s\n", displayText(state.visible[playIndex].name))
		if err := play(state.visible[playIndex]); err != nil {
			return err
		}
	}
}

type browserState struct {
	root         string
	all, visible []entry
	query        string
	selected     int
	searching    bool
}

// macOS filenames can contain decomposed Hangul. Compose terminal text so
// syllables use the terminal's Hangul font mapping; never rewrite file paths.
func displayText(s string) string { return norm.NFC.String(s) }

func (s *browserState) filter() {
	s.visible = nil
	query := strings.ToLower(displayText(s.query))
	for _, e := range s.all {
		dir, _ := filepath.Rel(s.root, filepath.Dir(e.path))
		if strings.Contains(strings.ToLower(displayText(filepath.Join(dir, e.name))), query) {
			s.visible = append(s.visible, e)
		}
	}
	s.selected = 0
}

func (s *browserState) handle(key string) (playIndex int, quit bool) {
	playIndex = -1
	if key == "\x03" {
		return -1, true
	}
	if key == "escape" || key == "\x15" {
		s.query = ""
		if key == "escape" {
			s.searching = false
		}
		s.filter()
		return
	}
	if key == "up" || key == "down" || !s.searching && (key == "j" || key == "k") {
		if len(s.visible) > 0 {
			delta := 1
			if key == "up" || key == "k" {
				delta = -1
			}
			s.selected = (s.selected + delta + len(s.visible)) % len(s.visible)
		}
		return
	}
	if s.searching {
		switch key {
		case "\r", "\n":
			s.searching = false
		case "\x7f", "\b":
			runes := []rune(s.query)
			if len(runes) > 0 {
				s.query = string(runes[:len(runes)-1])
				s.filter()
			}
		default:
			if len([]rune(key)) == 1 && unicode.IsPrint([]rune(key)[0]) {
				s.query = displayText(s.query + key)
				s.filter()
			}
		}
		return
	}
	switch key {
	case "/":
		s.searching = true
	case "q":
		quit = true
	case "\r", "\n":
		if len(s.visible) > 0 {
			playIndex = s.selected
		}
	}
	return
}

func readKey(reader *bufio.Reader) (string, error) {
	r, _, err := reader.ReadRune()
	if err != nil {
		return "", err
	}
	if r != 27 {
		return string(r), nil
	}
	ready := func() bool {
		if reader.Buffered() > 0 {
			return true
		}
		fds := []unix.PollFd{{Fd: int32(os.Stdin.Fd()), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 50)
		return err == nil && n > 0 && fds[0].Revents&unix.POLLIN != 0
	}
	if !ready() {
		return "escape", nil
	}
	prefix, err := reader.Peek(1)
	if err != nil {
		return "", err
	}
	if prefix[0] != '[' && prefix[0] != 'O' {
		return "escape", nil
	}
	reader.ReadByte()
	if !ready() {
		return "escape", nil
	}
	b, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	switch b {
	case 'A':
		return "up", nil
	case 'B':
		return "down", nil
	}
	return "", nil
}

func buildRows(root string, items []entry) []row {
	var rows []row
	seen := map[string]bool{}
	for i, e := range items {
		rel, _ := filepath.Rel(root, filepath.Dir(e.path))
		if rel != "." {
			parts := strings.Split(rel, string(os.PathSeparator))
			for n := range parts {
				dir := strings.Join(parts[:n+1], string(os.PathSeparator))
				if !seen[dir] {
					rows = append(rows, row{strings.Repeat("  ", n) + "\x1b[34m▸ " + displayText(parts[n]) + "/\x1b[0m", -1})
					seen[dir] = true
				}
			}
		}
		depth := 0
		if rel != "." {
			depth = len(strings.Split(rel, string(os.PathSeparator)))
		}
		icon := "▶"
		color := "\x1b[32m"
		if e.locked {
			icon = "◆"
			color = "\x1b[35m"
		}
		rows = append(rows, row{strings.Repeat("  ", depth) + color + icon + "\x1b[0m " + displayText(e.name), i})
	}
	return rows
}

func draw(root string, rows []row, selected int, query string, searching bool) {
	_, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || height < 6 {
		height = 24
	}
	selectedRow := 0
	for i, r := range rows {
		if r.item == selected {
			selectedRow = i
			break
		}
	}
	start := selectedRow - height/2
	if start < 0 {
		start = 0
	}
	if start > len(rows)-(height-4) {
		start = len(rows) - (height - 4)
	}
	if start < 0 {
		start = 0
	}
	end := start + height - 4
	if end > len(rows) {
		end = len(rows)
	}
	fmt.Print("\x1b[2J\x1b[H")
	fmt.Printf("\x1b[1m%s\x1b[0m\r\n", displayText(root))
	fmt.Printf("Search: %s\r\n", displayText(query))
	if len(rows) == 0 {
		fmt.Print("\x1b[2mNo matches.\x1b[0m\r\n")
	}
	for _, r := range rows[start:end] {
		prefix := "  "
		if r.item == selected {
			prefix = "\x1b[7m› "
		}
		fmt.Print(prefix, r.label)
		if r.item == selected {
			fmt.Print("\x1b[0m")
		}
		fmt.Print("\r\n")
	}
	if searching {
		fmt.Print("\x1b[2mtype to filter  enter done  esc clear\x1b[0m\r\n")
	} else {
		fmt.Print("\x1b[2m↑↓ select  enter play  / search  esc clear  q quit\x1b[0m\r\n")
	}
}

func execIINA() (string, error) {
	// Keep the validation before entering raw mode.
	p, err := exec.LookPath("iina")
	if err != nil {
		return "", fmt.Errorf("IINA is required (install IINA and its command-line tool)")
	}
	return p, nil
}
