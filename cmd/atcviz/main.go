// Command atcviz serves the viewer for the Clearway cell simulation on this
// computer (package viz).
//
//	go run ./cmd/atcviz
//	open http://127.0.0.1:8787
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/teddyknox/clearway/internal/viz"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "address to listen on")
	webDir := flag.String("web", "", "serve the page from this directory in place of the built-in copy")
	export := flag.String("export", "", "write the viewer to this directory as files for a web host, with one recorded run of each scenario, and stop")
	flag.Parse()
	if *export != "" {
		if err := viz.Export(*export); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("Clearway viewer: http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, viz.Handler(*webDir)))
}
