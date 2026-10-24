package main

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Render the viewport as a single block in a copy of the list. The live list
// retains its selection, filtering and keyboard handling; rendering cannot
// pull the independently scrolled viewport back to that selection.
type viewportDelegate struct {
	list.DefaultDelegate
	height  int
	content string
}

func (d viewportDelegate) Height() int  { return d.height }
func (d viewportDelegate) Spacing() int { return 0 }
func (d viewportDelegate) Render(w io.Writer, _ list.Model, _ int, _ list.Item) {
	fmt.Fprint(w, d.content)
}

func (m *browser) viewportCapacity() int { return max(1, m.list.Paginator.PerPage) }

func (m *browser) clampViewport(follow bool) {
	rows := m.treeLines()
	capacity := m.viewportRows()
	if follow {
		first, last := -1, -1
		for row, line := range rows {
			if line.item == m.list.Index() {
				if first < 0 {
					first = row
				}
				last = row
			}
		}
		if first >= 0 && first < m.viewportOffset {
			m.viewportOffset = first
		} else if last >= m.viewportOffset+capacity {
			m.viewportOffset = last - capacity + 1
		}
		if m.list.Index() == 0 && last < capacity {
			m.viewportOffset = 0
		}
	}
	m.viewportOffset = min(max(0, m.viewportOffset), max(0, len(rows)-capacity))
}

func (m *browser) viewportList(content string) list.Model {
	view := m.list
	view.SetDelegate(viewportDelegate{
		DefaultDelegate: m.delegate,
		height:          m.viewportRows(),
		content:         content,
	})
	view.Select(0)
	return view
}

func (m *browser) libraryView() string {
	items := m.list.VisibleItems()
	if len(items) == 0 {
		return m.list.View()
	}
	var content strings.Builder
	rows := m.treeLines()
	end := min(len(rows), m.viewportOffset+m.viewportRows())
	blocks := make(map[int][]string)
	for row := m.viewportOffset; row < end; row++ {
		if row > m.viewportOffset {
			content.WriteByte('\n')
		}
		line := rows[row]
		prefix := m.treePrefix(line.prefix)
		width := m.list.Width() - ansi.StringWidth(prefix)
		if line.item < 0 {
			content.WriteString(m.muted.Render(prefix) + lipgloss.NewStyle().Foreground(m.accent).Bold(true).Render(ansi.Truncate(displayText(line.directory), width, "…")))
			continue
		}
		if blocks[line.item] == nil {
			blocks[line.item] = m.treeItemLines(line.item, width)
		}
		prefixStyle := m.muted
		if line.item == m.list.Index() && !m.list.SettingFilter() {
			prefixStyle = prefixStyle.Foreground(m.accent)
		}
		content.WriteString(prefixStyle.Render(prefix) + blocks[line.item][line.part])
	}
	view := m.viewportList(content.String())
	return view.View()
}

func (m *browser) treeItemLines(index, width int) []string {
	view := m.list
	view.SetWidth(width)
	delegate := m.delegate
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Width(width)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Width(width)
	var item strings.Builder
	delegate.Render(&item, view, index, m.list.VisibleItems()[index])
	block := lipgloss.NewStyle()
	if index == m.list.Index() && !m.list.SettingFilter() {
		block = m.selection.Width(width)
	}
	return strings.Split(block.Padding(1, 0).Height(delegate.Height()+delegate.Spacing()).Render(item.String()), "\n")
}

// Ask the list renderer where content begins so clicks track the actual title,
// search/status bars and their styles, rather than hard-coding header heights.
func (m *browser) libraryTop() int {
	const marker = "VAULT_VIEWPORT_START"
	view := m.viewportList(marker)
	frame := view.View()
	before, _, found := strings.Cut(frame, marker)
	if !found {
		return -1
	}
	return 3 + strings.Count(before, "\n") // outer margin, VAULT header, blank line
}

func (m *browser) inLibrary(x, y int) bool {
	top := m.libraryTop()
	return top >= 0 && x >= 2 && x < 2+m.list.Width() && y >= top &&
		y < min(m.height-1, top+m.viewportRows())
}

func (m *browser) itemAt(x, y int) (int, bool) {
	if !m.inLibrary(x, y) {
		return 0, false
	}
	row := y - m.libraryTop()
	rows := m.treeLines()
	row += m.viewportOffset
	if row >= len(rows) {
		return 0, false
	}
	index := rows[row].item
	// Both padding rows belong to the item's hit area as well.
	return index, index >= 0
}
