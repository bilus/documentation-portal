package portal

import "net/http"

// docList writes the document list: a link to the document page of every
// markdown file in the content directory, sorted by path, or a 404 without a
// content directory.
func (s *site) docList(w http.ResponseWriter, r *http.Request) {
	// HOLE(1): list the markdown files that are not hidden, by path; 404 without s.docs
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<a href="/docs/guide/intro.md">guide/intro.md</a>`))
}

// docPage writes the document page of the markdown file at the request's
// path, or a 404 error page naming the path, also without a content
// directory.
func (s *site) docPage(w http.ResponseWriter, r *http.Request) {
	// HOLE(2): render it without active content, images via /raw/; else, or without s.docs, 404
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<h1 id="introduction">Introduction</h1>`))
}

// rawFile writes the image file at the request's path with the image type
// of its extension and headers that stop the browser from running it, or a
// 404 for any other file or without a content directory.
func (s *site) rawFile(w http.ResponseWriter, r *http.Request) {
	// HOLE(3): serve the image with nosniff and a sandbox CSP; else, or without s.docs, 404
}

// sidebarGroup is one group of the document sidebar: the markdown files of
// one directory, under the directory's path.
type sidebarGroup struct {
	Title string // empty for the top of the content directory
	Links []sidebarLink
}

// sidebarLink links the document page of one markdown file.
type sidebarLink struct {
	Title   string
	URL     string
	Current bool
}

// sidebar returns the document sidebar: the markdown files that are not
// hidden, grouped by directory, with the file at current marked.
func (s *site) sidebar(current string) []sidebarGroup {
	// HOLE(4): group the markdown files by directory, marking current; nil without s.docs
	return nil
}
