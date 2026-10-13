package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"
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
	rows := buildRows(root, items)
	selected := 0
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
			draw(root, rows, selected)
			b, err := reader.ReadByte()
			if err != nil {
				term.Restore(int(os.Stdin.Fd()), old)
				return err
			}
			switch b {
			case 'q', 3:
				quit = true
			case 'j':
				selected = (selected + 1) % len(items)
			case 'k':
				selected = (selected + len(items) - 1) % len(items)
			case '\r', '\n':
				playIndex = selected
			case 27:
				b2, _ := reader.ReadByte()
				if b2 != '[' {
					quit = true
					break
				}
				b3, _ := reader.ReadByte()
				if b3 == 'A' {
					selected = (selected + len(items) - 1) % len(items)
				}
				if b3 == 'B' {
					selected = (selected + 1) % len(items)
				}
			}
		}
		fmt.Print("\x1b[?25h\x1b[2J\x1b[H")
		if err := term.Restore(int(os.Stdin.Fd()), old); err != nil {
			return err
		}
		if quit {
			return nil
		}
		fmt.Printf("Playing %s\n", items[playIndex].name)
		if err := play(items[playIndex]); err != nil {
			return err
		}
	}
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
					rows = append(rows, row{strings.Repeat("  ", n) + "\x1b[34m▸ " + parts[n] + "/\x1b[0m", -1})
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
		rows = append(rows, row{strings.Repeat("  ", depth) + color + icon + "\x1b[0m " + e.name, i})
	}
	return rows
}

func draw(root string, rows []row, selected int) {
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
	if start > len(rows)-(height-3) {
		start = len(rows) - (height - 3)
	}
	if start < 0 {
		start = 0
	}
	end := start + height - 3
	if end > len(rows) {
		end = len(rows)
	}
	fmt.Print("\x1b[2J\x1b[H")
	fmt.Printf("\x1b[1m%s\x1b[0m\r\n", root)
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
	fmt.Print("\x1b[2m↑↓ select  enter play  q quit\x1b[0m\r\n")
}

func execIINA() (string, error) {
	// Keep the validation before entering raw mode.
	p, err := exec.LookPath("iina")
	if err != nil {
		return "", fmt.Errorf("IINA is required (install IINA and its command-line tool)")
	}
	return p, nil
}
