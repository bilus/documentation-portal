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

// Config says where the portal handler reads its content from, which
// portals it serves, and whether the viewer page hides the Try It console.
// ReadConfig reads one from a configuration file.
type Config struct {
	Root      fs.FS    // the documentation root handle
	Portals   []Portal // in the order of the home page and the portal menu
	HideTryIt bool     // hides the Try It console of the viewer page
	Chat      []Route  // the chat pages' routes, or none
}

// Route is a mux pattern and its handler.
type Route struct {
	Pattern string
	Handler http.Handler
}

// New builds the portal handler, or refuses portals that openPortals
// refuses, or missing Elements assets.
func New(cfg Config) (http.Handler, error) {
	sites, err := openPortals(cfg)
	if err != nil {
		return nil, err
	}
	assets, err := loadAssets()
	if err != nil {
		return nil, err
	}
	return newRouter(cfg, sites, assets)
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

// newRouter builds the router that sends each request to a portal's page by
// the portal's slug: a spec section's viewer page or raw spec, or a docs
// section's document list, document page or raw file, with the document
// sidebar from the section's toc file when it names one; and the home page,
// the Elements assets, and with a chat its routes.
func newRouter(cfg Config, sites []*site, assets fs.FS) (http.Handler, error) {
	// Until stage 1 routes by the portal's slug, the first portal answers at
	// the URLs of #29.
	rt := &router{sites: sites}
	s := sites[0]
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", rt.home)
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

// router sends each request to the site of its portal.
type router struct {
	sites []*site // one for each portal, in the order of the portal configuration
}

// portalFor returns the site of the portal whose slug is slug, or false.
func (rt *router) portalFor(slug string) (*site, bool) {
	// HOLE(1): find the portal
	return nil, false
}

// home redirects to the first section of the only portal, or writes the home
// page, which links the first section of each portal under its name.
func (rt *router) home(w http.ResponseWriter, r *http.Request) {
	// HOLE(1): write the home page. Until then, the first portal's first
	// section.
	rt.sites[0].index(w, r)
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
