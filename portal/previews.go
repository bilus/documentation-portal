package portal

import (
	"context"
	"net/http"
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
}

// WithPreviews wraps published, the portal handler of the published
// documentation, in the previews handler, which switches readers in and out
// of previews at /previews/ and sends each request to the portal handler of
// the reader's preview or to published. Without cfg.Open, it returns
// published itself.
func WithPreviews(published http.Handler, cfg PreviewsConfig) http.Handler {
	// HOLE(1): published as it is without cfg.Open
	return &previews{published: published, open: cfg.Open, access: cfg.Access}
}

// previews is the previews handler.
type previews struct {
	published http.Handler                                        // the published portal handler
	open      func(context.Context, string) (http.Handler, error) // the portal handler of a preview folder
	access    func(*http.Request) (Access, error)                 // the access hook, or nil
}

// ServeHTTP answers each request: it reads what the request asks of the
// previews, asks the access hook for the reader's access, opens the portal
// handler of the request's folder, and answers.
func (p *previews) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := readPreviewRequest(r)
	access := p.readerAccess(r, req)
	h, err := p.openFolder(r, req, access)
	p.send(w, r, req, h, err)
}

// previewRequest is what a request asks of the previews handler.
type previewRequest struct {
	leave  bool   // at /previews/: to leave the preview
	enter  string // the folder named in a longer /previews/ path, unchecked, or ""
	path   string // with enter, the clean path to open after the switch
	folder string // the reader's preview: the folder of the preview cookie, or ""
	ended  string // the folder of an ended preview, from the notice cookie, or ""
}

// readPreviewRequest reads what r asks of the previews: to leave the preview
// at /previews/, to switch to the folder named in a longer /previews/ path,
// with the path to open after the switch, the reader's preview from the
// preview cookie, and an ended preview from the notice cookie. A cookie
// without a valid folder name counts as none.
func readPreviewRequest(r *http.Request) previewRequest {
	// HOLE(1): read the /previews/ path, the preview cookie and the notice cookie
	return previewRequest{}
}

// readerAccess asks the access hook for the access of r's reader, once, for
// a request with a folder to switch to or a preview to serve, and never for
// a request to leave: Everything without a hook, and an access that allows
// nothing after an error or a nil access, whose cause goes to the log. For
// a request without a folder, it returns nil and asks nothing.
func (p *previews) readerAccess(r *http.Request, req previewRequest) Access {
	// HOLE(1): ask the hook when req has a folder to open
	return nil
}

// openFolder opens the portal handler of the request's folder, the one to
// switch to, else the reader's preview, for a reader whose access allows
// that preview, and none for a request to leave: an error for an invalid
// name, a folder hidden from the reader, a missing folder, or a failed load,
// and nil without a folder. Every error but a failed load's wraps
// fs.ErrNotExist.
func (p *previews) openFolder(r *http.Request, req previewRequest, access Access) (http.Handler, error) {
	// HOLE(1): check the name and the access, and open the folder
	return nil, nil
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
	// HOLE(1): answer each kind of request
	p.published.ServeHTTP(w, r)
}

// banner is the line above the navigation bar of a page: the reader's
// preview, with a link out of it, or the notice of an ended preview.
type banner struct {
	Preview string // the folder of the reader's preview, or ""
	Ended   string // the folder of an ended preview, or ""
}

// previewNotFound writes the 404 page of a /previews/ request that reaches
// the portal handler: Preview not found, with the folder's name.
func previewNotFound(w http.ResponseWriter, r *http.Request) {
	// HOLE(1): the page Preview not found, which names the folder
	http.NotFound(w, r)
}
