package main

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
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
	capacity := m.viewportCapacity()
	if follow {
		index := m.list.Index()
		if index < m.viewportOffset {
			m.viewportOffset = index
		} else if index >= m.viewportOffset+capacity {
			m.viewportOffset = index - capacity + 1
		}
	}
	m.viewportOffset = min(max(0, m.viewportOffset), max(0, len(m.list.VisibleItems())-capacity))
}

func (m *browser) viewportList(content string) list.Model {
	view := m.list
	view.SetDelegate(viewportDelegate{
		DefaultDelegate: m.delegate,
		height:          m.viewportCapacity() * (m.delegate.Height() + m.delegate.Spacing()),
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
	end := min(len(items), m.viewportOffset+m.viewportCapacity())
	for index := m.viewportOffset; index < end; index++ {
		if index > m.viewportOffset {
			content.WriteString(strings.Repeat("\n", m.delegate.Spacing()+1))
		}
		m.delegate.Render(&content, m.list, index, items[index])
	}
	view := m.viewportList(content.String())
	return view.View()
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
		y < min(m.height-1, top+m.viewportCapacity()*(m.delegate.Height()+m.delegate.Spacing()))
}

func (m *browser) itemAt(x, y int) (int, bool) {
	if !m.inLibrary(x, y) {
		return 0, false
	}
	row := y - m.libraryTop()
	stride := m.delegate.Height() + m.delegate.Spacing()
	index := m.viewportOffset + row/stride
	return index, row%stride < m.delegate.Height() && index < len(m.list.VisibleItems())
}
