// Package portal serves API documentation as an http.Handler that other
// programs can mount. The API is not stable yet.
package portal

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

// Config says where the portal handler reads its content from, which
// portals it serves, whether the viewer page hides the Try It console, and
// which portals and sections each reader may see. ReadConfig reads one from a
// configuration file.
type Config struct {
	Root      fs.FS    // the documentation root handle
	Portals   []Portal // in the order of the home page and the portal menu
	HideTryIt bool     // hides the Try It console of the viewer page
	Chat      []Route  // the chat pages' routes, or none

	// Access is the access hook: it returns the access of r's reader, or an
	// error, which hides every portal from the reader and goes to the log.
	// The portal handler calls it once for each request, and marks every
	// response private. Without it, every reader may see every portal and
	// section.
	Access func(r *http.Request) (Access, error)
}

// Route is a mux pattern and its handler.
type Route struct {
	Pattern string
	Handler http.Handler
}

// New builds the portal handler, which answers each request with the pages
// of the portals and sections visible to its reader, or refuses portals that
// openPortals refuses, or missing Elements assets.
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

// newRouter builds the router, whose routes send each request to a portal's
// page by the portal's slug: a spec section's viewer page or raw spec, or a
// docs section's document list, document page or raw file, with the document
// sidebar from the section's toc file when it names one; the home page, the
// Elements assets, and with a chat its routes, each portal's chat page behind
// the check of the other routes of its portal. The router keeps the sites and
// the access hook for its answer to each request.
func newRouter(cfg Config, sites []*site, assets fs.FS) (http.Handler, error) {
	mux := http.NewServeMux()
	rt := &router{sites: sites, access: cfg.Access, mux: mux}
	mux.HandleFunc("GET /{$}", rt.home)
	mux.HandleFunc("GET /portals/{portal}", rt.serve((*site).index))
	mux.HandleFunc("GET /portals/{portal}/{$}", rt.serve((*site).index))
	mux.HandleFunc("GET /portals/{portal}/specs/{slug}", rt.serve((*site).viewerPage))
	mux.HandleFunc("GET /portals/{portal}/api/specs/{slug}", rt.serve((*site).rawSpec))
	mux.HandleFunc("GET /portals/{portal}/docs/{slug}", rt.serve((*site).docsRoot))
	mux.HandleFunc("GET /portals/{portal}/docs/{slug}/{$}", rt.serve((*site).docList))
	mux.HandleFunc("GET /portals/{portal}/docs/{slug}/{path...}", rt.serve((*site).docPage))
	mux.HandleFunc("GET /portals/{portal}/raw/{slug}/{path...}", rt.serve((*site).rawFile))
	// Any other path of a portal: the named 404 for a slug of no portal.
	mux.HandleFunc("GET /portals/{portal}/{rest...}", rt.serve(func(_ *site, w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	mux.Handle("GET /assets/elements/", http.StripPrefix("/assets/elements/", http.FileServerFS(assets)))
	for _, r := range cfg.Chat {
		if err := handle(mux, rt.guard(r)); err != nil {
			return nil, err
		}
	}
	return rt, nil
}

// router sends each request to the site of its portal, among the reader's
// sites.
type router struct {
	sites  []*site                             // one for each portal, in the order of the portal configuration
	access func(*http.Request) (Access, error) // the access hook, or nil
	mux    *http.ServeMux                      // the routes of newRouter
}

// ServeHTTP answers each request with the pages of the portals and sections
// visible to its reader: it asks the access hook for the reader's access,
// builds the reader's view, and sends the request with the view through the
// routes.
func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.route(w, r, rt.viewFor(rt.readerAccess(r)))
}

// readerAccess asks the access hook for the access of r's reader, once:
// Everything without a hook, and an access that allows nothing after an
// error or a nil access, whose cause goes to the log.
func (rt *router) readerAccess(r *http.Request) Access {
	if rt.access == nil {
		return Everything
	}
	access, err := rt.access(r)
	switch {
	case err != nil:
		log.Printf("access hook: %v", err)
		return nothing{}
	case access == nil:
		log.Print("access hook: no access and no error")
		return nothing{}
	}
	return access
}

// viewFor builds the reader's view for access: the access and the reader's
// sites, a copy of the site of each portal visible to the reader, with only
// its visible sections, so that every page of the request reads only what is
// visible to the reader.
func (rt *router) viewFor(access Access) *view {
	return &view{access: access, sites: visibleSites(rt.sites, access)}
}

// route sends r with the reader's view v through the routes to its page, and
// with an access hook writes the response with Cache-Control: private.
func (rt *router) route(w http.ResponseWriter, r *http.Request, v *view) {
	var h http.Handler = rt.mux
	if rt.access != nil {
		h = private(h)
	}
	h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), viewKey{}, v)))
}

// guard returns route as it is, or, for a route under /portals/, such as a
// portal's chat page, with a handler that answers as for a slug of no portal
// when the portal of the request's path is hidden from the request's reader.
// It reads the pattern's path as ServeMux does, whatever its method and its
// spacing.
func (rt *router) guard(route Route) Route {
	h := route.Handler
	if h == nil || !strings.HasPrefix(patternPath(route.Pattern), "/portals/") {
		return route
	}
	route.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/portals/"), "/")
		servePortal(w, r, slug, func(_ *site, w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) })
	})
	return route
}

// patternPath returns the path of the ServeMux pattern p, without its method
// and its host, or "" for a pattern without a path.
func patternPath(p string) string {
	fields := strings.Fields(p)
	if len(fields) == 0 {
		return ""
	}
	last := fields[len(fields)-1]
	if i := strings.Index(last, "/"); i >= 0 {
		return last[i:]
	}
	return ""
}

// serve returns the handler that answers a request under
// /portals/{portal}/ with h and the site of the portal, as servePortal does.
func (rt *router) serve(h func(*site, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		servePortal(w, r, r.PathValue("portal"), h)
	}
}

// servePortal answers r with h and the site of the portal whose slug is slug,
// among the sites of r's reader, or with a 404 error page that links the home
// page for a slug of no visible portal.
func servePortal(w http.ResponseWriter, r *http.Request, slug string, h func(*site, http.ResponseWriter, *http.Request)) {
	s, ok := viewOf(r).portalFor(slug)
	if !ok {
		render(w, http.StatusNotFound, "error.html", page{Title: "Portal not found", Message: "No portal has the slug " + slug + ".", Nav: navBar{Links: []navLink{{Label: "Portals", URL: "/"}}}})
		return
	}
	h(s, w, r)
}

// home redirects to the first visible section of the only visible portal, or
// writes the home page, which links the first visible section of each
// visible portal under its name, or says that no portal is open to the
// reader.
func (rt *router) home(w http.ResponseWriter, r *http.Request) {
	sites := viewOf(r).sites
	if len(sites) == 1 {
		sites[0].index(w, r)
		return
	}
	if len(sites) == 0 {
		render(w, http.StatusOK, "home.html", page{Title: "Portals", Message: "No portals are open to you."})
		return
	}
	links := make([]navLink, 0, len(sites))
	for _, s := range sites {
		links = append(links, navLink{Label: s.name, URL: s.url()})
	}
	render(w, http.StatusOK, "home.html", page{Title: "Portals", Portals: links})
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
