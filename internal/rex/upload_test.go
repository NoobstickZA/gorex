package rex

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestUploadDownload: a file written through an upload connection, with
// its modification time; a name that exists refused unless replaced, the
// replaced file keeping its mode; an upload cancelled half way leaving
// neither the file nor its hidden part; and a file read back through a
// download connection, a folder and a missing file refused.
func TestUploadDownload(t *testing.T) {
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = ""
	}
	dir, err := os.MkdirTemp(base, "rex")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("GOREX_DIR", dir)
	go Serve()
	for range 100 {
		if _, err := os.Stat(filepath.Join(dir, "server.sock")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	c, err := Connect(LocalDialer(Dir()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()
	root := filepath.Join(dir, "files")
	os.Mkdir(root, 0o755)
	leftovers := func() []string {
		var names []string
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".gorex-upload-") {
				names = append(names, e.Name())
			}
		}
		return names
	}

	data := bytes.Repeat([]byte("0123456789"), 100_000) // 1 MB, several writes
	path := filepath.Join(root, "a b.bin")
	mod := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	var last int64
	if err := c.Upload(context.Background(), Upload{Path: path, Size: int64(len(data)), ModTime: mod}, bytes.NewReader(data), func(n int64) { last = n }); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) || last != int64(len(data)) {
		t.Fatalf("wrote %d bytes, progress %d, want %d", len(got), last, len(data))
	}
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(mod) {
		t.Errorf("modification time %v, want %v", fi.ModTime(), mod)
	}

	small := []byte("new\n")
	if err := c.Upload(context.Background(), Upload{Path: path, Size: int64(len(small))}, bytes.NewReader(small), nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("an upload over a file without Replace: %v", err)
	}
	if runtime.GOOS != "windows" {
		os.Chmod(path, 0o600)
	}
	if err := c.Upload(context.Background(), Upload{Path: path, Size: int64(len(small)), Replace: true}, bytes.NewReader(small), nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, small) {
		t.Errorf("replaced: %q", got)
	}
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("the replaced file's mode %v, want 0600", fi.Mode().Perm())
	}

	// Half the bytes, then nothing until cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	half := filepath.Join(root, "half.bin")
	pr, pw := io.Pipe()
	go func() {
		pw.Write(data[:len(data)/2])
		<-ctx.Done()
		pw.CloseWithError(ctx.Err())
	}()
	errc := make(chan error, 1)
	go func() {
		errc <- c.Upload(ctx, Upload{Path: half, Size: int64(len(data))}, pr, func(n int64) {
			if n >= int64(len(data)/2) {
				cancel()
			}
		})
	}()
	if err := <-errc; err != context.Canceled {
		t.Errorf("cancelled upload: %v", err)
	}
	for range 100 {
		if len(leftovers()) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(half); err == nil || len(leftovers()) > 0 {
		t.Errorf("after a cancelled upload: file %v, left %q", err, leftovers())
	}

	// The file uploaded first, downloaded through a connection of its
	// own, with its size and time; a folder and a missing file refused.
	os.WriteFile(path, data, 0o644)
	os.Chtimes(path, mod, mod)
	var got bytes.Buffer
	info, err := c.Download(context.Background(), path, &got, func(n int64) { last = n })
	if err != nil || !bytes.Equal(got.Bytes(), data) || info.Size != int64(len(data)) || !info.ModTime.Equal(mod) || last != int64(len(data)) {
		t.Fatalf("downloaded %d bytes, %+v, %v", got.Len(), info, err)
	}
	for _, p := range []string{root, filepath.Join(root, "missing")} {
		if _, err := c.Download(context.Background(), p, io.Discard, nil); err == nil {
			t.Errorf("downloaded %s", p)
		}
	}
}
