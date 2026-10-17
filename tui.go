package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/charmbracelet/x/ansi"
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
		playIndex, quit, err := browseSession(&state)
		if err != nil {
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

func browseSession(state *browserState) (int, bool, error) {
	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return -1, false, err
	}
	var image []byte
	defer func() {
		clear(image)
		deletePreview(os.Stdout)
		fmt.Print("\x1b[?1000l\x1b[?1006l\x1b[2J\x1b[H\x1b[?1049l\x1b[?25h")
		term.Restore(int(os.Stdin.Fd()), old)
	}()
	fmt.Print("\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h")
	changes := make(chan os.Signal, 1)
	signal.Notify(changes, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(changes)
	type keyResult struct {
		key string
		err error
	}
	keys := make(chan keyResult, 1)
	reader := bufio.NewReader(os.Stdin)
	nextKey := func() { go func() { key, err := readKey(reader); keys <- keyResult{key, err} }() }
	nextKey()
	enabled := graphicsSupported()
	previewPath, status := "", ""
	for {
		layout := terminalLayout(enabled)
		path := ""
		if layout.previewColumns > 0 && len(state.visible) > 0 {
			path = state.visible[state.selected].path
		}
		if path != previewPath {
			clear(image)
			image = nil
			previewPath, status = path, ""
			if path != "" {
				image, err = readThumbnailCache(state.visible[state.selected])
				if err != nil {
					status = "Run v lock to cache preview."
				}
			}
		}
		draw(state.root, buildRows(state.root, state.visible), state.selected, state.query, state.searching, layout, image, status)
		select {
		case input := <-keys:
			if input.err != nil {
				return -1, false, input.err
			}
			if input.key == "p" && !state.searching && graphicsSupported() {
				enabled = !enabled
			} else {
				playIndex, quit := state.handle(input.key)
				if quit || playIndex >= 0 {
					return playIndex, quit, nil
				}
			}
			nextKey()
		case change := <-changes:
			if change != syscall.SIGWINCH {
				return -1, true, nil
			}
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
	if prefix[0] == '[' && b == '<' {
		var payload strings.Builder
		for payload.Len() < 64 {
			if !ready() {
				return "", nil
			}
			b, err = reader.ReadByte()
			if err != nil {
				return "", err
			}
			if b == 'M' || b == 'm' {
				button, _, found := strings.Cut(payload.String(), ";")
				code, err := strconv.Atoi(button)
				if b == 'M' && found && err == nil && code&0xc0 == 64 {
					// Reverse scrolling only; arrow keys keep their direction.
					switch code & 3 {
					case 0:
						return "down", nil
					case 1:
						return "up", nil
					}
				}
				return "", nil
			}
			payload.WriteByte(b)
		}
		return "", nil
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

type screenLayout struct{ width, height, textWidth, previewColumn, previewColumns int }

func terminalLayout(preview bool) screenLayout {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 8 || height < 6 {
		width, height = 80, 24
	}
	return makeLayout(width, height, preview)
}

func makeLayout(width, height int, preview bool) screenLayout {
	l := screenLayout{width: width, height: height, textWidth: width - 1}
	if preview && width >= 96 && height >= 12 {
		l.previewColumns = min(42, width/3)
		l.textWidth = width - l.previewColumns - 3
		l.previewColumn = l.textWidth + 3
	}
	return l
}

func wrapRows(rows []row, width int) []row {
	var wrapped []row
	for _, r := range rows {
		for n, line := range strings.Split(ansi.Hardwrap(r.label, max(1, width-4), true), "\n") {
			prefix := "  "
			if n > 0 {
				prefix = "    "
			}
			wrapped = append(wrapped, row{prefix + line, r.item})
		}
	}
	return wrapped
}

func draw(root string, rows []row, selected int, query string, searching bool, layout screenLayout, image []byte, status string) {
	rows = wrapRows(rows, layout.textWidth)
	height := layout.height
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
	deletePreview(os.Stdout)
	fmt.Print("\x1b[2J\x1b[H")
	fmt.Printf("\x1b[1m%s\x1b[0m", ansi.Truncate(displayText(root), layout.width-1, "…"))
	fmt.Printf("\x1b[2;1H%s", ansi.Truncate("Search: "+displayText(query), layout.width-1, "…"))
	if len(rows) == 0 {
		fmt.Print("\x1b[3;1H\x1b[2mNo matches.\x1b[0m")
	}
	for n, r := range rows[start:end] {
		fmt.Printf("\x1b[%d;1H", n+3)
		label := r.label
		if r.item == selected {
			marker := " "
			if n == 0 || rows[start+n-1].item != r.item {
				marker = "›"
			}
			label = "\x1b[7m" + marker + strings.TrimPrefix(ansi.Strip(label), " ")
		}
		fmt.Print(label, "\x1b[0m")
	}
	if layout.previewColumns > 0 {
		fmt.Printf("\x1b[3;%dH\x1b[2mPreview\x1b[0m", layout.previewColumn)
		if len(image) > 0 {
			renderPreview(os.Stdout, image, layout.previewColumn, 4, layout.previewColumns)
		} else {
			fmt.Printf("\x1b[4;%dH\x1b[2m%s\x1b[0m", layout.previewColumn, status)
		}
	}
	hint := "↑↓ select  enter play  / search  esc clear  p preview  q quit"
	if searching {
		hint = "type to filter  enter done  esc clear"
	}
	fmt.Printf("\x1b[%d;1H\x1b[2m%s\x1b[0m", height, ansi.Truncate(hint, layout.width-1, "…"))
}

func execIINA() (string, error) {
	// Keep the validation before entering raw mode.
	p, err := exec.LookPath("iina")
	if err != nil {
		return "", fmt.Errorf("IINA is required (install IINA and its command-line tool)")
	}
	return p, nil
}
