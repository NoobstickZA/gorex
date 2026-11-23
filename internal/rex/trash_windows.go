package rex

import (
	"errors"
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOperationW = shell32.NewProc("SHFileOperationW")
)

// shFileOpStruct is SHFILEOPSTRUCTW, laid out as on 64-bit Windows.
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 3
	fofSilent         = 0x0004
	fofNoConfirmation = 0x0010
	fofAllowUndo      = 0x0040
	fofNoErrorUI      = 0x0400
)

// moveToTrash moves path to the Recycle Bin, through the shell, without
// its dialogs. Only fixed drives have one: on others, as network drives,
// the shell would delete the file for good without asking. Nor can paths
// longer than MAX_PATH go there.
func moveToTrash(path string) error {
	if len(path) >= windows.MAX_PATH {
		return errors.New(ErrNoTrash)
	}
	root, err := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
	if err != nil || windows.GetDriveType(root) != windows.DRIVE_FIXED {
		return errors.New(ErrNoTrash)
	}
	// pFrom is a list of paths, each ending with a NUL, and the list with
	// another.
	from, err := windows.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0)
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}
	if r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op))); r != 0 {
		return fmt.Errorf("the Recycle Bin refused %s (error %#x)", filepath.Base(path), r)
	}
	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("moving %s to the Recycle Bin was stopped", filepath.Base(path))
	}
	return nil
}
