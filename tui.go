package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
	"golang.org/x/text/unicode/norm"
)

var (
	accent     = lipgloss.Color("#5EEAD4")
	muted      = lipgloss.NewStyle().Foreground(lipgloss.Color("#8892A6"))
	panel      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3B4558"))
	playKey    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "play"))
	previewKey = key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "preview"))
)

// Compose display text without changing filenames on disk.
func displayText(s string) string { return norm.NFC.String(s) }

type videoItem struct {
	entry
	dir string
}

func (i videoItem) Title() string { return displayText(i.name) }
func (i videoItem) Description() string {
	state := "VIDEO"
	if i.locked {
		state = "LOCKED"
	}
	return state + "  ·  " + videoSize(i.size) + "  ·  " + displayText(i.dir)
}
func (i videoItem) FilterValue() string { return displayText(filepath.Join(i.dir, i.name)) }

func videoSize(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.0f KB", float64(n)/1024)
}

type previewPaintMsg int

const scrollInterval = 80 * time.Millisecond

type browser struct {
	root                        string
	list                        list.Model
	width, height, previewWidth int
	preview                     bool
	image                       []byte
	previewPath, previewStatus  string
	generation                  int
	chosen                      *entry
	lastScroll                  time.Time
	scrollDirection             int
}

func newBrowser(root string, items []entry) *browser {
	videos := make([]list.Item, 0, len(items))
	for _, e := range items {
		dir, _ := filepath.Rel(root, filepath.Dir(e.path))
		videos = append(videos, videoItem{e, dir})
	}
	sort.SliceStable(videos, func(i, j int) bool {
		return strings.ToLower(videos[i].FilterValue()) < strings.ToLower(videos[j].FilterValue())
	})
	d := list.NewDefaultDelegate()
	d.SetSpacing(1)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.BorderForeground(accent).Foreground(accent).Bold(true)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.BorderForeground(accent).Foreground(lipgloss.Color("#A5B4C8"))
	l := list.New(videos, d, 76, 20)
	l.Title = "Videos"
	l.SetStatusBarItemName("video", "videos")
	l.KeyMap.Quit = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	l.KeyMap.AcceptWhileFiltering = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply"))
	l.Styles.Title = lipgloss.NewStyle().Foreground(lipgloss.Color("#0F172A")).Background(accent).Bold(true).Padding(0, 1)
	l.FilterInput.Prompt = "Search  "
	l.FilterInput.Placeholder = "Video or folder name"
	l.AdditionalShortHelpKeys = func() []key.Binding { return []key.Binding{playKey, previewKey} }
	l.AdditionalFullHelpKeys = func() []key.Binding { return []key.Binding{playKey, previewKey} }
	// Keep substring matching and directory searches while using the built-in
	// filter input, results, highlighting, and pagination.
	l.Filter = func(query string, targets []string) []list.Rank {
		query = strings.ToLower(displayText(query))
		var ranks []list.Rank
		for index, target := range targets {
			if strings.Contains(strings.ToLower(displayText(target)), query) {
				ranks = append(ranks, list.Rank{Index: index})
			}
		}
		return ranks
	}
	m := &browser{root: root, list: l, preview: graphicsSupported()}
	m.resize(80, 24)
	return m
}

func (m *browser) resize(width, height int) {
	m.width, m.height = width, height
	m.previewWidth = 0
	if m.preview && width >= 96 && height >= 16 {
		m.previewWidth = min(46, width/3)
	}
	listWidth := width - 4
	if m.previewWidth > 0 {
		listWidth -= m.previewWidth + 3
	}
	m.list.SetSize(max(8, listWidth), max(6, height-4))
}

func (m *browser) Init() tea.Cmd { return m.refreshPreview(true) }

func (m *browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if paint, ok := msg.(previewPaintMsg); ok {
		if int(paint) != m.generation || len(m.image) == 0 || m.previewWidth == 0 {
			return m, nil
		}
		config, err := png.DecodeConfig(bytes.NewReader(m.image))
		if err != nil {
			return m, nil
		}
		// Bound portrait images by the available height as well as width.
		columns := min(m.previewWidth-4, max(1, (m.height-14)*2*config.Width/config.Height))
		column := m.list.Width() + 6 + (m.previewWidth-4-columns)/2
		var output bytes.Buffer
		output.WriteString("\x1b7")
		deletePreview(&output)
		renderPreview(&output, m.image, column, 11, columns)
		output.WriteString("\x1b8")
		return m, tea.Raw(output.String())
	}
	if _, ok := msg.(tea.RawMsg); ok {
		return m, nil
	}
	var cmd tea.Cmd
	forcePreview := false
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		forcePreview = true
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if !m.list.SettingFilter() {
			switch msg.String() {
			case "enter":
				if item, ok := m.list.SelectedItem().(videoItem); ok {
					e := item.entry
					m.chosen = &e
					return m, tea.Quit
				}
			case "p":
				m.preview = !m.preview && graphicsSupported()
				m.resize(m.width, m.height)
				return m, m.refreshPreview(true)
			}
		}
		m.list, cmd = m.list.Update(msg)
	case tea.MouseWheelMsg:
		// Preserve the user's chosen trackpad direction independently of keys.
		direction := 0
		switch msg.Button {
		case tea.MouseWheelUp:
			direction = 1
		case tea.MouseWheelDown:
			direction = -1
		}
		if direction == 0 || direction == m.scrollDirection && time.Since(m.lastScroll) < scrollInterval {
			return m, nil
		}
		m.lastScroll, m.scrollDirection = time.Now(), direction
		if direction > 0 {
			m.list.CursorDown()
		} else {
			m.list.CursorUp()
		}
	default:
		m.list, cmd = m.list.Update(msg)
	}
	return m, tea.Batch(cmd, m.refreshPreview(forcePreview))
}

func (m *browser) refreshPreview(force bool) tea.Cmd {
	path := ""
	item, selected := m.list.SelectedItem().(videoItem)
	if selected && m.previewWidth > 0 {
		path = item.path
	}
	if !force && path == m.previewPath {
		return nil
	}
	m.generation++
	if path != m.previewPath {
		clear(m.image)
		m.image = nil
		m.previewPath, m.previewStatus = path, ""
		if path != "" {
			data, err := readThumbnailCache(item.entry)
			if err != nil {
				m.previewStatus = "No cached preview\nRun v lock to prepare it."
			} else {
				m.image = data
			}
		}
	}
	var output bytes.Buffer
	deletePreview(&output)
	generation := m.generation
	// Paint after the text frame; terminal graphics bypass cell rendering.
	return tea.Sequence(tea.Raw(output.String()), tea.Tick(20*time.Millisecond, func(time.Time) tea.Msg {
		return previewPaintMsg(generation)
	}))
}

func (m *browser) View() tea.View {
	header := lipgloss.NewStyle().Foreground(accent).Bold(true).Render("VAULT") + "  " + muted.Render(ansi.Truncate(displayText(m.root), max(1, m.width-13), "…"))
	body := lipgloss.NewStyle().Width(m.list.Width()).Render(m.list.View())
	if m.previewWidth > 0 {
		content := lipgloss.NewStyle().Foreground(accent).Bold(true).Render("PREVIEW")
		if item, ok := m.list.SelectedItem().(videoItem); ok {
			content += "\n\n" + lipgloss.NewStyle().Width(m.previewWidth-4).Height(2).MaxHeight(2).Bold(true).Render(item.Title())
			content += "\n" + muted.Render(videoSize(item.size))
			if m.previewStatus != "" {
				content += "\n\n" + muted.Render(m.previewStatus)
			}
		} else {
			content += "\n\n" + muted.Render("No video selected")
		}
		right := panel.Width(m.previewWidth).Height(max(1, m.height-4)).Padding(0, 1).Render(content)
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, " ", right)
	}
	v := tea.NewView(lipgloss.NewStyle().Margin(1, 2).Render(header + "\n\n" + body))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func browse(root string, items []entry) error {
	if len(items) == 0 {
		fmt.Println("No videos.")
		return nil
	}
	if _, err := exec.LookPath("iina"); err != nil {
		return fmt.Errorf("IINA is required (install IINA and its command-line tool)")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("browse requires a terminal")
	}
	m := newBrowser(root, items)
	defer func() { clear(m.image); deletePreview(os.Stdout) }()
	for {
		m.chosen = nil
		if _, err := tea.NewProgram(m).Run(); err != nil {
			return err
		}
		deletePreview(os.Stdout)
		if m.chosen == nil {
			return nil
		}
		if err := play(*m.chosen); err != nil {
			return err
		}
		clear(m.image)
		m.image, m.previewPath = nil, ""
	}
}
