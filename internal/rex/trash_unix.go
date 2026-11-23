//go:build !windows && !darwin

package rex

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// moveToTrash moves path to the user's trash as the freedesktop.org
// specification has it, which desktops show and restore from: the file
// in Trash/files, and a .trashinfo in Trash/info telling where it was.
// Only a trash that is there already is used: a server without a desktop
// has none, and one made there would fill up unseen. A file on another
// filesystem than the trash's has none either.
func moveToTrash(path string) error {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return errors.New(ErrNoTrash)
		}
		data = filepath.Join(home, ".local", "share")
	}
	trash := filepath.Join(data, "Trash")
	if fi, err := os.Stat(trash); err != nil || !fi.IsDir() {
		return errors.New(ErrNoTrash)
	}
	files, info := filepath.Join(trash, "files"), filepath.Join(trash, "info")
	if err := os.MkdirAll(files, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(info, 0o700); err != nil {
		return err
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	text := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		(&url.URL{Path: path}).EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
	for i := 1; i < 1000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s.%d%s", stem, i, ext)
		}
		// The info file, made first and only if new, claims the name.
		infoPath := filepath.Join(info, name+".trashinfo")
		f, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		_, werr := f.WriteString(text)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(infoPath)
			return werr
		}
		if err := os.Rename(path, filepath.Join(files, name)); err != nil {
			os.Remove(infoPath)
			if errors.Is(err, syscall.EXDEV) {
				return errors.New(ErrNoTrash)
			}
			return err
		}
		return nil
	}
	return errors.New(ErrNoTrash)
}
