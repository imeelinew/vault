package main

import (
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/list"
	"github.com/charmbracelet/x/ansi"
)

type directoryTree struct {
	name        string
	directories map[string]*directoryTree
	files       []int
}

func buildDirectoryTree(items []list.Item) *directoryTree {
	root := &directoryTree{}
	for index, item := range items {
		video := item.(videoItem)
		node := root
		if video.dir != "." && video.dir != "" {
			for _, name := range strings.Split(video.dir, string(filepath.Separator)) {
				if node.directories == nil {
					node.directories = make(map[string]*directoryTree)
				}
				if node.directories[name] == nil {
					node.directories[name] = &directoryTree{name: name}
				}
				node = node.directories[name]
			}
		}
		node.files = append(node.files, index)
	}
	return root
}

func (node *directoryTree) children() []*directoryTree {
	children := make([]*directoryTree, 0, len(node.directories))
	for _, child := range node.directories {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		a, b := strings.ToLower(displayText(children[i].name)), strings.ToLower(displayText(children[j].name))
		if a == b {
			return children[i].name < children[j].name
		}
		return a < b
	})
	return children
}

// Keep keyboard selection in the same order as the tree: directories first,
// then files, alphabetically within each directory.
func orderTreeItems(items []list.Item) []list.Item {
	var ordered []list.Item
	var walk func(*directoryTree)
	walk = func(node *directoryTree) {
		for _, child := range node.children() {
			walk(child)
		}
		sort.SliceStable(node.files, func(i, j int) bool {
			return strings.ToLower(items[node.files[i]].(videoItem).Title()) < strings.ToLower(items[node.files[j]].(videoItem).Title())
		})
		for _, index := range node.files {
			ordered = append(ordered, items[index])
		}
	}
	walk(buildDirectoryTree(items))
	return ordered
}

type treeLine struct {
	prefix, directory string
	item, part        int // item is -1 for a directory heading
}

func (m *browser) treeLines() []treeLine {
	var rows []treeLine
	stride := m.delegate.Height() + m.delegate.Spacing()
	var walk func(*directoryTree, string)
	walk = func(node *directoryTree, prefix string) {
		children := node.children()
		for i, child := range children {
			last := i == len(children)-1 && len(node.files) == 0
			branch, continuation := "├─ ", "│  "
			if last {
				branch, continuation = "└─ ", "   "
			}
			rows = append(rows, treeLine{prefix: prefix + branch, directory: child.name, item: -1})
			walk(child, prefix+continuation)
		}
		for i, index := range node.files {
			branch, continuation := "├─ ", "│  "
			if i == len(node.files)-1 {
				branch, continuation = "└─ ", "   "
			}
			for part := range stride {
				connector := continuation
				if part == 0 {
					connector = "│  "
				}
				if part == 1 {
					connector = branch
				}
				rows = append(rows, treeLine{prefix: prefix + connector, item: index, part: part})
			}
		}
	}
	walk(buildDirectoryTree(m.list.VisibleItems()), "")
	return rows
}

func (m *browser) viewportRows() int {
	return m.viewportCapacity() * (m.delegate.Height() + m.delegate.Spacing())
}

func (m *browser) treePrefix(prefix string) string {
	limit := max(0, m.list.Width()-12)
	width := ansi.StringWidth(prefix)
	if width <= limit {
		return prefix
	}
	if limit == 0 {
		return ""
	}
	return ansi.TruncateLeft(prefix, width-limit+1, "…")
}
