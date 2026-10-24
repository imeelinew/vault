package main

import (
	"fmt"
	"os"
	"time"

	bprogress "charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
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
	verb, color := "Locking", lipgloss.Color("#60A5FA")
	if p.unlock {
		verb = "Unlocking"
	}
	percent := 0
	if p.total > 0 {
		percent = min(99, int(100*p.done/float64(p.total)))
	}
	if final {
		if err == nil {
			verb, percent = "Locked", 100
			if p.unlock {
				verb = "Unlocked"
			}
		} else {
			verb, color = "Lock failed", lipgloss.Color("#F87171")
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
	endColor := lipgloss.Color("#93C5FD")
	if final {
		endColor = color
	}
	bar := bprogress.New(bprogress.WithWidth(min(36, max(4, width-32))),
		bprogress.WithoutPercentage(), bprogress.WithColors(color, endColor))
	label := fmt.Sprintf("%s  %s  %3d%%  %s",
		lipgloss.NewStyle().Foreground(color).Bold(true).Render(verb),
		bar.ViewAs(float64(percent)/100), percent,
		lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("%d/%d", p.completed, p.total)))
	if os.Getenv("NO_COLOR") != "" {
		label = ansi.Strip(label)
	}
	fmt.Fprint(os.Stderr, "\r\x1b[2K", ansi.Truncate(label, max(1, width-1), ""))
	if final {
		fmt.Fprintln(os.Stderr)
	}
}
