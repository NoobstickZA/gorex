package rex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Upload starts a connection that writes a file of the host's from the
// bytes that follow its line: Size of them, raw, which no other request
// waits behind. The server writes them to a hidden file beside Path and
// renames it to Path once all have come, so that Path is never half
// written; a connection that ends before then leaves nothing. Exec makes
// a new file executable, and a replaced file keeps its own mode; ModTime,
// when set, is the file's modification time. Path must not exist unless
// Replace.
//
// The server answers the line with {"ok":true} before it reads the bytes,
// and once more when the file is in place.
type Upload struct {
	Op      string    `json:"op"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Exec    bool      `json:"exec,omitempty"`
	ModTime time.Time `json:"modTime,omitzero"`
	Replace bool      `json:"replace,omitempty"`
}

// upload serves an upload connection, r holding what came after its line.
func (s *Server) upload(conn net.Conn, r *bufio.Reader, u Upload) {
	defer conn.Close()
	answer := func(err error) {
		res := struct {
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}{OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		b, _ := json.Marshal(res)
		conn.Write(append(b, '\n'))
	}
	tmp, path, err := uploadFile(u)
	if err != nil {
		answer(err)
		return
	}
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	answer(nil)
	if _, err := io.CopyN(tmp, r, u.Size); err != nil {
		answer(fmt.Errorf("the upload ended early: %w", err))
		return
	}
	if err := tmp.Close(); err != nil {
		answer(err)
		return
	}
	if !u.ModTime.IsZero() {
		os.Chtimes(tmp.Name(), time.Now(), u.ModTime)
	}
	if !u.Replace {
		// Another hand made the name since the upload began.
		if _, err := os.Lstat(path); err == nil {
			answer(errExists(path))
			return
		}
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		answer(err)
		return
	}
	done = true
	answer(nil)
}

// uploadFile makes the hidden file an upload writes, beside its path, in
// the mode the file is to have.
func uploadFile(u Upload) (*os.File, string, error) {
	path, err := filePath(u.Path)
	if err != nil {
		return nil, "", err
	}
	if u.Size < 0 {
		return nil, "", errors.New("no size")
	}
	mode := fs.FileMode(0o644)
	if u.Exec {
		mode = 0o755
	}
	switch fi, err := os.Stat(path); {
	case err == nil && fi.IsDir():
		return nil, "", fmt.Errorf("%s is a folder", filepath.Base(path))
	case err == nil && !u.Replace:
		return nil, "", errExists(path)
	case err == nil:
		mode = fi.Mode().Perm()
	case !errors.Is(err, fs.ErrNotExist):
		return nil, "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".gorex-upload-*")
	if err != nil {
		return nil, "", err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, "", err
	}
	return tmp, path, nil
}

// Upload writes size bytes of src to the host's file u.Path through a
// connection of its own, telling progress how many it sent so far.
// Cancelling ctx ends the connection, and the server leaves nothing.
func (c *Client) Upload(ctx context.Context, u Upload, src io.Reader, progress func(sent int64)) error {
	u.Op = "upload"
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	fail := func(err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	b, _ := json.Marshal(u)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return fail(err)
	}
	if err := readOK(conn); err != nil {
		return fail(err)
	}
	buf := make([]byte, 256<<10)
	var sent int64
	for sent < u.Size {
		n, err := src.Read(buf[:min(int64(len(buf)), u.Size-sent)])
		if n > 0 {
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return fail(werr)
			}
			sent += int64(n)
			if progress != nil {
				progress(sent)
			}
		}
		if err == io.EOF && sent < u.Size {
			return fmt.Errorf("the file got shorter while it was uploaded")
		}
		if err != nil && err != io.EOF {
			return fail(err)
		}
	}
	if err := readOK(conn); err != nil {
		return fail(err)
	}
	return nil
}

// Download starts a connection that reads a file of the host's: the
// server answers its line with the file's size, mode and modification
// time ({"ok":true,"size":…}), then sends that many bytes, raw.
type Download struct {
	Op   string `json:"op"`
	Path string `json:"path"`
}

// DownloadInfo is what the server tells of a file before its bytes.
type DownloadInfo struct {
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
	Size    int64     `json:"size"`
	Mode    uint32    `json:"mode"`
	ModTime time.Time `json:"modTime"`
}

// download serves a download connection.
func (s *Server) download(conn net.Conn, d Download) {
	defer conn.Close()
	answer := func(info DownloadInfo) {
		b, _ := json.Marshal(info)
		conn.Write(append(b, '\n'))
	}
	path, err := filePath(d.Path)
	if err != nil {
		answer(DownloadInfo{Error: err.Error()})
		return
	}
	f, err := os.Open(path)
	if err != nil {
		answer(DownloadInfo{Error: err.Error()})
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	switch {
	case err != nil:
		answer(DownloadInfo{Error: err.Error()})
		return
	case !fi.Mode().IsRegular():
		answer(DownloadInfo{Error: filepath.Base(path) + " is not a file"})
		return
	}
	answer(DownloadInfo{OK: true, Size: fi.Size(), Mode: uint32(fi.Mode().Perm()), ModTime: fi.ModTime()})
	io.CopyN(conn, f, fi.Size())
}

// Download writes the host's file path to w through a connection of its
// own, telling progress how many bytes came so far, and returns what the
// server told of the file. Cancelling ctx ends the connection.
func (c *Client) Download(ctx context.Context, path string, w io.Writer, progress func(got int64)) (DownloadInfo, error) {
	var info DownloadInfo
	conn, err := c.dial()
	if err != nil {
		return info, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	fail := func(err error) (DownloadInfo, error) {
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		return info, err
	}
	b, _ := json.Marshal(Download{Op: "download", Path: path})
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return fail(err)
	}
	line, err := readLine(conn)
	if err != nil {
		return fail(err)
	}
	if err := json.Unmarshal(line, &info); err != nil {
		return fail(err)
	}
	if !info.OK {
		// An older server's answer to a request it doesn't know has its
		// error in the same field.
		return info, errors.New(info.Error)
	}
	buf := make([]byte, 256<<10)
	var got int64
	for got < info.Size {
		n, err := conn.Read(buf[:min(int64(len(buf)), info.Size-got)])
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return fail(werr)
			}
			got += int64(n)
			if progress != nil {
				progress(got)
			}
		}
		if err != nil {
			if err == io.EOF && got < info.Size {
				err = io.ErrUnexpectedEOF
			}
			if got < info.Size {
				return fail(err)
			}
		}
	}
	return info, nil
}
