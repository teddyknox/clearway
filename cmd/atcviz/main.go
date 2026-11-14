// Command atcviz serves a viewer for the Clearway cell simulation.
//
// It runs a scenario on request, records it, and sends the recording to a
// page that replays it: aircraft, contracts, proposals and the kernel's
// verdicts, predicted conflicts, the Safety Net, and losses of separation.
//
//	go run ./cmd/atcviz
//	open http://127.0.0.1:8787
package main

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/teddyknox/clearway/internal/sim"
)

//go:embed web
var webFS embed.FS

type cache struct {
	mu    sync.Mutex
	order []string
	data  map[string][]byte
}

func (c *cache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.data[key]
	return b, ok
}

func (c *cache) put(key string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok {
		c.order = append(c.order, key)
	}
	c.data[key] = b
	for len(c.order) > 6 {
		delete(c.data, c.order[0])
		c.order = c.order[1:]
	}
}

// newMux returns the handler. If webDir is not empty, the page is served
// from that directory, so that an edit shows on reload. Otherwise the copy
// built into the binary is served.
func newMux(webDir string) *http.ServeMux {
	web, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	if webDir != "" {
		web = os.DirFS(webDir)
	}
	runs := &cache{data: map[string][]byte{}}
	mux := http.NewServeMux()
	files := http.FileServer(http.FS(web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// A local tool: always revalidate, so a rebuilt page is never stale.
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/scenarios", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, mustJSON(scenarios))
	})
	mux.HandleFunc("/api/run", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sc := scenarioByID(q.Get("scenario"))
		if sc == nil {
			http.Error(w, "unknown scenario", http.StatusNotFound)
			return
		}
		seed, minutes := sc.Seed, sc.Minutes
		if v, err := strconv.ParseUint(q.Get("seed"), 10, 64); err == nil {
			seed = v
		}
		if v, err := strconv.ParseFloat(q.Get("minutes"), 64); err == nil {
			minutes = min(240, max(5, v))
		}
		key := fmt.Sprintf("%s/%d/%g", sc.ID, seed, minutes)
		body, ok := runs.get(key)
		if !ok {
			rec, _, err := sim.Trace(sc.config(seed, minutes))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			body = mustJSON(rec)
			runs.put(key, body)
		}
		writeJSON(w, r, body)
	})
	return mux
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func writeJSON(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		_, _ = w.Write(body)
		return
	}
	w.Header().Set("Content-Encoding", "gzip")
	zw, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	_, _ = zw.Write(body)
	_ = zw.Close()
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "address to listen on")
	webDir := flag.String("web", "", "serve the page from this directory in place of the built-in copy")
	flag.Parse()
	log.Printf("Clearway viewer: http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, newMux(*webDir)))
}
