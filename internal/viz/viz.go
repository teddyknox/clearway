// Package viz is the viewer for the Clearway cell simulation, as an HTTP
// handler.
//
// It runs a scenario on request, records it, and sends the recording to a
// page that replays it: aircraft, contracts, proposals and the kernel's
// verdicts, predicted conflicts, the Safety Net, and losses of separation.
// Command atcviz serves it on this computer. Export writes it as files for
// a host that has no simulator.
package viz

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
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

// Handler returns the handler of the viewer. If webDir is not empty, the page is served
// from that directory, so that an edit shows on reload. Otherwise the copy
// built into the binary is served.
func Handler(webDir string) http.Handler {
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

// Export writes the viewer to dir as files that any web server can serve,
// with no simulator behind them: the page, the list of scenarios, and one
// recorded run of each scenario with its own seed and duration. The page
// then offers no other seed and no other duration.
//
// Export also writes vercel.json, which sends the two requests of the page
// to those files.
func Export(dir string) error {
	web, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	if err := os.CopyFS(dir, web); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "runs"), 0o755); err != nil {
		return err
	}
	type fixed struct {
		Scenario
		Fixed bool `json:"fixed"`
	}
	var list []fixed
	for i := range scenarios {
		sc := &scenarios[i]
		rec, _, err := sim.Trace(sc.config(sc.Seed, sc.Minutes))
		if err != nil {
			return fmt.Errorf("scenario %s: %w", sc.ID, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "runs", sc.ID+".json"), mustJSON(rec), 0o644); err != nil {
			return err
		}
		list = append(list, fixed{*sc, true})
	}
	if err := os.WriteFile(filepath.Join(dir, "scenarios.json"), mustJSON(list), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "vercel.json"), []byte(vercelJSON), 0o644)
}

const vercelJSON = `{
  "rewrites": [
    { "source": "/api/scenarios", "destination": "/scenarios.json" },
    {
      "source": "/api/run",
      "has": [{ "type": "query", "key": "scenario", "value": "(?<id>[a-z-]+)" }],
      "destination": "/runs/:id.json"
    }
  ]
}
`

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
