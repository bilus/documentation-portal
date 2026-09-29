package portal

import (
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
)

// docList writes the document list: a link to the document page of every
// markdown file in the content directory, sorted by path, or a 404 without a
// content directory.
func (s *site) docList(w http.ResponseWriter, r *http.Request) {
	if s.docs == nil {
		http.NotFound(w, r)
		return
	}
	paths, err := markdownFiles(s.docs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "list.html", page{Title: "Documents", Paths: paths, Nav: s.nav(), Sidebar: s.sidebar("")})
}

// markdownFiles returns the paths of the markdown files of fsys that are not
// hidden, in byte order.
func markdownFiles(fsys fs.FS) ([]string, error) {
	var paths []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if ext := path.Ext(p); d.IsDir() || ext != ".md" && ext != ".markdown" {
			return nil
		}
		// Stat follows a symlink, and one out of the content directory fails.
		if info, err := fs.Stat(fsys, p); err != nil || !info.Mode().IsRegular() {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	sort.Strings(paths)
	return paths, err
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
