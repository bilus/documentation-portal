package portal

import (
	"context"
	"net/http"
	"strings"
)

// Unpublished returns the handler a program serves in place of the portal
// handler while its documentation source holds no documentation yet, as an
// archive's bucket does before the first upload: every request gets a page
// in the portal's layout saying so, with the reader's account links from
// cfg.Account, a link to the previews, and status 503, sent uncacheable
// since the next check may bring the documentation. The page lives under
// cfg.BasePath like the portal's pages, and takes the custom templates and
// assets of opts, whose other options it ignores. It refuses the custom
// templates that New refuses.
func Unpublished(cfg Config, opts ...Option) (http.Handler, error) {
	var ro renderOptions
	for _, opt := range opts {
		opt(&ro)
	}
	pages, err := pageTemplates(ro.templates)
	if err != nil {
		return nil, err
	}
	base := cfg.BasePath
	notice := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var links []AccountLink
		if cfg.Account != nil {
			links = cfg.Account(r)
		}
		r = r.WithContext(context.WithValue(r.Context(), viewKey{}, &view{access: Everything, account: links, sites: nil, pages: pages}))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Add("Vary", "Cookie")
		render(w, r, http.StatusServiceUnavailable, "error.html", page{
			Title:   "No documentation yet",
			Message: "The documentation has not been published. Try again in a few minutes.",
			Nav:     navBar{Links: []navLink{{Label: "Previews", URL: base + "/previews/"}}},
		})
	})
	h := notice
	if ro.assets != nil {
		assets := customAssets(ro.assets)
		h = func(w http.ResponseWriter, r *http.Request) {
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasPrefix(r.URL.Path, "/assets/custom/") {
				assets.ServeHTTP(w, r)
				return
			}
			notice(w, r)
		}
	}
	return withBasePath(base, h), nil
}
