// Package portal serves API documentation as an http.Handler that other
// programs can mount. The API is not stable yet.
package portal

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// Config says where the portal reads its content from.
type Config struct {
	Specs    fs.FS
	SpecPath string // relative to Specs
}

// New builds the portal, or refuses a spec path outside the specs directory
// or missing Elements assets.
func New(cfg Config) (http.Handler, error) {
	if err := checkConfig(cfg); err != nil {
		return nil, err
	}
	assets, err := loadAssets()
	if err != nil {
		return nil, err
	}
	return newRouter(cfg, assets), nil
}

// checkConfig checks the portal configuration, so that the spec path stays
// inside the specs directory.
func checkConfig(cfg Config) error {
	if cfg.Specs == nil {
		return errors.New("no specs directory")
	}
	if cfg.SpecPath == "." || !fs.ValidPath(cfg.SpecPath) {
		return fmt.Errorf("spec path %q is not a file inside the specs directory", cfg.SpecPath)
	}
	return nil
}

// loadAssets loads the Elements assets, or refuses to start without them.
func loadAssets() (fs.FS, error) {
	return assetsIn(elements)
}

// all: also embeds elements/.gitkeep, so this compiles before `make setup`.
//
//go:embed all:elements
var elements embed.FS

// assetsIn returns the elements directory of fsys, or an error naming each
// Elements asset missing from it.
func assetsIn(fsys fs.FS) (fs.FS, error) {
	dir, err := fs.Sub(fsys, "elements")
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, name := range []string{"web-components.min.js", "styles.min.css"} {
		if _, err := fs.Stat(dir, name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("Elements assets missing (%s): run `make setup` before building", strings.Join(missing, ", "))
	}
	return dir, nil
}

// newRouter builds the router that sends each request to the viewer page, the
// raw spec or the Elements assets.
func newRouter(cfg Config, assets fs.FS) http.Handler {
	// HOLE(4): redirect / to /specs/{spec path}; serve the viewer page at /specs/{spec path}, the raw spec at /api/specs/{spec path} and the assets under /assets/; answer an invalid spec with 422 naming the file and the reason, a missing spec or any other path with 404
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
	return mux
}
