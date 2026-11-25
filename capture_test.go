package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCaptureViewable: captures, gzipped ones too, open in webshark; other
// files as before.
func TestCaptureViewable(t *testing.T) {
	for name, want := range map[string]string{
		"/x/dump.pcap": "capture", `C:\x\Dump.PCAPNG`: "capture", "/x/a.cap": "capture",
		"/x/a.pcap.gz": "capture", "/x/a.gz": "", "/x/a.png": "image", "/x/a.txt": "",
	} {
		if got := viewable(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// TestShark: webshark's page gets its capture's bytes from the host by the
// name its URL gives, the same name for the same file and another for a
// file of the same name elsewhere; its watch tells init, then nothing,
// then append as the capture grows and full as it is written anew; and
// its build's files come from the folder, a gzipped one unpacked, a route
// of the page's as its index.html.
func TestShark(t *testing.T) {
	a, _ := newTestApp(t)
	root := testDir(t)
	defer os.RemoveAll(root)
	capture := filepath.Join(root, "dump.pcap")
	os.WriteFile(capture, []byte("0123456789"), 0o644)
	other := filepath.Join(root, "sub", "dump.pcap")
	os.MkdirAll(filepath.Dir(other), 0o755)
	os.WriteFile(other, []byte("other"), 0o644)

	nameOf := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || !strings.HasPrefix(raw, sharkOrigin+"/webshark/?") || u.Query().Get("embed") != "1" {
			t.Fatalf("page %q", raw)
		}
		return u.Query().Get("capture")
	}
	name := nameOf(sharkURL(capture))
	if again := nameOf(sharkURL(capture)); again != name {
		t.Errorf("the same capture was named %q, then %q", name, again)
	}
	otherName := nameOf(sharkURL(other))
	if otherName == name {
		t.Errorf("two captures share the name %q", name)
	}
	get := func(target string) string {
		rec := httptest.NewRecorder()
		a.serveShark(rec, httptest.NewRequest("GET", target, nil))
		return rec.Body.String()
	}
	if got := get("/webshark/captures/" + url.PathEscape(name)); got != "0123456789" {
		t.Errorf("capture %q", got)
	}
	if got := get("/webshark/captures/" + url.PathEscape(otherName)); got != "other" {
		t.Errorf("the other capture %q", got)
	}

	watch := func() string { return get("/webshark/watch?capture=" + url.QueryEscape(name)) }
	kinds := []string{watch(), watch()}
	os.WriteFile(capture, []byte("0123456789abc"), 0o644)
	kinds = append(kinds, watch())
	os.WriteFile(capture, []byte("01"), 0o644)
	kinds = append(kinds, watch())
	for i, want := range []string{`"kind":"init"`, "", `"kind":"append"`, `"kind":"full"`} {
		if want == "" && strings.Contains(kinds[i], "capture-changed") || want != "" && !strings.Contains(kinds[i], want) {
			t.Errorf("watch %d: %q, want %q", i, kinds[i], want)
		}
	}

	build := testDir(t)
	defer os.RemoveAll(build)
	old := sharkRoot
	defer func() { sharkRoot = old }()
	sharkRoot = func() (string, error) { return build, nil }
	os.WriteFile(filepath.Join(build, "index.html"), []byte("<base href=\"/webshark/\">"), 0o644)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	io.WriteString(zw, "wasm bytes")
	zw.Close()
	os.MkdirAll(filepath.Join(build, "assets"), 0o755)
	os.WriteFile(filepath.Join(build, "assets", "x.wasm.gz"), gz.Bytes(), 0o644)
	rec := httptest.NewRecorder()
	a.serveShark(rec, httptest.NewRequest("GET", "/webshark/assets/x.wasm", nil))
	if rec.Body.String() != "wasm bytes" || rec.Header().Get("Content-Type") != "application/wasm" {
		t.Errorf("gzipped file: %q as %q", rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	for _, route := range []string{"/webshark/", "/webshark/embed/rtp"} {
		if got := get(route); !strings.Contains(got, "/webshark/") {
			t.Errorf("%s: %q, want the index", route, got)
		}
	}
	rec = httptest.NewRecorder()
	a.serveShark(rec, httptest.NewRequest("GET", "/webshark/missing.js", nil))
	if rec.Code != 404 {
		t.Errorf("a missing file: %d", rec.Code)
	}
}
