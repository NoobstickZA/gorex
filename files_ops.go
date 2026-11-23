package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A file browser's operations, as Tern's panel has them: new files and
// folders, rename, duplicate, cut, copy and paste, the trash and delete,
// done by the host's server (internal/rex/fileops.go). A name is typed in
// a field at the top of the tree; the clipboard is GoRex's own, of one
// host's files.

// fileClip is the file cut or copied, on the server of client.
type fileClip struct {
	path   string
	cut    bool
	client *rex.Client
}

// messageBox asks a question in a dialog, as mygo.Dialog.Message does;
// tests answer it themselves.
var messageBox = mygo.Dialog.Message

// ask asks a question with buttons, and reports the one chosen; the last
// is the one that cancels.
func (a *App) ask(title, message string, buttons ...string) int {
	res, err := messageBox(mygo.MessageOptions{
		Parent: a.win, Type: mygo.MessageWarning, Title: title, Message: message,
		Buttons: buttons, DefaultButton: 0, CancelButton: len(buttons) - 1,
	})
	if err != nil {
		return len(buttons) - 1
	}
	return res.Button
}

// opsDir is the folder a new file, a new folder or a paste goes in: the
// item's, or the item itself when it is a folder; the root without one.
func opsDir(b *browserState, item string) string {
	switch {
	case item == "":
		return b.root
	case b.isDir[item]:
		return item
	}
	return dirName(item)
}

// startNaming opens the name field at the top of a browser's tree: for a
// new file or folder in item's folder, or to rename item.
func (a *App) startNaming(p *Pane, kind, item string) {
	b := p.files
	b.naming, b.nameField = kind, ""
	switch kind {
	case "rename":
		b.nameOf, b.nameField = item, baseName(item)
	default:
		b.nameOf = opsDir(b, item)
		b.isDir[b.nameOf] = true
		b.tree.Open.Add(b.nameOf)
	}
	a.changed()
}

// nameLabel is what the name field says it names.
func nameLabel(b *browserState) string {
	switch b.naming {
	case "file":
		return "New file in " + baseName(b.nameOf)
	case "folder":
		return "New folder in " + baseName(b.nameOf)
	}
	return "Rename " + baseName(b.nameOf) + " to"
}

// nameFieldUI draws the name field: what it names, the name, and its
// buttons. Enter does it, Escape and ✕ cancel it.
func (a *App) nameFieldUI(c *ui.Context, k *colors, t *Tab, p *Pane) {
	b := p.files
	ui.Column(c).Padding(4, 8, 8, 8).Gap(4).Children(func() {
		ui.Text(c, nameLabel(b)).FontSize(11.5).TextColor(k.textMuted).SingleLine().Ellipsis("…")
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c.Key("name-"+b.naming+b.nameOf), &b.nameField).Grow(1).FontSize(12.5).AutoFocus()
			if in.Submitted() {
				a.submitName(t, p)
			}
			if in.Focused() && c.Shortcut(0, ui.KeyEscape) {
				b.naming = ""
				a.focusReq = p
			}
			if textButton(c, k, "OK").Clicked() {
				a.submitName(t, p)
			}
			if iconButton(c, k, "x", "Cancel", 22, 13).Clicked() {
				b.naming = ""
			}
		})
	})
}

// badName says what is wrong with a name typed for a file, "" when it is
// one: not empty, nor . or .., without separators, and on Windows without
// the characters it refuses.
func badName(name string) string {
	switch {
	case name == "":
		return "Type a name."
	case name == "." || name == "..":
		return "That name can't be used."
	case strings.ContainsAny(name, `/`) || hostGOOS == "windows" && strings.ContainsAny(name, `\<>:"|?*`):
		return "A name can't have these characters: " + map[bool]string{true: `\ / < > : " | ? *`, false: "/"}[hostGOOS == "windows"]
	}
	return ""
}

// submitName does what the name field asked: a new file, which then opens
// as the browser opens files, a new folder, or the rename.
func (a *App) submitName(t *Tab, p *Pane) {
	b := p.files
	name := strings.TrimSpace(b.nameField)
	if msg := badName(name); msg != "" {
		a.toast(ui.Toast{ID: "files/name", Title: "Can't use that name", Description: msg, Timeout: 4 * time.Second})
		return
	}
	kind, of := b.naming, b.nameOf
	b.naming = ""
	a.focusReq = p
	client := a.client
	if client == nil {
		return
	}
	switch kind {
	case "file":
		path := joinPath(of, name)
		a.fileOp("create "+name, func() (string, error) { return path, client.CreateFile(path) }, p, of, func() {
			if t.Kind == paneTerminal {
				a.peekFromPanel(t, path)
			} else {
				a.openFile(t, path, false)
			}
		})
	case "folder":
		path := joinPath(of, name)
		a.fileOp("create "+name, func() (string, error) { return path, client.Mkdir(path) }, p, of, nil)
	case "rename":
		if name == baseName(of) {
			return
		}
		to := joinPath(dirName(of), name)
		a.fileOp("rename "+baseName(of), func() (string, error) { return to, client.Rename(of, to) }, p, dirName(of), func() {
			a.pathMoved(of, to)
		})
	}
}

// fileOp runs an operation on the host in the background, which answers
// the path it made; then the browser reads dir again and chooses that
// path, and done runs. A failure shows in a toast.
func (a *App) fileOp(what string, op func() (string, error), p *Pane, dir string, done func()) {
	go func() {
		show, err := op()
		a.update(func() {
			if err != nil {
				a.toast(ui.Toast{ID: "files/op", Title: "Couldn't " + what, Description: opError(err), Timeout: 6 * time.Second})
				return
			}
			if p.files != nil {
				a.refreshDir(p, dir)
				if show != "" {
					p.files.reveal = show
				}
			}
			if done != nil {
				done()
			}
			a.changed()
		})
	}()
}

// opError is a failed operation's error as the user reads it: an old
// server's, which has no file operations, says what to do.
func opError(err error) string {
	if strings.Contains(err.Error(), "unknown op") {
		return "The session server is from an older GoRex. Use Restart Session Server… to replace it."
	}
	return err.Error()
}

// pathMoved has the files open in editors and the popup follow a rename
// or a move, those inside a folder too.
func (a *App) pathMoved(from, to string) {
	move := func(e *fileView) {
		if e == nil || e.path == "" {
			return
		}
		switch {
		case samePath(e.path, from):
			e.path = to
		case underDir(e.path, from):
			e.path = joinPath(to, strings.TrimLeft(e.path[len(from):], `/\`))
		}
	}
	for _, t := range a.tabs {
		for _, q := range t.panes() {
			if q.Kind == paneEditor {
				move(q.edit)
			}
		}
	}
	if pk := a.peek; pk != nil {
		move(pk.view)
	}
}

// copyName is the ith name for a copy of base: base itself, then "name
// copy.ext", "name copy 2.ext", as Finder and VS Code name them; a
// folder's name has no extension.
func copyName(base string, dir bool, i int) string {
	ext := ""
	if j := strings.LastIndex(base, "."); j > 0 && !dir {
		ext = base[j:]
	}
	stem := strings.TrimSuffix(base, ext)
	switch i {
	case 0:
		return base
	case 1:
		return stem + " copy" + ext
	}
	return fmt.Sprintf("%s copy %d%s", stem, i, ext)
}

// copyFree copies from into dir under the first of its copy names, from
// the first, that the host has free, and answers the path it made. The
// browser's listing may be behind the host's, so a name the host says
// exists moves on to the next.
func copyFree(client *rex.Client, from, dir string, isDir bool, first int) (string, error) {
	for i := first; i < first+100; i++ {
		to := joinPath(dir, copyName(baseName(from), isDir, i))
		err := client.Copy(from, to)
		if err != nil && strings.Contains(err.Error(), "already exists") {
			continue
		}
		return to, err
	}
	return "", fmt.Errorf("no free name for a copy of %s", baseName(from))
}

// duplicate copies item beside itself, under a name of its own.
func (a *App) duplicate(p *Pane, item string) {
	client := a.client
	if client == nil {
		return
	}
	dir, isDir := dirName(item), p.files.isDir[item]
	a.fileOp("duplicate "+baseName(item), func() (string, error) { return copyFree(client, item, dir, isDir, 1) }, p, dir, nil)
}

// clip cuts or copies item to GoRex's clipboard.
func (a *App) clip(item string, cut bool) {
	a.fileClip = &fileClip{path: item, cut: cut, client: a.client}
	what := "Copied "
	if cut {
		what = "Cut "
	}
	a.toast(ui.Toast{ID: "files/clip", Title: what + baseName(item), Description: "Paste it in a folder of this host's files.", Timeout: 3 * time.Second})
}

// canPaste reports whether the clipboard has a file of this window's host.
func (a *App) canPaste() bool {
	return a.fileClip != nil && a.fileClip.client == a.client && a.client != nil
}

// paste puts the clipboard's file in item's folder: moved when it was
// cut, which empties the clipboard, else copied under a free name.
func (a *App) paste(p *Pane, item string) {
	if !a.canPaste() {
		return
	}
	cl, client := a.fileClip, a.client
	dir := opsDir(p.files, item)
	if cl.cut {
		if samePath(dirName(cl.path), dir) {
			return
		}
		to := joinPath(dir, baseName(cl.path))
		from := cl.path
		a.fileClip = nil
		a.fileOp("move "+baseName(from), func() (string, error) { return to, client.Rename(from, to) }, p, dir, func() {
			a.refreshDir(p, dirName(from))
			a.pathMoved(from, to)
		})
		return
	}
	from, isDir := cl.path, p.files.isDir[cl.path]
	a.fileOp("paste "+baseName(from), func() (string, error) { return copyFree(client, from, dir, isDir, 0) }, p, dir, nil)
}

// trashName is the trash as this host's system calls it.
func trashName() string {
	if hostGOOS == "windows" {
		return "Recycle Bin"
	}
	return "Trash"
}

// trash moves item to the host's trash, asking first. Where the host has
// no trash for it, it offers to delete it for good, asking again.
func (a *App) trash(p *Pane, item string) {
	client := a.client
	if client == nil {
		return
	}
	name := baseName(item)
	if a.ask("Move "+name+" to the "+trashName()+"?", "You can restore it from the "+trashName()+".", "Move to "+trashName(), "Cancel") != 0 {
		return
	}
	dir := dirName(item)
	go func() {
		err := client.Trash(item)
		a.update(func() {
			switch {
			case err != nil && err.Error() == rex.ErrNoTrash:
				if a.ask(name+" can't go to the "+trashName(), "This host has no "+trashName()+" for it. Delete it for good? This can't be undone.", "Delete", "Cancel") == 0 {
					a.fileOp("delete "+name, func() (string, error) { return "", client.Delete(item) }, p, dir, nil)
				}
			case err != nil:
				a.toast(ui.Toast{ID: "files/op", Title: "Couldn't move " + name + " to the " + trashName(), Description: opError(err), Timeout: 6 * time.Second})
			default:
				a.refreshDir(p, dir)
				a.changed()
			}
		})
	}()
}

// deleteItem deletes item for good, asking first.
func (a *App) deleteItem(p *Pane, item string) {
	client := a.client
	if client == nil {
		return
	}
	name := baseName(item)
	if a.ask("Delete "+name+" for good?", "This can't be undone. Move to "+trashName()+" keeps it in the "+trashName()+" instead.", "Delete", "Cancel") != 0 {
		return
	}
	a.fileOp("delete "+name, func() (string, error) { return "", client.Delete(item) }, p, dirName(item), nil)
}

// opsMenu adds the file operations to the menu of a browser's row.
func (a *App) opsMenu(m *ui.Menu, p *Pane, item string) {
	m.Separator()
	if m.Item("New File…").Chosen() {
		a.laterService(func() { a.startNaming(p, "file", item) })
	}
	if m.Item("New Folder…").Chosen() {
		a.laterService(func() { a.startNaming(p, "folder", item) })
	}
	root := samePath(item, p.files.root)
	if m.Item("Rename…").Disabled(root).Chosen() {
		a.laterService(func() { a.startNaming(p, "rename", item) })
	}
	if m.Item("Duplicate").Disabled(root).Chosen() {
		a.laterService(func() { a.duplicate(p, item) })
	}
	m.Separator()
	if m.Item("Cut").Disabled(root).Chosen() {
		a.laterService(func() { a.clip(item, true) })
	}
	if m.Item("Copy").Chosen() {
		a.laterService(func() { a.clip(item, false) })
	}
	if m.Item("Paste").Disabled(!a.canPaste()).Chosen() {
		a.laterService(func() { a.paste(p, item) })
	}
	m.Separator()
	if m.Item("Move to " + trashName() + "…").Disabled(root).Chosen() {
		a.laterService(func() { a.trash(p, item) })
	}
	if m.Item("Delete…").Disabled(root).Chosen() {
		a.laterService(func() { a.deleteItem(p, item) })
	}
}

// opsKeys are the file operations' keys while a browser's tree has the
// focus: Cmd+N and Cmd+Shift+N (Ctrl elsewhere) a new file or folder, F2
// rename, Cmd+X, C and V, and Delete, or Cmd+Backspace, the trash.
func (a *App) opsKeys(c *ui.Context, p *Pane, item string) {
	b := p.files
	root := item == "" || samePath(item, b.root)
	switch {
	case c.Shortcut(ui.Cmd, ui.KeyN):
		a.startNaming(p, "file", item)
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyN):
		a.startNaming(p, "folder", item)
	case c.Shortcut(0, ui.KeyF2) && !root:
		a.startNaming(p, "rename", item)
	case c.Shortcut(ui.Cmd, ui.KeyX) && !root:
		a.clip(item, true)
	case c.Shortcut(ui.Cmd, ui.KeyC) && item != "":
		a.clip(item, false)
	case c.Shortcut(ui.Cmd, ui.KeyV):
		a.paste(p, item)
	case (c.Shortcut(0, ui.KeyDelete) && runtime.GOOS != "darwin" || c.Shortcut(ui.Cmd, ui.KeyBackspace)) && !root:
		item := item
		a.laterService(func() { a.trash(p, item) })
	}
}
