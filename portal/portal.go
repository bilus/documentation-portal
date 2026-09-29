// Package portal serves API documentation as an http.Handler that other
// programs can mount. The API is not stable yet.
package portal

import (
	"fmt"
	"io/fs"
	"net/http"
)

// Config says where the portal reads its content from.
type Config struct {
	Specs    fs.FS
	SpecPath string // relative to Specs
}

// New builds the portal, or refuses a spec path outside the specs directory
// or missing Elements assets.
func New(cfg Config) (http.Handler, error) {
	// HOLE(2): refuse a nil Specs or a SpecPath that leaves it, refuse missing Elements assets, then route / to a redirect, /specs/{spec path} to the viewer page, /api/specs/{spec path} to the raw spec and /assets/ to the Elements assets
	mux := http.NewServeMux()
	mux.HandleFunc("GET /specs/{path...}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<elements-api apiDescriptionUrl="/api/specs/%s" router="hash" hideTryIt="true"></elements-api>`, cfg.SpecPath)
	})
	mux.HandleFunc("GET /api/specs/{path...}", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := fs.ReadFile(cfg.Specs, cfg.SpecPath)
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(raw)
	})
	return mux, nil
}
