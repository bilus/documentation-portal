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

// Config says where the portal reads its content from and whether the viewer
// page hides the Try It console.
type Config struct {
	Root      fs.FS  // the documentation root
	SpecPath  string // relative to Root
	DocsPath  string // the content directory, relative to Root, or empty
	HideTryIt bool   // hides the Try It console of the viewer page
}

// New builds the portal, or refuses a spec path or a content directory path
// that leaves the documentation root, or missing Elements assets.
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

// checkConfig checks the portal configuration, so that the spec path and the
// content directory stay inside the documentation root.
func checkConfig(cfg Config) error {
	if cfg.Root == nil {
		return errors.New("no documentation root")
	}
	if cfg.SpecPath == "." || !fs.ValidPath(cfg.SpecPath) {
		return fmt.Errorf("spec path %q is not a file inside the documentation root", cfg.SpecPath)
	}
	if cfg.DocsPath == "" {
		return nil
	}
	if info, err := fs.Stat(cfg.Root, cfg.DocsPath); !fs.ValidPath(cfg.DocsPath) || err != nil || !info.IsDir() {
		return fmt.Errorf("content directory path %q is not a directory inside the documentation root", cfg.DocsPath)
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

// newRouter builds the router that sends each request to the viewer page, the
// raw spec, the Elements assets and, with a content directory, the document
// list, a document page or a raw file.
func newRouter(cfg Config, assets fs.FS) http.Handler {
	s := &site{root: cfg.Root, specPath: cfg.SpecPath, docsPath: cfg.DocsPath, hideTryIt: cfg.HideTryIt}
	if cfg.DocsPath != "" {
		// Sub fails only on an invalid path, which checkConfig refuses.
		s.docs, _ = fs.Sub(cfg.Root, cfg.DocsPath)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /specs/{path...}", s.viewerPage)
	mux.HandleFunc("GET /api/specs/{path...}", s.rawSpec)
	mux.Handle("GET /assets/elements/", http.StripPrefix("/assets/elements/", http.FileServerFS(assets)))
	if s.docs != nil {
		mux.HandleFunc("GET /docs/{$}", s.docList)
		mux.HandleFunc("GET /docs/{path...}", s.docPage)
		mux.HandleFunc("GET /raw/{path...}", s.rawFile)
	}
	return mux
}
