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
	Root      fs.FS   // the documentation root handle
	SpecPath  string  // relative to Root
	DocsPath  string  // the content directory, relative to Root, or empty
	HideTryIt bool    // hides the Try It console of the viewer page
	Chat      []Route // the chat page's routes, or none
}

// Route is a mux pattern and its handler.
type Route struct {
	Pattern string
	Handler http.Handler
}

// New builds the portal, or refuses a spec path outside the documentation
// root, a content directory path that names no directory in it, or missing
// Elements assets.
func New(cfg Config) (http.Handler, error) {
	docs, err := checkConfig(cfg)
	if err != nil {
		return nil, err
	}
	assets, err := loadAssets()
	if err != nil {
		return nil, err
	}
	return newRouter(cfg, docs, assets), nil
}

// checkConfig checks the portal configuration and opens its content
// directory, so that the spec path and the content directory stay inside the
// documentation root. Without a content directory path it returns nil.
func checkConfig(cfg Config) (fs.FS, error) {
	if cfg.Root == nil {
		return nil, errors.New("no documentation root")
	}
	if cfg.SpecPath == "." || !fs.ValidPath(cfg.SpecPath) {
		return nil, fmt.Errorf("spec path %q is not a file inside the documentation root", cfg.SpecPath)
	}
	if cfg.DocsPath == "" {
		return nil, nil
	}
	docs, err := fs.Sub(cfg.Root, cfg.DocsPath)
	var info fs.FileInfo
	if err == nil {
		info, err = fs.Stat(docs, ".")
	}
	if err != nil {
		return nil, fmt.Errorf("content directory path %q: %w", cfg.DocsPath, err)
	}
	if !info.IsDir() || !directory(cfg.Root, cfg.DocsPath) {
		return nil, fmt.Errorf("content directory path %q is not a directory reached through no symlink", cfg.DocsPath)
	}
	return docs, nil
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
// raw spec, the Elements assets, with a content directory the document list, a
// document page or a raw file, and with a chat its routes.
func newRouter(cfg Config, docs, assets fs.FS) http.Handler {
	s := &site{root: cfg.Root, specPath: cfg.SpecPath, docsPath: cfg.DocsPath, docs: docs, hideTryIt: cfg.HideTryIt, chat: len(cfg.Chat) > 0}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /specs/{path...}", s.viewerPage)
	mux.HandleFunc("GET /api/specs/{path...}", s.rawSpec)
	mux.Handle("GET /assets/elements/", http.StripPrefix("/assets/elements/", http.FileServerFS(assets)))
	if docs != nil {
		mux.HandleFunc("GET /docs/{$}", s.docList)
		mux.HandleFunc("GET /docs/{path...}", s.docPage)
		mux.HandleFunc("GET /raw/{path...}", s.rawFile)
	}
	for _, r := range cfg.Chat {
		mux.Handle(r.Pattern, r.Handler)
	}
	return mux
}
