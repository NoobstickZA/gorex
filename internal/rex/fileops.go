package rex

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The file operations of a file browser: new files and folders, rename,
// copy, the trash and deleting. None replaces a file that exists.

// ErrNoTrash is trash's error where the host has no trash for the path:
// the window may then offer to delete it.
const ErrNoTrash = "no trash"

// errExists refuses an operation whose target is there already.
func errExists(path string) error { return fmt.Errorf("%s already exists", filepath.Base(path)) }

// makeDir answers mkdir: a new folder, refused when the name exists.
func makeDir(path string) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, errExists(path)
		}
		return nil, err
	}
	return nil, nil
}

// createFile answers createFile: a new empty file, refused when the name
// exists.
func createFile(path string) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, errExists(path)
		}
		return nil, err
	}
	return nil, f.Close()
}

// renameFile answers rename: path moves to to, refused when to exists,
// but for the same file under another case of its name, as on Windows
// and macOS.
func renameFile(path, to string) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	if to, err = filePath(to); err != nil {
		return nil, err
	}
	from, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Lstat(to); err == nil && !os.SameFile(from, fi) {
		return nil, errExists(to)
	}
	if from.IsDir() && insideDir(to, path) {
		return nil, fmt.Errorf("can't move %s into itself", filepath.Base(path))
	}
	return nil, os.Rename(path, to)
}

// copyFile answers copy: path, a file or a folder with all inside it,
// copied to to with its permissions, links as links. It refuses when to
// exists, or is inside the folder copied.
func copyFile(path, to string) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	if to, err = filePath(to); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(to); err == nil {
		return nil, errExists(to)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() && insideDir(to, path) {
		return nil, fmt.Errorf("can't copy %s into itself", filepath.Base(path))
	}
	return nil, copyTree(path, to, fi)
}

func copyTree(path, to string, fi fs.FileInfo) error {
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		return os.Symlink(target, to)
	case fi.IsDir():
		if err := os.Mkdir(to, fi.Mode().Perm()|0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				return err
			}
			if err := copyTree(filepath.Join(path, e.Name()), filepath.Join(to, e.Name()), info); err != nil {
				return err
			}
		}
		return os.Chmod(to, fi.Mode().Perm())
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// insideDir reports whether path is dir or inside it.
func insideDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// trashFile answers trash: path goes to the host's trash, which each
// system has its own way (trash_*.go), or ErrNoTrash where it has none
// for it.
func trashFile(path string) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return nil, moveToTrash(abs)
}

// deleteFile answers delete: path and all inside it, gone for good. A
// filesystem's root is refused.
func deleteFile(path string) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	if filepath.Dir(path) == path {
		return nil, fmt.Errorf("won't delete %s", path)
	}
	if _, err := os.Lstat(path); err != nil {
		return nil, err
	}
	return nil, os.RemoveAll(path)
}
