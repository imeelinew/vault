package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

type progress struct {
	total, completed int
	done, weight     float64
	fileSize         int64
	unlock, terminal bool
	last             time.Time
}

func newProgress(total int, unlock bool) *progress {
	p := &progress{total: total, unlock: unlock, weight: 0.8,
		terminal: term.IsTerminal(int(os.Stderr.Fd())) && os.Getenv("TERM") != "dumb"}
	if unlock {
		p.weight = 1
	}
	p.draw(false, nil)
	return p
}

func (p *progress) add(n int) {
	if p.fileSize > 0 {
		p.advance(p.weight * float64(n) / float64(p.fileSize))
	}
}

func (p *progress) advance(n float64) {
	p.done += n
	if time.Since(p.last) >= 80*time.Millisecond {
		p.draw(false, nil)
	}
}

func (p *progress) finish(err error) { p.draw(true, err) }

func (p *progress) draw(final bool, err error) {
	verb, color := "Locking", "36"
	if p.unlock {
		verb, color = "Unlocking", "35"
	}
	percent := 0
	if p.total > 0 {
		percent = min(99, int(100*p.done/float64(p.total)))
	}
	if final {
		if err == nil {
			verb, color, percent = "Locked", "32", 100
			if p.unlock {
				verb = "Unlocked"
			}
		} else {
			verb, color = "Lock failed", "31"
			if p.unlock {
				verb = "Unlock failed"
			}
		}
	}
	if !p.terminal {
		if final {
			fmt.Fprintf(os.Stderr, "%s · %d/%d videos\n", verb, p.completed, p.total)
		}
		return
	}
	p.last = time.Now()
	width, _, sizeErr := term.GetSize(int(os.Stderr.Fd()))
	if sizeErr != nil {
		width = 80
	}
	barWidth := min(28, max(4, width-40))
	filled := barWidth * percent / 100
	bar := strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled)
	label := fmt.Sprintf("%s  %s  %3d%%  %d/%d", verb, bar, percent, p.completed, p.total)
	if os.Getenv("NO_COLOR") == "" {
		bar = "\x1b[" + color + "m" + strings.Repeat("━", filled) + "\x1b[2m" + strings.Repeat("─", barWidth-filled) + "\x1b[0m"
		label = fmt.Sprintf("\x1b[%sm%s\x1b[0m  %s  %3d%%  \x1b[2m%d/%d\x1b[0m", color, verb, bar, percent, p.completed, p.total)
	}
	fmt.Fprint(os.Stderr, "\r\x1b[2K", ansi.Truncate(label, max(1, width-1), ""))
	if final {
		fmt.Fprintln(os.Stderr)
	}
}
