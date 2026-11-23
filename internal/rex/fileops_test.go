package rex

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestFileOps: the file browser's operations through the server: new
// files and folders, rename (and a case-only one), copy of a folder with
// a link, none replacing a name that exists or putting a folder into
// itself, and delete.
func TestFileOps(t *testing.T) {
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
	p := func(names ...string) string { return filepath.Join(append([]string{root}, names...)...) }
	exists := func(path string) bool { _, err := os.Lstat(path); return err == nil }

	if err := c.Mkdir(p("src")); err != nil {
		t.Fatal(err)
	}
	if err := c.Mkdir(p("src")); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("mkdir over a folder: %v", err)
	}
	if err := c.CreateFile(p("src", "a.txt")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p("src", "a.txt"), []byte("hello"), 0o640)
	if err := c.CreateFile(p("src", "a.txt")); err == nil {
		t.Error("createFile replaced a file")
	}
	if b, _ := os.ReadFile(p("src", "a.txt")); string(b) != "hello" {
		t.Errorf("the file has %q", b)
	}

	// Rename refuses a name that exists, but takes another case of its
	// own.
	os.WriteFile(p("src", "b.txt"), []byte("b"), 0o644)
	if err := c.Rename(p("src", "a.txt"), p("src", "b.txt")); err == nil {
		t.Error("rename replaced b.txt")
	}
	if err := c.Rename(p("src", "a.txt"), p("src", "c.txt")); err != nil || !exists(p("src", "c.txt")) || exists(p("src", "a.txt")) {
		t.Errorf("rename: %v", err)
	}
	if err := c.Rename(p("src", "c.txt"), p("src", "C.txt")); err != nil {
		t.Errorf("case-only rename: %v", err)
	}
	if err := c.Rename(p("src"), p("src", "inner")); err == nil {
		t.Error("rename moved a folder into itself")
	}

	// Copy takes a folder whole, with its permissions and links.
	if runtime.GOOS != "windows" {
		os.Symlink("b.txt", p("src", "link"))
	}
	if err := c.Copy(p("src"), p("copy")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p("copy", "C.txt")); string(b) != "hello" {
		t.Errorf("the copy has %q", b)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(p("copy", "C.txt")); fi == nil || fi.Mode().Perm() != 0o640 {
			t.Errorf("the copy's mode: %v", fi)
		}
		if target, err := os.Readlink(p("copy", "link")); err != nil || target != "b.txt" {
			t.Errorf("the copied link: %q, %v", target, err)
		}
	}
	if err := c.Copy(p("src"), p("copy")); err == nil {
		t.Error("copy replaced a folder")
	}
	if err := c.Copy(p("src"), p("src", "again")); err == nil {
		t.Error("copy put a folder into itself")
	}

	if err := c.Delete(p("copy")); err != nil || exists(p("copy")) {
		t.Errorf("delete: %v", err)
	}
	if err := c.Delete(filepath.VolumeName(root) + string(filepath.Separator)); err == nil {
		t.Error("delete took a filesystem's root")
	}
}

// TestTrash: a file goes to the trash where the user has one, with what
// restores it, and is refused with ErrNoTrash where there is none. On
// Windows the trash is the Recycle Bin, which a test can't keep apart
// from the user's own.
func TestTrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Recycle Bin is the user's own")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	file := filepath.Join(home, "notes.txt")
	os.WriteFile(file, []byte("x"), 0o644)

	if err := moveToTrash(file); err == nil || err.Error() != ErrNoTrash {
		t.Fatalf("without a trash: %v", err)
	}
	trash := filepath.Join(home, ".Trash")
	if runtime.GOOS != "darwin" {
		trash = filepath.Join(home, "data", "Trash")
	}
	os.MkdirAll(trash, 0o700)
	for range 2 {
		os.WriteFile(file, []byte("x"), 0o644)
		if err := moveToTrash(file); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("the file is still there")
		}
	}
	if runtime.GOOS == "darwin" {
		if entries, _ := os.ReadDir(trash); len(entries) != 2 {
			t.Errorf("the trash has %d files", len(entries))
		}
		return
	}
	files, _ := os.ReadDir(filepath.Join(trash, "files"))
	infos, _ := os.ReadDir(filepath.Join(trash, "info"))
	if len(files) != 2 || len(infos) != 2 {
		t.Fatalf("the trash has %d files and %d infos", len(files), len(infos))
	}
	b, _ := os.ReadFile(filepath.Join(trash, "info", "notes.txt.trashinfo"))
	if !strings.HasPrefix(string(b), "[Trash Info]\nPath="+file+"\nDeletionDate=") {
		t.Errorf("the trash info: %q", b)
	}
}
