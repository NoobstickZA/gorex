package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// Packet captures (.pcap, .pcapng, .cap, gzipped too) open in View in
// webshark (github.com/hepictel/webshark, resources/webshark), which
// dissects them in the page with Wireshark compiled to WebAssembly. GoRex
// serves its kiosk build at gorex-shark://localhost/webshark/ and answers
// the three requests its API would: the capture's bytes, through a
// download connection, so captures on ssh hosts open where they are; the
// list of captures; and its changes, so a capture still being written,
// as by tcpdump -w, grows in the page.

// sharkOrigin is where the page of webshark is: the scheme's, at
// localhost (http://gorex-shark.localhost on Windows).
var sharkOrigin = "gorex-shark://localhost"

// sharkCaptures are the captures the pages were given, by the name the
// pages know them by, and the size a page was last told of each.
type sharkCaptures struct {
	mu    sync.Mutex
	paths map[string]string
	sizes map[string]int64
}

var sharks = sharkCaptures{paths: map[string]string{}, sizes: map[string]int64{}}

// isCapture reports whether a file is a packet capture by its name.
func isCapture(name string) bool {
	n := strings.TrimSuffix(strings.ToLower(strings.ReplaceAll(name, `\`, "/")), ".gz")
	switch path.Ext(n) {
	case ".pcap", ".pcapng", ".cap":
		return true
	}
	return false
}

// sharkURL is the page that shows the host's capture p: webshark,
// embedded, on the capture's name. Two captures of one name are told
// apart by a number after the second.
func sharkURL(p string) string {
	sharks.mu.Lock()
	defer sharks.mu.Unlock()
	base := baseName(p)
	name := base
	for n := 2; ; n++ {
		if had, ok := sharks.paths[name]; !ok || had == p {
			break
		}
		name = fmt.Sprintf("%d-%s", n, base)
	}
	sharks.paths[name] = p
	delete(sharks.sizes, name) // a page anew is told it as new
	return sharkOrigin + "/webshark/?capture=" + url.QueryEscape(name) + "&embed=1"
}

// sharkPath is the host's path of the capture a page knows as name.
func sharkPath(name string) (string, bool) {
	sharks.mu.Lock()
	defer sharks.mu.Unlock()
	p, ok := sharks.paths[name]
	return p, ok
}

// serveShark answers the requests of webshark's page.
func (a *App) serveShark(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/webshark")
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasPrefix(rest, "/captures/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(rest, "/captures/"))
		a.sharkCapture(w, r, name)
	case rest == "/json" && r.URL.Query().Get("method") == "files":
		sharkFiles(w)
	case rest == "/json":
		// Dissection is the page's own, not the API's.
		http.Error(w, `{"err":1,"errstr":"not supported"}`, http.StatusBadRequest)
	case rest == "/watch":
		a.sharkWatch(w, r)
	default:
		sharkAsset(w, r, strings.TrimPrefix(rest, "/"))
	}
}

// sharkCapture sends a capture's bytes as they come from the host.
func (a *App) sharkCapture(w http.ResponseWriter, r *http.Request, name string) {
	p, ok := sharkPath(name)
	client := a.client
	if !ok || client == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	cw := &countWriter{w: w}
	if _, err := client.Download(r.Context(), p, cw, nil); err != nil {
		log.Printf("webshark: capture %s: %v", p, err)
		if cw.n == 0 {
			http.Error(w, opError(err), http.StatusNotFound)
		}
	}
}

// countWriter counts what it writes, to know whether a response began.
type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n += int64(n)
	return n, err
}

// sharkFiles lists the captures the pages were given, as the API's
// json?method=files does.
func sharkFiles(w http.ResponseWriter) {
	type file struct {
		Name string `json:"name"`
	}
	list := struct {
		Files []file `json:"files"`
		Pwd   string `json:"pwd"`
	}{Files: []file{}, Pwd: "."}
	sharks.mu.Lock()
	for name := range sharks.paths {
		list.Files = append(list.Files, file{Name: name})
	}
	sharks.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// sharkWatch tells the page how its capture changed since it last asked:
// "init" the first time, then "append" as it grows, "full" when it was
// written anew, and nothing while it stays as it was. A custom scheme's
// response reaches the page only once it ends, and holds back the others
// meanwhile, so each answer ends at once, and the page's EventSource asks
// again a second later.
func (a *App) sharkWatch(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("capture")
	p, ok := sharkPath(name)
	client := a.client
	if !ok || client == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, "retry: 1000\n\n")
	fi, err := client.Stat(p)
	if err != nil {
		return
	}
	sharks.mu.Lock()
	prev, seen := sharks.sizes[name]
	sharks.sizes[name] = fi.Size
	sharks.mu.Unlock()
	kind := "none"
	switch {
	case !seen:
		kind = "init"
	case fi.Size < prev:
		kind = "full"
	case fi.Size > prev:
		kind = "append"
	}
	if kind == "none" {
		return
	}
	data, _ := json.Marshal(map[string]any{"size": fi.Size, "mtime": fi.ModTime.UnixMilli(), "kind": kind})
	fmt.Fprintf(w, "event: capture-changed\ndata: %s\n\n", data)
}

// sharkAsset serves a file of webshark's build from the app's resources:
// one kept gzipped, as its WebAssembly is, unpacked as it is sent; a path
// of the page's own routes, its index.html.
func sharkAsset(w http.ResponseWriter, r *http.Request, name string) {
	root, err := sharkRoot()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name = path.Clean("/" + name)[1:]
	if name == "" {
		name = "index.html"
	}
	file := filepath.Join(root, filepath.FromSlash(name))
	if fi, err := os.Stat(file); err == nil && !fi.IsDir() {
		http.ServeFile(w, r, file)
		return
	}
	if f, err := os.Open(file + ".gz"); err == nil {
		defer f.Close()
		zr, err := gzip.NewReader(f)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		io.Copy(w, zr)
		return
	}
	if path.Ext(name) != "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(root, "index.html"))
}

// sharkRoot is the folder of webshark's build: among the app's
// resources; tests replace it.
var sharkRoot = func() (string, error) {
	res, err := mygo.App.Path(mygo.PathResources)
	return filepath.Join(res, "webshark"), err
}
