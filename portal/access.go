package portal

import (
	"bufio"
	"net"
	"net/http"
)

// Access is the access of one reader: whether the reader may see each portal
// and section of the portal configuration. The access hook returns one for
// each request, and the portal handler asks it about each portal and section
// as the portal configuration gives them, labels included. A section is
// visible when Access allows it and its portal, and a portal when Access
// allows it and at least one of its sections.
type Access interface {
	// Portal reports whether the reader may see the portal p.
	Portal(p Portal) bool
	// Section reports whether the reader may see the section s of the
	// portal p.
	Section(p Portal, s Section) bool
}

// Everything is the access of a reader who may see every portal and
// section: every reader's without an access hook.
var Everything Access = everything{}

type everything struct{}

func (everything) Portal(Portal) bool           { return true }
func (everything) Section(Portal, Section) bool { return true }

// nothing is the access of a reader who may see no portal: every reader's
// after an error from the access hook.
type nothing struct{}

func (nothing) Portal(Portal) bool           { return false }
func (nothing) Section(Portal, Section) bool { return false }

// AccessOf returns the access of r's reader, as the portal handler that
// serves r found it: the access from the access hook, or Everything without
// one. For a request served by no portal handler, it returns an access that
// allows nothing, so that a route of the portal configuration's Chat mounted
// elsewhere shows nothing.
func AccessOf(r *http.Request) Access {
	if access := viewOf(r).access; access != nil {
		return access
	}
	return nothing{}
}

// view is the reader's view of one request, as the router built it: the
// reader's access, and the reader's sites.
type view struct {
	access Access
	sites  []*site // each with only its visible sections, in the order of the portal configuration
}

// viewKey keys the reader's view in the context of a request served by the
// router.
type viewKey struct{}

// viewOf returns the reader's view that the router put in r's context, or a
// view of nothing for a request outside the router and for a nil view.
func viewOf(r *http.Request) *view {
	if v, ok := r.Context().Value(viewKey{}).(*view); ok && v != nil {
		return v
	}
	return &view{access: nothing{}}
}

// portalFor returns the site of the portal whose slug is slug, among the
// reader's sites, or false.
func (v *view) portalFor(slug string) (*site, bool) {
	for _, s := range v.sites {
		if s.slug == slug {
			return s, true
		}
	}
	return nil, false
}

// visibleSites returns the reader's sites for access: of sites, a copy of the
// site of each portal visible to the reader, in order, with only its visible
// sections, and with the others for its portal menu.
func visibleSites(sites []*site, access Access) []*site {
	var visible []*site
	for _, s := range sites {
		if v, ok := s.visibleTo(access); ok {
			visible = append(visible, v)
		}
	}
	for _, v := range visible {
		v.portals = visible
	}
	return visible
}

// visibleTo returns a copy of the site with only the sections visible to the
// reader with access, which keeps the whole site, or false when access is
// nil, hides the portal or hides every section of it.
func (s *site) visibleTo(access Access) (*site, bool) {
	if access == nil || !access.Portal(s.config) {
		return nil, false
	}
	var sections []*section
	for _, sec := range s.sections {
		if access.Section(s.config, sec.config) {
			sections = append(sections, sec)
		}
	}
	if len(sections) == 0 {
		return nil, false
	}
	v := *s
	v.sections = sections
	v.whole = s
	if s.whole != nil {
		v.whole = s.whole
	}
	return &v, true
}

// privateWriter writes each response with Cache-Control: private, even when
// the response's handler deleted the header first, as Go's file server does
// for an error.
type privateWriter struct {
	http.ResponseWriter
	wrote bool // whether the final status went out
}

// newPrivateWriter returns w in a privateWriter, with Cache-Control: private
// set already for a response whose handler writes nothing, whose status
// net/http writes below the privateWriter.
func newPrivateWriter(w http.ResponseWriter) *privateWriter {
	w.Header().Set("Cache-Control", "private")
	return &privateWriter{ResponseWriter: w}
}

// WriteHeader writes the status code with Cache-Control: private. As in
// net/http, an informational status other than 101 leaves the final status
// to a later call.
func (w *privateWriter) WriteHeader(code int) {
	w.Header().Set("Cache-Control", "private")
	if code >= 200 || code == http.StatusSwitchingProtocols {
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write writes b, after the status 200 with Cache-Control: private when no
// final status went out before.
func (w *privateWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// FlushError sends the buffered response to the client, after the status 200
// with Cache-Control: private when no final status went out before, as in
// net/http. http.ResponseController calls it in place of Unwrap.
func (w *privateWriter) FlushError() error {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Flush is FlushError without its error, for a handler that flushes through
// http.Flusher.
func (w *privateWriter) Flush() { _ = w.FlushError() }

// Hijack hands the connection to the caller, for the chat's socket.
func (w *privateWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

// Unwrap returns the response writer under w, for the other methods of
// http.ResponseController, such as its deadlines.
func (w *privateWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
