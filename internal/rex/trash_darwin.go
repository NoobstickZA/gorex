package rex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// moveToTrash moves path into the user's ~/.Trash, under a name of its
// own there, as Finder does but for Put Back, which only Finder offers.
// A file on another volume than the home folder's has no trash here.
func moveToTrash(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return errors.New(ErrNoTrash)
	}
	trash := filepath.Join(home, ".Trash")
	if fi, err := os.Stat(trash); err != nil || !fi.IsDir() {
		return errors.New(ErrNoTrash)
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		name := base
		switch {
		case i == 2:
			name = stem + " " + time.Now().Format("15.04.05") + ext
		case i > 2:
			name = fmt.Sprintf("%s %s %d%s", stem, time.Now().Format("15.04.05"), i, ext)
		}
		to := filepath.Join(trash, name)
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if err := os.Rename(path, to); err != nil {
			if errors.Is(err, syscall.EXDEV) {
				return errors.New(ErrNoTrash)
			}
			return err
		}
		return nil
	}
	return errors.New(ErrNoTrash)
}
