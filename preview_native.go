package main

import (
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

// The MyGo preview of an editor pane: MyGo's ui/markdown shows the buffer
// in native UI. It is the preview by default; Settings › Editor › Markdown
// preview makes the web view's the default, and the chip in the header
// switches between them. Links to the web, to headings and to the host's
// files work, and Mermaid diagrams are drawn as in omp's transcripts;
// scroll sync with the editor and raw HTML are the web view's alone.

// modeNative is an editor pane showing the MyGo preview.
const modeNative = "native"

// nativePreview is an editor pane's MyGo preview: the text it shows, of
// the buffer and its version it has, and the view's options, made anew
// for each file, which load its images from its directory.
type nativePreview struct {
	path    string
	buf     *editor.Buffer
	text    string
	version uint64
	due     bool
	opts    *markdown.Options
	// k is the palette of the last frame, which Mermaid's figures take.
	k *colors
}

// switchRenderer moves a view in preview between the web view's preview
// and MyGo's.
func (a *App) switchRenderer(e *fileView) {
	switch e.mode {
	case modePreview:
		e.mode = modeNative
	case modeNative:
		a.showPreview(e, true)
	default:
		return
	}
	e.keys = true
	a.changed()
}

// nativeBody draws the MyGo preview of a view's buffer, which follows
// edits a moment after them, as the web view's does. A file read again,
// as when it changed on disk, is a new buffer, whose versions start
// again: the preview starts again from its text.
func (a *App) nativeBody(c *ui.Context, k *colors, e *fileView, h *fileHost) {
	np := e.native
	if np == nil || np.path != e.path || np.buf != e.buf {
		np = &nativePreview{path: e.path, buf: e.buf, text: e.buf.String(), version: e.buf.Version()}
		np.opts = &markdown.Options{
			LoadImage: a.hostImages(dirName(e.path)),
			OnLink:    func(dest string) { a.nativeLink(e, np, dest) },
			Code: map[string]func(*ui.Context, *markdown.CodeBlockView){"mermaid": func(c *ui.Context, b *markdown.CodeBlockView) {
				a.tspMermaidBlock(c, np.k, nil, b)
			}},
		}
		e.native = np
	}
	np.k = k
	// A heading asked for, by a link that opened the file or one in it,
	// is scrolled to once: kept, every edit would scroll back to it.
	if e.previewFragment != "" {
		np.opts.Anchor, e.previewFragment = e.previewFragment, ""
	}
	if v := e.buf.Version(); v != np.version && !np.due {
		np.due = true
		time.AfterFunc(previewDebounce, func() {
			a.update(func() {
				np.due = false
				if e.native != np || e.buf == nil {
					return
				}
				np.text, np.version = e.buf.String(), e.buf.Version()
				a.changed()
			})
		})
	}
	np.opts.Theme = &markdown.Theme{
		Text: k.text, Muted: k.textMuted, Link: k.busy, CodeFont: a.editorFont().Family,
		// Tables as the web view's: lines that show, a header row and
		// every other row shaded, and columns that take the pane's width
		// before they wrap.
		TableBorder: k.text.Alpha(0.22), TableHeader: k.text.Alpha(0.09), TableStripe: k.text.Alpha(0.04),
		TableCellPadding: [2]float32{0.75, 0.45}, TableMaxColumn: 60,
	}
	s := ui.Scroll(c.Key("native-preview")).Grow(1).MinHeight(0).Background(k.bgBottom).Padding(16, 28).Focusable()
	s.Children(func() {
		markdown.View(c, np.text, np.opts)
	})
	np.opts.Anchor = ""
	a.fileFocus(s, e, h)
}

// nativeLink follows a link clicked in the MyGo preview as the web view's
// page does: one to a heading scrolls there, one to the web opens with the
// system, and one to the host's files opens as the place showing the view
// opens them.
func (a *App) nativeLink(e *fileView, np *nativePreview, dest string) {
	if strings.HasPrefix(dest, "#") {
		np.opts.Anchor = dest
		a.changed()
		return
	}
	if u, err := url.Parse(dest); err == nil && len(u.Scheme) > 1 && u.Scheme != "file" {
		if ui.SafeToOpen(dest) {
			mygo.Shell.OpenExternal(dest)
		}
		return
	}
	target, fragment, _ := strings.Cut(dest, "#")
	if target == "" {
		return
	}
	a.openHostPath(e, hostTarget(dirName(e.path), target), fragment)
}

// hostImages loads the images of a Markdown file of the host's, as the
// web view's preview does through gorex-file: paths relative to its
// directory, read through the window's server. Images on the web are not
// loaded: they show their descriptions.
func (a *App) hostImages(dir string) func(url string) (ui.ImageSource, error) {
	return func(u string) (ui.ImageSource, error) {
		if strings.Contains(u, "://") || strings.HasPrefix(u, "data:") {
			return nil, os.ErrNotExist
		}
		target := hostTarget(dir, u)
		client := a.client
		if client == nil {
			return nil, os.ErrNotExist
		}
		fd, err := client.ReadFile(target, previewFileCap)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(path.Ext(target), ".svg") {
			return ui.ParseSVG(fd.Data)
		}
		return ui.DecodeBitmap(fd.Data)
	}
}

// hostTarget is the host's path a link or an image of a Markdown file in
// dir names: a path relative to dir or absolute, with or without file:,
// its query, fragment and percent escapes taken off, and its . and ..
// resolved, as the web view's page resolves its links.
func hostTarget(dir, u string) string {
	target := strings.TrimPrefix(u, "file:")
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		target = target[:i]
	}
	if p, err := url.PathUnescape(target); err == nil {
		target = p
	}
	if !strings.HasPrefix(target, "/") && !(len(target) > 1 && target[1] == ':') {
		target = joinPath(dir, target)
	}
	if hostGOOS == "windows" {
		target = strings.ReplaceAll(target, `\`, "/")
	}
	if !strings.HasPrefix(target, "//") { // a share's \\server keeps both
		target = path.Clean(target)
	}
	if hostGOOS == "windows" {
		target = strings.ReplaceAll(target, "/", `\`)
	}
	return target
}
