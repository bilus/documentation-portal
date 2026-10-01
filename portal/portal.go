// Package portal serves API documentation as an http.Handler that other
// programs can mount. The API is not stable yet.
package portal

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// Config says where the portal reads its content from, which sections the
// navigation bar shows, and whether the viewer page hides the Try It console.
// ReadConfig reads one from a configuration file.
type Config struct {
	Root      fs.FS     // the documentation root handle
	Sections  []Section // in the order of the navigation bar
	HideTryIt bool      // hides the Try It console of the viewer page
	Chat      []Route   // the chat page's routes, or none
}

// Route is a mux pattern and its handler.
type Route struct {
	Pattern string
	Handler http.Handler
}

// New builds the portal, or refuses sections that openSections refuses, or
// missing Elements assets.
func New(cfg Config) (http.Handler, error) {
	sections, err := openSections(cfg)
	if err != nil {
		return nil, err
	}
	assets, err := loadAssets()
	if err != nil {
		return nil, err
	}
	return newRouter(cfg, sections, assets)
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
	for _, name := range []string{"web-components.min.js", "styles.min.css", "LICENSE"} {
		if _, err := fs.Stat(dir, name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("Elements assets missing (%s): run `make setup` before building", strings.Join(missing, ", "))
	}
	return dir, nil
}

// newRouter builds the router that sends each request to a section's page: a
// spec section's viewer page or raw spec, or a docs section's document list,
// document page or raw file, with the document sidebar from the section's toc
// file when it names one; and the Elements assets, and with a chat its routes.
func newRouter(cfg Config, sections []*section, assets fs.FS) (http.Handler, error) {
	s := newSite(cfg, sections)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /specs/{slug}", s.viewerPage)
	mux.HandleFunc("GET /api/specs/{slug}", s.rawSpec)
	mux.Handle("GET /assets/elements/", http.StripPrefix("/assets/elements/", http.FileServerFS(assets)))
	mux.HandleFunc("GET /docs/{slug}", s.docsRoot)
	mux.HandleFunc("GET /docs/{slug}/{$}", s.docList)
	mux.HandleFunc("GET /docs/{slug}/{path...}", s.docPage)
	mux.HandleFunc("GET /raw/{slug}/{path...}", s.rawFile)
	for _, r := range cfg.Chat {
		if err := handle(mux, r); err != nil {
			return nil, err
		}
	}
	return mux, nil
}

// handle adds r to mux, and returns as an error what ServeMux.Handle panics
// with: a nil handler, an invalid pattern, or one that conflicts with a route
// already added.
func handle(mux *http.ServeMux, r Route) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("chat route %q: %v", r.Pattern, p)
		}
	}()
	mux.Handle(r.Pattern, r.Handler)
	return nil
}
