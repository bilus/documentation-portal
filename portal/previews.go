package portal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
)

// The cookies of the previews handler.
const (
	previewCookie = "portal-preview"       // the folder of the reader's preview
	endedCookie   = "portal-preview-ended" // the folder of an ended preview, for its notice
)

// PreviewAccess is an access that also decides which previews its reader
// may open. With an access hook, the previews handler opens a preview only
// for a reader whose access is a PreviewAccess that allows the preview's
// folder.
type PreviewAccess interface {
	Access
	// Preview reports whether the reader may open the preview of the folder
	// with the given name.
	Preview(folder string) bool
}

// PreviewsConfig holds the opener of the preview folders and the access
// hook of the previews handler.
type PreviewsConfig struct {
	// Open returns the portal handler of the preview folder with the given
	// name, loaded at the folder's first request, or an error, which wraps
	// fs.ErrNotExist for a folder without documentation. Without it,
	// previews are off.
	Open func(ctx context.Context, folder string) (http.Handler, error)

	// Access is the access hook, as in Config: with it, a reader may open a
	// preview when the reader's access is a PreviewAccess that allows the
	// folder. Without it, every reader may open every preview.
	Access func(r *http.Request) (Access, error)

	// BasePath is the base path of the portal handlers, as in Config, so
	// that the previews handler reads /previews/ under it and sends its
	// redirects and cookies there.
	BasePath string
}

// WithPreviews wraps published, the portal handler of the published
// documentation, in the previews handler, which switches readers in and out
// of previews at /previews/ and sends each request to the portal handler of
// the reader's preview or to published. Without cfg.Open, it returns
// published itself.
func WithPreviews(published http.Handler, cfg PreviewsConfig) http.Handler {
	if cfg.Open == nil {
		return published
	}
	return &previews{published: published, open: cfg.Open, access: cfg.Access, base: cfg.BasePath}
}

// previews is the previews handler.
type previews struct {
	published http.Handler                                        // the published portal handler
	open      func(context.Context, string) (http.Handler, error) // the portal handler of a preview folder
	access    func(*http.Request) (Access, error)                 // the access hook, or nil
	base      string                                              // the base path, or ""
}

// ServeHTTP answers each request: it reads what the request asks of the
// previews, asks the access hook for the reader's access, opens the portal
// handler of the request's folder, and answers.
func (p *previews) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := readPreviewRequest(r, p.base)
	access := p.readerAccess(r, req)
	h, err := p.openFolder(r, req, access)
	p.send(w, r, req, h, err)
}

// previewRequest is what a request asks of the previews handler.
type previewRequest struct {
	leave  bool   // at /previews/: to leave the preview
	enter  string // the folder named in a longer /previews/ path, unchecked, or ""
	path   string // with enter, the URL to open after the switch: a clean, escaped path and the query
	folder string // the reader's preview: the folder of the preview cookie, or ""
	ended  string // the folder of an ended preview, from the notice cookie, or ""
}

// readPreviewRequest reads what r asks of the previews: to leave the preview
// at /previews/, to switch to the folder named in a longer /previews/ path,
// with the path to open after the switch, the reader's preview from the
// preview cookie, and an ended preview from the notice cookie. A cookie
// without a valid folder name counts as none.
func readPreviewRequest(r *http.Request, base string) previewRequest {
	var req previewRequest
	// ServeMux unescapes each segment of the escaped path on its own. A path
	// outside the base asks nothing of the previews.
	escaped, under := strings.CutPrefix(r.URL.EscapedPath(), base)
	segments := strings.Split(strings.TrimPrefix(escaped, "/"), "/")
	if first, err := url.PathUnescape(segments[0]); under && err == nil && first == "previews" {
		switch {
		case len(segments) == 1 || len(segments) == 2 && segments[1] == "":
			req.leave = true
		case segments[1] != "":
			req.enter = segments[1]
			if name, err := url.PathUnescape(segments[1]); err == nil {
				req.enter = name
			}
			req.path = cleanTarget(strings.Join(segments[2:], "/"), r.URL.RawQuery)
		}
	}
	if c, err := r.Cookie(previewCookie); err == nil && validFolder(c.Value) {
		req.folder = c.Value
	}
	if c, err := r.Cookie(endedCookie); err == nil && validFolder(c.Value) {
		req.ended = c.Value
	}
	return req
}

// cleanTarget returns the URL of the escaped path p of this site with the
// query, the path cleaned of dot segments and of repeated slashes, which
// would name another host, with a trailing slash kept.
func cleanTarget(p, query string) string {
	clean := path.Clean("/" + p)
	if strings.HasSuffix(p, "/") && clean != "/" {
		clean += "/"
	}
	if query != "" {
		clean += "?" + query
	}
	return clean
}

// validFolder reports whether name is a folder name: one path segment of
// ASCII letters, digits, '.', '_' and '-', other than . and .., so that it
// fits a cookie too.
func validFolder(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, c := range []byte(name) {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// readerAccess asks the access hook for the access of r's reader, once, for
// a request with a folder to switch to or a preview to serve, and never for
// a request to leave: Everything without a hook, and an access that allows
// nothing after an error or a nil access, whose cause goes to the log. For
// a request without a folder, it returns nil and asks nothing.
func (p *previews) readerAccess(r *http.Request, req previewRequest) Access {
	if req.leave || req.enter == "" && req.folder == "" {
		return nil
	}
	if p.access == nil {
		return Everything
	}
	access, err := p.access(r)
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

// openFolder opens the portal handler of the request's folder, the one to
// switch to, else the reader's preview, for a reader whose access allows
// that preview, and none for a request to leave: an error for an invalid
// name, a folder hidden from the reader, a missing folder, or a failed load,
// and nil without a folder. Every error but a failed load's wraps
// fs.ErrNotExist.
func (p *previews) openFolder(r *http.Request, req previewRequest, access Access) (http.Handler, error) {
	folder := req.enter
	if folder == "" {
		folder = req.folder
	}
	if req.leave || folder == "" {
		return nil, nil
	}
	if !validFolder(folder) {
		return nil, fmt.Errorf("preview %q: not a folder name: %w", folder, fs.ErrNotExist)
	}
	if pa, ok := access.(PreviewAccess); !ok || !pa.Preview(folder) {
		return nil, fmt.Errorf("preview %q: hidden from the reader: %w", folder, fs.ErrNotExist)
	}
	h, err := p.open(r.Context(), folder)
	if err == nil && h == nil {
		err = errors.New("no portal handler")
	}
	if err != nil {
		return nil, fmt.Errorf("preview %q: %w", folder, err)
	}
	return h, nil
}

// send answers the request: it leaves the preview, or switches to the opened
// folder with a redirect that sets the preview cookie; sends the request to
// h, the preview's portal handler, with the banner, or after a failure to
// the published portal handler with the notice or the 404 page of the
// folder; sends a request with the notice cookie alone to the published
// portal handler with the notice; each of these privately; and sends a
// request with neither cookie, unchanged, to the published portal handler.
// The cause of a failed load goes to the log.
func (p *previews) send(w http.ResponseWriter, r *http.Request, req previewRequest, h http.Handler, err error) {
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Print(err)
	}
	switch {
	case req.leave:
		uncached(w.Header())
		http.SetCookie(w, expired(previewCookie, p.base))
		http.Redirect(w, r, p.base+"/", http.StatusFound)
	case req.enter != "" && err == nil:
		uncached(w.Header())
		http.SetCookie(w, &http.Cookie{Name: previewCookie, Value: req.enter, Path: cookiePath(p.base), HttpOnly: true, SameSite: http.SameSiteLaxMode})
		if req.ended != "" {
			http.SetCookie(w, expired(endedCookie, p.base))
		}
		http.Redirect(w, r, p.base+req.path, http.StatusFound)
	case req.enter != "":
		// The published portal handler writes the 404 page, and the cookie stays.
		noStore(p.published).ServeHTTP(w, withBanner(r, banner{Preview: req.folder}))
	case req.folder != "" && err == nil:
		noStore(h).ServeHTTP(w, withBanner(r, banner{Preview: req.folder}))
	case req.folder != "":
		http.SetCookie(w, expired(previewCookie, p.base))
		// A page with the notice takes this cookie out of its response.
		http.SetCookie(w, &http.Cookie{Name: endedCookie, Value: req.folder, Path: cookiePath(p.base), HttpOnly: true, SameSite: http.SameSiteLaxMode})
		noStore(p.published).ServeHTTP(w, withBanner(r, banner{Ended: req.folder}))
	case req.ended != "":
		noStore(p.published).ServeHTTP(w, withBanner(r, banner{Ended: req.ended}))
	default:
		p.published.ServeHTTP(w, r)
	}
}

// uncached marks a response that depends on the preview cookie as one no
// cache keeps, the browser's history cache included.
func uncached(h http.Header) {
	h.Set("Cache-Control", "no-store")
	h.Add("Vary", "Cookie")
}

// banner is the line above the navigation bar of a page: the reader's
// preview, with a link out of it, or the notice of an ended preview.
type banner struct {
	Preview string // the folder of the reader's preview, or ""
	Ended   string // the folder of an ended preview, or ""
}

// bannerKey keys the banner in the context of a request from the previews
// handler.
type bannerKey struct{}

// withBanner returns r with the banner b in its context, or r itself for an
// empty b.
func withBanner(r *http.Request, b banner) *http.Request {
	if b == (banner{}) {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), bannerKey{}, b))
}

// bannerOf returns the banner in r's context, or nil.
func bannerOf(r *http.Request) *banner {
	if b, ok := r.Context().Value(bannerKey{}).(banner); ok {
		return &b
	}
	return nil
}

// dropCookie takes the Set-Cookie lines of the cookie name out of h, and
// reports whether h held one.
func dropCookie(h http.Header, name string) bool {
	lines := h.Values("Set-Cookie")
	kept := slices.DeleteFunc(slices.Clone(lines), func(line string) bool { return strings.HasPrefix(line, name+"=") })
	if len(kept) == len(lines) {
		return false
	}
	h.Del("Set-Cookie")
	for _, line := range kept {
		h.Add("Set-Cookie", line)
	}
	return true
}

// expired returns the cookie name with the path /, expired, so that the
// browser drops it.
func expired(name, base string) *http.Cookie {
	return &http.Cookie{Name: name, Path: cookiePath(base), MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

// cookiePath returns the path the previews' cookies are set for: the base
// path with a slash, so that the cookies of a portal under a base path stay
// off the rest of its host.
func cookiePath(base string) string {
	return base + "/"
}

// previewNotFound writes the 404 page of a /previews/ request that reaches
// the portal handler: Preview not found, with the folder's name.
func previewNotFound(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusNotFound, "error.html", page{Title: "Preview not found", Message: "No preview has the folder name " + r.PathValue("folder") + ".", Nav: navBar{Links: []navLink{{Label: "Portals", URL: baseOf(r) + "/"}}}})
}
