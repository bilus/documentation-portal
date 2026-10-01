package portal

import (
	"bytes"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
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
		// Lstat, not the entry's type bits, rules out a symlink.
		if ext := path.Ext(p); d.IsDir() || ext != ".md" && ext != ".markdown" || !regularFile(fsys, p) {
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
	p := r.PathValue("path")
	src, err := s.readDoc(p)
	if errors.Is(err, errNoDoc) {
		render(w, http.StatusNotFound, "error.html", page{Title: "Document not found", Message: "No markdown file at " + p + ".", Nav: s.nav()})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body, err := s.renderMarkdown(src, p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "doc.html", page{Title: p, Body: body, Nav: s.nav(), Sidebar: s.sidebar(p)})
}

// errNoDoc means the request names no markdown file of the content directory.
var errNoDoc = errors.New("no such document")

// readDoc reads the markdown file at p, or returns errNoDoc for any path
// missing from the document list.
func (s *site) readDoc(p string) ([]byte, error) {
	if s.docs == nil {
		return nil, errNoDoc
	}
	paths, err := markdownFiles(s.docs)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(paths, p) {
		return nil, errNoDoc
	}
	return fs.ReadFile(s.docs, p)
}

var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

var sanitizer = bluemonday.UGCPolicy()

// renderMarkdown renders src, the markdown file at docPath, as HTML without
// active content, with its relative image references under /raw/ and each
// relative link pointed at the page that serves its link target.
func (s *site) renderMarkdown(src []byte, docPath string) (template.HTML, error) {
	doc := markdown.Parser().Parse(text.NewReader(src))
	dir := path.Dir(docPath)
	var nowhere []*ast.Link
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Image:
			dest := unescape(n.Destination)
			if url := rawImageURL(dir, dest); !bytes.Equal(url, dest) {
				n.Destination = url
			}
		case *ast.Link:
			dest := unescape(n.Destination)
			switch url, ok := s.linkURL(docPath, dest); {
			case !ok:
				nowhere = append(nowhere, n)
			case !bytes.Equal(url, dest):
				n.Destination = url
			}
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", err
	}
	for _, link := range nowhere {
		parent := link.Parent()
		for c := link.FirstChild(); c != nil; c = link.FirstChild() {
			parent.InsertBefore(parent, link, c)
		}
		parent.RemoveChild(parent, link)
	}
	var buf bytes.Buffer
	if err := markdown.Renderer().Render(&buf, src, doc); err != nil {
		return "", err
	}
	return template.HTML(sanitizer.SanitizeBytes(buf.Bytes())), nil
}

// unescape resolves the backslash escapes and character references of a link
// destination, as goldmark's renderer does before it writes one.
func unescape(dest []byte) []byte {
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(dest)))
}

// rawImageURL turns an image reference relative to dir into its /raw/ URL,
// and returns any other reference unchanged.
func rawImageURL(dir string, dest []byte) []byte {
	u, err := url.Parse(string(dest))
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" || strings.HasPrefix(u.Path, "/") {
		return dest
	}
	p := path.Join(dir, u.Path)
	if p == ".." || strings.HasPrefix(p, "../") {
		return dest
	}
	return []byte((&url.URL{Path: "/raw/" + p, RawQuery: u.RawQuery, Fragment: u.Fragment}).String())
}

// rawFile writes the image file at the request's path with the image type
// of its extension and headers that stop the browser from running it, or a
// 404 for any other file or without a content directory.
func (s *site) rawFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	p := r.PathValue("path")
	ctype, ok := imageTypes[path.Ext(p)]
	if s.docs == nil || !ok || !fs.ValidPath(p) || strings.HasPrefix(p, ".") || strings.Contains(p, "/.") {
		http.NotFound(w, r)
		return
	}
	if !regularFile(s.docs, p) {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.docs, p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Write(data)
}

// regularFile reports whether p is a regular file of fsys that no symlink
// leads to, in its own name or in the name of a directory above it.
func regularFile(fsys fs.FS, p string) bool {
	info, err := fs.Lstat(fsys, p)
	return err == nil && info.Mode().IsRegular() && directory(fsys, path.Dir(p))
}

// directory reports whether p is a directory of fsys that no symlink leads
// to, in its own name or in the name of a directory above it.
func directory(fsys fs.FS, p string) bool {
	for ; p != "."; p = path.Dir(p) {
		if info, err := fs.Lstat(fsys, p); err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

// imageTypes maps the extension of an image to its Content-Type.
var imageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
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

// sidebar returns the document sidebar, with the file at current marked: the
// entries of the toc file when the configuration names one, else the
// markdown files that are not hidden, grouped by directory. A toc file that
// is missing or invalid gives the markdown files, and a line in the log.
func (s *site) sidebar(current string) []sidebarGroup {
	if s.docs == nil {
		return nil
	}
	if s.tocPath != "" {
		groups, err := s.tocSidebar(current)
		if err == nil {
			return groups
		}
		log.Printf("toc file %s: %v", s.tocPath, err)
	}
	paths, err := markdownFiles(s.docs)
	if err != nil {
		return nil
	}
	byDir := map[string][]sidebarLink{}
	for _, p := range paths {
		dir := path.Dir(p)
		byDir[dir] = append(byDir[dir], sidebarLink{Title: path.Base(p), URL: (&url.URL{Path: "/docs/" + p}).String(), Current: p == current})
	}
	var groups []sidebarGroup
	if links, ok := byDir["."]; ok {
		groups = append(groups, sidebarGroup{Links: links})
		delete(byDir, ".")
	}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		groups = append(groups, sidebarGroup{Title: dir, Links: byDir[dir]})
	}
	return groups
}
