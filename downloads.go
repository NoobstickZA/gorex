package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A file or folder of the file panel or an editor tab's tree drags out of
// the window to a file manager, which copies it: this machine's as it is;
// a host's, on Windows, as virtual files Explorer copies with its own
// progress, reading each through a download connection (rex.Download) as
// it writes it; elsewhere, where MyGo offers no virtual files, as a copy
// GoRex downloads to a temporary folder as the drag starts, which the
// file manager takes once it is whole.

// dragOutLimit is how many files and folders a folder may hold to drag
// out, as each is listed when the drag starts.
const dragOutLimit = 10000

// dragEntry is a file or folder of the host's a drag offers: its path
// there, and its place in the drag.
type dragEntry struct {
	remote, rel string
	dir         bool
	size        int64
	mod         time.Time
}

// dragOut is what dragging item, a row of browser pane p, offers.
func (a *App) dragOut(p *Pane, item string) transfer.Data {
	b := p.files
	if b == nil || a.bind.noFiles() {
		return transfer.Data{}
	}
	if !a.bind.host() {
		d, _ := transfer.FileData(item)
		return d
	}
	client := a.client
	if client == nil {
		return transfer.Data{}
	}
	entries, err := a.dragEntries(client, b, item)
	if err != nil {
		a.toast(ui.Toast{Title: "Can't drag " + baseName(item), Description: opError(err), Type: "error"})
		return transfer.Data{}
	}
	if runtime.GOOS == "windows" {
		files := make([]transfer.VirtualFile, len(entries))
		for i, e := range entries {
			files[i] = transfer.VirtualFile{Path: e.rel, Dir: e.dir, Size: e.size, ModTime: e.mod}
			if !e.dir {
				remote := e.remote
				files[i].Open = func() (io.ReadCloser, error) { return downloadReader(client, remote), nil }
			}
		}
		d, err := transfer.VirtualFiles(files...)
		if err != nil {
			a.toast(ui.Toast{Title: "Can't drag " + baseName(item), Description: err.Error(), Type: "error"})
		}
		return d
	}
	return a.dragCopy(client, item, entries)
}

// dragEntries lists item for a drag: the file, or the folder and all it
// holds, folders before what they hold. A folder of more than
// dragOutLimit entries is refused.
func (a *App) dragEntries(client *rex.Client, b *browserState, item string) ([]dragEntry, error) {
	var fi rex.FileInfo
	found := false
	for _, e := range b.dirs[dirName(item)] {
		if e.Name == baseName(item) {
			fi, found = e, true
			break
		}
	}
	if !found {
		var err error
		if fi, err = client.Stat(item); err != nil {
			return nil, err
		}
	}
	entries := []dragEntry{{remote: item, rel: baseName(item), dir: fi.Dir, size: fi.Size, mod: fi.ModTime}}
	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		fis, err := client.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, fi := range fis {
			if len(entries) >= dragOutLimit {
				return fmt.Errorf("%s holds more than %d files and folders", baseName(item), dragOutLimit)
			}
			e := dragEntry{remote: joinPath(dir, fi.Name), rel: rel + "/" + fi.Name, dir: fi.Dir, size: fi.Size, mod: fi.ModTime}
			entries = append(entries, e)
			if e.dir {
				if err := walk(e.remote, e.rel); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if fi.Dir {
		if err := walk(item, baseName(item)); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// downloadReader reads the host's file remote as it downloads; closing it
// ends the download.
func downloadReader(client *rex.Client, remote string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		_, err := client.Download(context.Background(), remote, pw, nil)
		pw.CloseWithError(err)
	}()
	return pr
}

// dragCopy offers a copy of the drag's entries, downloaded to a folder of
// its own under the temporary folder as the drag starts, as a file list
// the file manager reads when it takes the drop: by then, it waits for
// the copy to be whole. A toast tells how far it is, with Cancel. Copies
// of earlier drags go after a day.
func (a *App) dragCopy(client *rex.Client, item string, entries []dragEntry) transfer.Data {
	root := filepath.Join(os.TempDir(), "gorex-drag")
	os.MkdirAll(root, 0o700)
	if old, err := os.ReadDir(root); err == nil {
		for _, e := range old {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				os.RemoveAll(filepath.Join(root, e.Name()))
			}
		}
	}
	dir, err := os.MkdirTemp(root, "drag-")
	if err != nil {
		return transfer.Data{}
	}
	target := filepath.Join(dir, baseName(item))
	ctx, cancel := context.WithCancel(context.Background())
	var total int64
	for _, e := range entries {
		total += e.size
	}
	var got atomic.Int64
	id := fmt.Sprintf("download/%d", uploadSeq.Add(1))
	done := make(chan struct{})
	var failed error
	go func() {
		defer close(done)
		show := func() {
			a.toast(ui.Toast{ID: id, Title: "Downloading " + baseName(item), Type: "working", Timeout: -1,
				Description: sizeText(got.Load()) + " of " + sizeText(total),
				Data:        toastProgress(float64(got.Load()) / float64(max(total, 1))), Action: "Cancel", OnAction: cancel})
		}
		show()
		last := time.Now()
		for _, e := range entries {
			local := filepath.Join(dir, filepath.FromSlash(e.rel))
			if e.dir {
				if failed = os.MkdirAll(local, 0o755); failed != nil {
					break
				}
				continue
			}
			f, err := os.Create(local)
			if err != nil {
				failed = err
				break
			}
			var before = got.Load()
			info, err := client.Download(ctx, e.remote, f, func(n int64) {
				got.Store(before + n)
				if time.Since(last) > 200*time.Millisecond {
					last = time.Now()
					show()
				}
			})
			f.Close()
			if err != nil {
				failed = fmt.Errorf("%s: %w", baseName(e.remote), err)
				break
			}
			if !info.ModTime.IsZero() {
				os.Chtimes(local, time.Now(), info.ModTime)
			}
		}
		switch {
		case failed != nil && ctx.Err() != nil:
			a.toast(ui.Toast{ID: id, Title: "Download cancelled", Timeout: 3 * time.Second})
		case failed != nil:
			a.toast(ui.Toast{ID: id, Title: "Couldn't download " + baseName(item), Description: opError(failed), Type: "error", Timeout: -1})
		default:
			a.toast(ui.Toast{ID: id, Title: "Downloaded " + baseName(item), Description: sizeText(total), Timeout: 2 * time.Second})
		}
	}()
	var once sync.Once
	var list []byte
	var listErr error
	provider := func() ([]byte, error) {
		once.Do(func() {
			<-done
			if failed != nil {
				listErr = failed
				return
			}
			d, err := transfer.FileData(target)
			if err != nil {
				listErr = err
				return
			}
			list, listErr = d.Read(transfer.FileList)
		})
		return list, listErr
	}
	return transfer.New(transfer.NewItem(transfer.Lazy(transfer.FileList, provider), transfer.Lazy(transfer.URIList, provider)))
}
