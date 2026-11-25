package main

import (
	"bytes"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

// Peek, as Tern calls it: a card of a file's start beside a file
// browser's row, which opens as the pointer rests on the row, or on Space
// for the row chosen. The code calls it a glance, as the popup is the
// peek (tsp_files.go). It draws natively: Markdown rendered, code
// highlighted, pictures; other files say what they are. A file is read
// through the server once the card is due, and kept until its folder
// changes.

const (
	glanceDelay = 500 * time.Millisecond
	glanceGrace = 150 * time.Millisecond
	// glanceCap is the most of a text file read, glanceImageCap of a
	// picture's, and glanceLines the lines of code the card shows.
	glanceCap      = 256 << 10
	glanceImageCap = 16 << 20
	glanceLines    = 300
	glanceWidth    = 480
)

// fileGlance is the card showing, or about to: the file, the browser it
// is of, when the pointer came onto the row and when it left the row and
// the card. byKey is a card Space opened, which stays until Space or
// Escape closes it or the choice moves; held a row chosen or opened,
// whose card waits for the pointer to come onto a row again.
type fileGlance struct {
	path        string
	pane        *Pane
	since, left time.Time
	open        bool
	byKey       bool
	held        bool
	overCard    bool
}

// glanceData is what the card shows of a file: Markdown, which a file of
// code is too, fenced; a picture; or, for a file it can't show, what it
// is (info). cut tells that the code was cut to glanceLines.
type glanceData struct {
	loading bool
	err     string
	md      string
	cut     bool
	image   ui.ImageSource
	info    string
	opts    *markdown.Options
}

// glanceRow opens and closes the card of a file's row, drawn as row, as
// the pointer comes and goes, and draws it while it shows. A press, the
// popup, or the choice moving closes it (browserBody).
func (a *App) glanceRow(c *ui.Context, k *colors, t *Tab, p *Pane, item string, row ui.Element) {
	if !is(a.eff.Editor.Peek) || a.peek != nil {
		return
	}
	g := &a.glance
	now := c.Now()
	switch {
	case g.byKey:
		if g.path != item || g.pane != p {
			return
		}
	case row.Hovered():
		if g.path != item || g.pane != p {
			*g = fileGlance{path: item, pane: p, since: now}
		}
		if g.held {
			return
		}
		g.left = time.Time{}
		if !g.open {
			if wait := glanceDelay - now.Sub(g.since); wait > 0 {
				if wait < time.Minute {
					c.After(wait)
				}
				return
			}
			g.open = true
		}
	case g.path != item || g.pane != p:
		return
	case !g.open:
		*g = fileGlance{}
		return
	case g.overCard:
		g.left = time.Time{}
	default:
		if g.left.IsZero() {
			g.left = now
		}
		if wait := glanceGrace - now.Sub(g.left); wait > 0 {
			c.After(wait)
		} else {
			*g = fileGlance{}
			return
		}
	}
	a.drawGlance(c, k, t, p, item, row)
}

// toggleGlance opens the card of the chosen row on Space, or closes it.
func (a *App) toggleGlance(p *Pane, item string) {
	g := &a.glance
	if g.open && g.path == item && g.pane == p {
		*g = fileGlance{}
	} else {
		*g = fileGlance{path: item, pane: p, open: true, byKey: true}
	}
	a.changed()
}

// closeGlance closes the card of browser p, if it shows one.
func (a *App) closeGlance(p *Pane) {
	if a.glance.pane == p {
		a.glance = fileGlance{}
	}
}

// holdGlance closes the card as a row of browser p is chosen or opened,
// and keeps it closed while the pointer stays on that row, as a press
// closes a tooltip.
func (a *App) holdGlance(p *Pane, item string) {
	a.glance = fileGlance{path: item, pane: p, held: true}
}

// drawGlance draws the card of a file beside its row: a header naming the
// file, a click on which opens it as the browser does, and what it shows
// of it, scrolled in the card.
func (a *App) drawGlance(c *ui.Context, k *colors, t *Tab, p *Pane, item string, row ui.Element) {
	d := a.glanceFor(p, item)
	maxH := max(c.Root().Bounds().H*0.6, 160)
	ui.Overlay(c, func() {
		card := ui.Column(c.Key("file-glance")).AttachTo(row, ui.AnchorTopRight, ui.AnchorTopLeft).MarginX(12).
			Width(glanceWidth).MaxHeight(maxH).Radius(10).Clip().Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 8, 24, 0, k.shadowFocused)
		card.Children(func() {
			head := ui.Row(c).Gap(8).Padding(8, 10, 8, 12).AlignItems(ui.Center).Cursor(ui.CursorPointer).
				Role(ui.RoleButton).Label("Open " + baseName(item)).Tooltip("Open")
			head.Children(func() {
				ui.Icon(c, icon("file")).Size(13, 13).TextColor(k.iconMuted).Shrink(0)
				ui.Text(c, relPath(p.files.root, item)).FontSize(12).TextColor(k.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				ui.Text(c, "Peek").FontSize(11).TextColor(k.textMuted).Padding(1, 6).Radius(5).Border(1, k.panelBorder).Shrink(0)
			})
			if head.Clicked() {
				a.glance = fileGlance{}
				if t.Kind == paneTerminal {
					a.peekFromPanel(t, item)
				} else {
					a.openFile(t, item, false)
				}
			}
			ui.Box(c).Height(1).Background(k.panelBorder)
			body := ui.Scroll(c.Key("file-glance-body")).MinHeight(0).Shrink(1).Padding(10, 14, 12, 14)
			body.Children(func() {
				switch {
				case d.loading:
					ui.Text(c, "Loading…").FontSize(12).TextColor(k.textMuted)
				case d.err != "":
					ui.Text(c, d.err).FontSize(12).TextColor(k.textMuted)
				case d.image != nil:
					ui.Image(c, d.image).MaxWidthPercent(100).MaxHeight(maxH - 80)
				case d.md != "":
					d.opts.Theme = &markdown.Theme{Text: k.text, Muted: k.textMuted, Link: k.busy, CodeFont: a.editorFont().Family}
					markdown.View(c, d.md, d.opts)
					if d.cut {
						ui.Text(c, "Showing the first 300 lines.").FontSize(11.5).TextColor(k.textMuted).MarginY(6)
					}
				default:
					ui.Text(c, d.info).FontSize(12).TextColor(k.textMuted)
				}
			})
		})
		a.glance.overCard = card.Hovered()
	})
}

// glanceFor is what the card shows of a file, read from the host once,
// in the background, and kept until its folder changes (dirChanged).
func (a *App) glanceFor(p *Pane, file string) *glanceData {
	if d, ok := a.glances[file]; ok {
		return d
	}
	if a.glances == nil || len(a.glances) > 64 {
		a.glances = map[string]*glanceData{}
	}
	d := &glanceData{loading: true}
	a.glances[file] = d
	about := fileAbout(p.files, file)
	kind := viewable(file)
	client := a.client
	if kind == "capture" {
		d.loading, d.info = false, about+"\nPacket capture. Click the name above to open it in webshark."
		return d
	}
	if kind == "document" || client == nil {
		d.loading, d.info = false, about+"\nNo preview. Click the name above to open it."
		return d
	}
	limit := int64(glanceCap)
	if kind == "image" {
		limit = glanceImageCap
	}
	go func() {
		fd, err := client.ReadFile(file, limit)
		var img ui.ImageSource
		var imgErr error
		if err == nil && kind == "image" {
			if strings.EqualFold(path.Ext(strings.ReplaceAll(file, `\`, "/")), ".svg") {
				img, imgErr = ui.ParseSVG(fd.Data)
			} else {
				img, imgErr = ui.DecodeBitmap(fd.Data)
			}
		}
		a.update(func() {
			d.loading = false
			switch {
			case err != nil && strings.Contains(err.Error(), "too large"):
				d.info = about + "\nToo large to peek. Click the name above to open it."
			case err != nil:
				d.err = "Can't read the file: " + err.Error()
			case kind == "image" && imgErr != nil:
				d.info = about + "\nThis picture can't be shown."
			case kind == "image":
				d.image = img
			case !textual(fd.Data):
				d.info = about + "\nNot a text file."
			default:
				d.md, d.cut = glanceMarkdown(file, string(fd.Data))
				d.opts = &markdown.Options{LoadImage: a.hostImages(dirName(file))}
			}
			a.changed()
		})
	}()
	return d
}

// fileAbout says what a file is: its name, size and when it changed, as
// the browser listed it.
func fileAbout(b *browserState, file string) string {
	name := baseName(file)
	for _, fi := range b.dirs[dirName(file)] {
		if fi.Name == name {
			return name + " · " + sizeText(fi.Size) + " · " + fi.ModTime.Local().Format("2 Jan 2006 15:04")
		}
	}
	return name
}

// textual reports whether data reads as text: no NUL bytes and UTF-8 in
// its first 8 KB, but for a rune the 8 KB cut.
func textual(data []byte) bool {
	head := data[:min(len(data), 8<<10)]
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	for i := 0; i < 3 && len(head) > 0 && !utf8.Valid(head); i++ {
		head = head[:len(head)-1]
	}
	return utf8.Valid(head)
}

// glanceMarkdown is the Markdown the card shows of a text file: a
// Markdown file as it is, and any other as a fenced code block of its
// first glanceLines lines, in its language; cut tells that lines were
// left out.
func glanceMarkdown(file, text string) (md string, cut bool) {
	if isMarkdown(file) {
		return text, false
	}
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > glanceLines {
		text, cut = strings.Join(lines[:glanceLines], ""), true
	}
	lang := ""
	if l := editor.LanguageForText(file, text); l != nil {
		lang = l.Name
		if len(l.Extensions) > 0 {
			lang = l.Extensions[0][1:]
		}
	}
	// The fence is longer than any run of backticks in the code.
	run, longest := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return fence + lang + "\n" + text + fence + "\n", cut
}
