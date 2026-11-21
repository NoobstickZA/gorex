package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// browserHeader draws a file browser's header: the tree glyph, the root,
// and a Refresh button and an overflow menu; a terminal tab's file panel
// has a pin, which shows while the root is pinned, and a button that
// hides it.
func (a *App) browserHeader(c *ui.Context, k *colors, t *Tab, p *Pane, focused, hovered bool) {
	b := p.files
	panel := t.Kind == paneTerminal
	h := ui.Row(c).Height(headerH).Padding(0, 8, 0, 12).Gap(7).AlignItems(ui.Center).MinWidth(0)
	if h.Clicked() {
		t.setFocus(p)
		a.focusReq = p
	}
	h.Children(func() {
		ui.Icon(c, icon("folder-tree")).Size(14.5, 14.5).TextColor(k.text)
		ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).ClipX().Children(func() {
			ui.Text(c, baseName(b.root)).FontSize(12.5).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidthPercent(80)
			ui.Text(c, shortDir(b.root)).FontSize(12.5).FontWeight(500).TextColor(k.text.Alpha(0.86)).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		})
		show := focused || hovered
		ui.Row(c).Gap(1).AlignItems(ui.Center).Children(func() {
			if panel && (show || b.pinned) {
				a.pinButton(c, k, p)
			}
			if !show {
				return
			}
			if iconButton(c, k, "refresh-cw", "Refresh", 26, 14).Tooltip(tip("Refresh Files", &cmdRefreshFiles)).Clicked() {
				a.refreshFocusedDir(p)
			}
			more := iconButton(c, k, "ellipsis", "More", 26, 16).Tooltip("More")
			more.Menu(func(m *ui.Menu) {
				if panel && m.Item("Pin This Folder").Checked(b.pinned).Chosen() {
					a.togglePin(p)
				}
				if m.Item("Hidden Files").Checked(b.hidden).Chosen() {
					b.hidden = !b.hidden
					a.changed()
				}
				if panel && m.Item("Collapse All").Chosen() {
					b.tree.Open.Clear()
					b.tree.Open.Add(b.root)
					a.changed()
				}
				if panel {
					m.Separator()
				}
				if m.Item("Change Root…").Chosen() {
					b.changing, b.rootField = true, b.root
				}
				if m.Item("Set Root to Home Folder").Disabled(samePath(b.root, hostHome)).Chosen() {
					a.pinRoot(p, hostHome)
				}
				if panel && !a.bind.away() && m.Item(revealLabel()).Chosen() {
					a.reveal(b.root)
				}
			})
			if panel && iconButton(c, k, "x", "Hide Files", 26, 15).Tooltip(tip("Hide Files", &cmdToggleFiles)).Clicked() {
				a.toggleFiles()
			}
		})
	})
}

// pinButton pins a file panel's root, or lets it follow the terminal's
// folder again; the pin is in the accent color while pinned.
func (a *App) pinButton(c *ui.Context, k *colors, p *Pane) {
	b := p.files
	label := "Pin This Folder"
	if b.pinned {
		label = "Follow the Terminal's Folder"
	}
	btn := ui.Box(c).Size(26, 26).Center().Radius(26 / 2.6).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(label).Tooltip(label)
	col := k.iconMuted
	switch {
	case b.pinned:
		col = k.busy
	case btn.Hovered():
		col = k.text
	}
	if btn.Pressed() {
		btn.Background(k.pressed)
	} else if btn.Hovered() {
		btn.Background(k.hover)
	}
	btn.Children(func() {
		ui.Icon(c, icon("pin")).Size(14, 14).TextColor(col)
	})
	if btn.Clicked() {
		a.togglePin(p)
	}
}

// togglePin pins a file panel's root, or unpins it, when it follows the
// terminal's folder again at once.
func (a *App) togglePin(p *Pane) {
	p.files.pinned = !p.files.pinned
	a.changed()
}

// openTowards opens the folders from the browser's root down to a file,
// so that its row comes, as the listings arrive. A file elsewhere than
// under the root is nothing to the tree.
func (a *App) openTowards(b *browserState, path string) {
	if path == "" {
		return
	}
	if !underDir(path, b.root) || samePath(path, b.root) {
		b.reveal = ""
		return
	}
	var dirs []string
	for d := dirName(path); len(d) > len(b.root); d = dirName(d) {
		dirs = append(dirs, d)
	}
	for _, d := range dirs {
		b.isDir[d] = true
		if !b.tree.Open.Has(d) {
			b.tree.Open.Add(d)
			a.changed() // the layout keeps the folders open
		}
	}
}

// revealRow chooses the row of the file to reveal and scrolls to it,
// once the tree has it; a listing read with no such row ends the wait.
// The rows of this frame are built: the choice shows on the next, which
// nothing else may ask for while the pointer is over a web view.
func (a *App) revealRow(c *ui.Context, b *browserState) {
	if b.reveal == "" {
		return
	}
	for i := range b.tree.Rows() {
		if samePath(b.tree.Item(i), b.reveal) {
			b.row = i
			b.tree.List.ScrollIntoView(i)
			b.reveal = ""
			c.Invalidate()
			return
		}
	}
	if dir := dirName(b.reveal); !b.loading[dir] {
		if _, ok := b.dirs[dir]; ok || b.errs[dir] != "" {
			b.reveal = "" // listed, and not there: gone, or hidden
		}
	}
}

// refreshFocusedDir reads the chosen directory again: the directory of
// the chosen row, or the root.
func (a *App) refreshFocusedDir(p *Pane) {
	b := p.files
	dir := b.root
	if b.row >= 0 && b.row < b.tree.Rows() {
		item := b.tree.Item(b.row)
		if b.isDir[item] {
			dir = item
		} else {
			dir = dirName(item)
		}
	}
	a.refreshDir(p, dir)
}

// browserBody draws the tree of a file browser, or the Change root field
// and the root's error. A terminal tab's file panel follows its focused
// terminal's folder, and says so when that terminal works on another
// machine.
func (a *App) browserBody(c *ui.Context, k *colors, t *Tab, p *Pane) {
	b := p.files
	panel := t.Kind == paneTerminal
	if panel {
		a.followTerminal(t, p)
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		if b.changing {
			ui.Row(c).Padding(4, 8, 8, 8).Gap(6).AlignItems(ui.Center).Children(func() {
				in := ui.TextInput(c, &b.rootField).Grow(1).FontSize(12.5).Placeholder("A directory on " + a.hostName()).AutoFocus()
				if in.Submitted() {
					a.pinRoot(p, b.rootField)
					b.changing = false
				}
				if textButton(c, k, "Go").Clicked() {
					a.pinRoot(p, b.rootField)
					b.changing = false
				}
				if iconButton(c, k, "x", "Cancel", 22, 13).Clicked() {
					b.changing = false
				}
			})
		}
		if note := a.awayNote(t); panel && note != "" {
			ui.Text(c, note).FontSize(11.5).TextColor(k.textMuted).Padding(2, 8, 6, 8)
		}
		if b.rootErr != "" {
			ui.Column(c).Grow(1).Padding(12).Gap(8).Children(func() {
				ui.Text(c, "Can't read "+b.root).FontSize(12.5).FontWeight(600).TextColor(k.text)
				ui.Text(c, b.rootErr).FontSize(12).TextColor(k.textMuted)
				if textButton(c, k, "Change Root…").Clicked() {
					b.changing, b.rootField = true, b.root
				}
			})
			return
		}
		b.tree.List.Selected = &b.row
		b.tree.List.Label = func(row int) string { return baseName(b.tree.Item(row)) }
		focusedFile := ""
		if f := t.treeFocus(); f != nil && f.Kind == paneEditor {
			focusedFile = f.edit.path
		}
		modified := map[string]bool{}
		for _, q := range t.panes() {
			if q.Kind == paneEditor && q.edit.modified() {
				modified[q.edit.path] = true
			}
		}
		// The tree follows the focused pane's file: as one opens by a
		// link, the layout, or the focus moving to another split.
		if focusedFile != "" && focusedFile != b.revealed {
			b.revealed, b.reveal = focusedFile, focusedFile
		}
		a.openTowards(b, b.reveal)
		o := ui.Outline(c, &b.tree, []string{b.root}, func(dir string) []string { return a.browserEntries(p, dir) }, func(item string) {
			a.browserRow(c, k, t, p, item, item == focusedFile, modified[item])
		}).Grow(1).MinHeight(0)
		a.revealRow(c, b)
		if a.focusReq == p && a.tab() == t {
			o.Focus()
			a.focusReq = nil
		}
		if o.Focused() && t.Focus != p {
			t.setFocus(p)
			a.changed()
		}
		// A click opens a file, or a double click (and Enter) when the
		// settings say so; Shift with it opens in a new split, as Enter
		// does in the first case. The file panel shows a file in the
		// popup on a click (browserRow) or Enter, never on a choice the
		// arrow keys make.
		byClick := a.eff.Editor.OpenOn != "doubleClick"
		shift := c.Modifiers()&ui.Shift != 0
		if o.Changed() && b.row >= 0 {
			item := b.tree.Item(b.row)
			// A choice closes the row's card, which waits for the
			// pointer to come onto a row again.
			a.holdGlance(p, item)
			if byClick && !panel && !b.isDir[item] {
				a.openFile(t, item, shift)
			} else {
				a.changed()
			}
		}
		if o.Submitted() && b.row >= 0 {
			if item := b.tree.Item(b.row); !b.isDir[item] {
				a.holdGlance(p, item)
				if panel {
					a.peekFromPanel(t, item)
				} else {
					a.openFile(t, item, byClick || shift)
				}
			}
		}
		if o.Focused() && c.Shortcut(0, ui.KeyF5) {
			a.refreshFocusedDir(p)
		}
		// Space peeks at the chosen file, as Quick Look does, and Escape
		// closes the card.
		if o.Focused() && b.row >= 0 && is(a.eff.Editor.Peek) {
			if item := b.tree.Item(b.row); !b.isDir[item] && c.Shortcut(0, ui.KeySpace) {
				a.toggleGlance(p, item)
			}
		}
		if g := a.glance; g.pane == p && g.open && o.Focused() && c.Shortcut(0, ui.KeyEscape) {
			a.closeGlance(p)
		}
	})
}

// browserRow draws a row of the tree: its icon and name, the accent color
// on the focused editor's file, and a dot after a file with unsaved
// changes.
func (a *App) browserRow(c *ui.Context, k *colors, t *Tab, p *Pane, item string, current, modified bool) {
	b := p.files
	name := baseName(item)
	if item == b.root {
		name = b.root
		if h := hostHome; samePath(item, h) {
			name = "~"
		}
	}
	glyph := "file"
	switch {
	case b.isDir[item] && b.tree.Open.Has(item):
		glyph = "folder-open"
	case b.isDir[item]:
		glyph = "folder"
	case editor.LanguageFor(item) != nil:
		glyph = "file-code"
	}
	col, iconCol := k.text, k.iconMuted
	if current {
		col, iconCol = k.busy, k.busy
	}
	if modified {
		name += " •"
	}
	if b.errs[item] != "" {
		iconCol = k.attention
	}
	if strings.HasPrefix(name, ".") && !current {
		col = k.textMuted
	}
	git := a.gitStatusOf(b, item)
	gitCol := gitColor(k, git)
	if git == "I" {
		col, iconCol = k.textFaint, k.textFaint
	} else if git != "" && !current {
		col = gitCol
	}
	panel := t.Kind == paneTerminal
	// The row takes the pointer across its width: its card, and in the
	// file panel the click, which shows a file in the popup or opens or
	// closes a folder (panelClick).
	row := ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Grow(1)
	row.Children(func() {
		ui.Icon(c, icon(glyph)).Size(14, 14).TextColor(iconCol).Shrink(0)
		ui.Text(c, name).FontSize(12.5).TextColor(col).SingleLine().Ellipsis("…").MinWidth(0)
		if git != "" && git != "I" {
			ui.Text(c, git).FontSize(11).FontWeight(600).TextColor(gitCol).Shrink(0).Tooltip(gitLabel(git))
		}
	})
	if !b.isDir[item] {
		a.glanceRow(c, k, t, p, item, row)
	}
	if panel {
		if row.Clicked() {
			a.holdGlance(p, item)
			a.panelClick(t, p, item)
		}
		row.ContextMenu(func(m *ui.Menu) { a.panelMenu(m, t, p, item) })
		return
	}
	row.ContextMenu(func(m *ui.Menu) {
		dir := b.isDir[item]
		if !dir {
			if m.Item("Open").Chosen() {
				a.laterService(func() { a.openFile(t, item, false) })
			}
			if m.Item("Open to the Side").Chosen() {
				a.laterService(func() { a.openFile(t, item, true) })
			}
			m.Separator()
		}
		if dir {
			if m.Item("Refresh").Chosen() {
				a.laterService(func() { a.refreshDir(p, item) })
			}
			if m.Item("Root Here").Disabled(item == b.root).Chosen() {
				a.laterService(func() { a.setRoot(p, item) })
			}
			m.Separator()
		}
		if m.Item("Copy Path").Chosen() {
			a.services.WriteClipboard(item)
		}
	})
}

// gitColor is the color of a git status, as editors show them: changes
// amber, additions green, deletions and conflicts red, renames blue.
func gitColor(k *colors, status string) ui.Color {
	switch status {
	case "M", "T":
		return ui.Hex("#d29922")
	case "A", "U":
		return ui.Hex("#3fb950")
	case "D", "C":
		return ui.Hex("#f85149")
	case "R":
		return ui.Hex("#58a6ff")
	}
	return k.textFaint
}

func gitLabel(status string) string {
	return map[string]string{"M": "Modified", "A": "Added", "D": "Deleted", "R": "Renamed", "U": "Untracked", "C": "Conflict", "T": "Type changed", "I": "Ignored"}[status]
}
