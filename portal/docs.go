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

// docsRoot redirects to the document list of the docs section with the
// request's slug, or answers 404 for a slug of no docs section.
func (s *site) docsRoot(w http.ResponseWriter, r *http.Request) {
	sec, ok := s.docsFor(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, sec.pageURL(), http.StatusMovedPermanently)
}

// docList writes the document list of the docs section with the request's
// slug: a link to the document page of every markdown file of its content
// directory, sorted by path, or a 404 for a slug of no docs section.
func (s *site) docList(w http.ResponseWriter, r *http.Request) {
	sec, ok := s.docsFor(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	paths, err := markdownFiles(sec.docs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	docs := make([]docLink, 0, len(paths))
	for _, p := range paths {
		docs = append(docs, docLink{Path: p, URL: sec.docURL(p, "", "")})
	}
	render(w, http.StatusOK, "list.html", page{Title: sec.Title, Section: sec.Title, Docs: docs, Nav: s.nav(), Sidebar: s.sidebar(sec, "")})
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
// path in the docs section with the request's slug, or a 404 error page for
// a slug of no docs section and for a path that names no markdown file of the
// section.
func (s *site) docPage(w http.ResponseWriter, r *http.Request) {
	slug, p := r.PathValue("slug"), r.PathValue("path")
	sec, ok := s.docsFor(slug)
	if !ok {
		render(w, http.StatusNotFound, "error.html", page{Title: "Document not found", Message: "No docs section has the slug " + slug + ".", Nav: s.nav()})
		return
	}
	src, err := sec.readDoc(p)
	if errors.Is(err, errNoDoc) {
		render(w, http.StatusNotFound, "error.html", page{Title: "Document not found", Message: "No markdown file at " + p + ".", Nav: s.nav()})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body, err := s.renderMarkdown(sec, src, p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "doc.html", page{Title: p, Section: sec.Title, Body: body, Nav: s.nav(), Sidebar: s.sidebar(sec, p)})
}

// errNoDoc means the request names no markdown file of a content directory.
var errNoDoc = errors.New("no such document")

// readDoc reads the markdown file at p of the section's content directory,
// or returns errNoDoc for any path missing from the section's document list,
// and for a spec section.
func (sec *section) readDoc(p string) ([]byte, error) {
	if sec.docs == nil {
		return nil, errNoDoc
	}
	paths, err := markdownFiles(sec.docs)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(paths, p) {
		return nil, errNoDoc
	}
	return fs.ReadFile(sec.docs, p)
}

var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

var sanitizer = bluemonday.UGCPolicy()

// renderMarkdown renders src, the markdown file at docPath of the docs
// section sec, as HTML without active content, with its relative image
// references under the section's /raw/ URLs and each relative link pointed at
// the page that serves its link target.
func (s *site) renderMarkdown(sec *section, src []byte, docPath string) (template.HTML, error) {
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
			if url := rawImageURL(sec, dir, dest); !bytes.Equal(url, dest) {
				n.Destination = url
			}
		case *ast.Link:
			dest := unescape(n.Destination)
			switch url, ok := s.linkURL(sec, docPath, dest); {
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

// rawImageURL turns an image reference relative to dir, a directory of the
// content directory of the docs section sec, into its raw file URL, and
// returns any other reference unchanged.
func rawImageURL(sec *section, dir string, dest []byte) []byte {
	u, err := url.Parse(string(dest))
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" || strings.HasPrefix(u.Path, "/") {
		return dest
	}
	p := path.Join(dir, u.Path)
	if p == ".." || strings.HasPrefix(p, "../") {
		return dest
	}
	return []byte((&url.URL{Path: sec.base + "/raw/" + sec.slug + "/" + p, RawQuery: u.RawQuery, Fragment: u.Fragment}).String())
}

// rawFile writes the image file at the request's path in the docs section
// with the request's slug, with the image type of its extension and headers
// that stop the browser from running it, or a 404 for any other file and for
// a slug of no docs section.
func (s *site) rawFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	p := r.PathValue("path")
	sec, found := s.docsFor(r.PathValue("slug"))
	ctype, ok := imageTypes[path.Ext(p)]
	if !found || !ok || !fs.ValidPath(p) || strings.HasPrefix(p, ".") || strings.Contains(p, "/.") {
		http.NotFound(w, r)
		return
	}
	if !regularFile(sec.docs, p) {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(sec.docs, p)
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

// sidebarGroup is one group of the document sidebar, under its title: the
// markdown files of one directory, or the links of a group of the toc file.
type sidebarGroup struct {
	Title string // empty for no heading
	Links []sidebarLink
}

// sidebarLink links a page from the document sidebar: a document page, the
// viewer page, or another site.
type sidebarLink struct {
	Title   string
	URL     string
	Current bool
}

// noteTocProblem logs err, the problem of the section's toc file, unless the
// log already named the same problem last, and forgets the last problem when
// err is nil.
func (sec *section) noteTocProblem(err error) {
	problem := ""
	if err != nil {
		problem = err.Error()
	}
	sec.tocMu.Lock()
	defer sec.tocMu.Unlock()
	if problem != "" && problem != sec.tocProblem {
		log.Printf("toc file %s: %s", sec.Toc, problem)
	}
	sec.tocProblem = problem
}

// sidebar returns the document sidebar of the docs section sec, with its file
// at current marked: the entries of the section's toc file when it names
// one, else the section's markdown files that are not hidden, grouped by
// directory. A problem with the toc file gives the markdown files, and a line
// in the log. A spec section has no sidebar.
func (s *site) sidebar(sec *section, current string) []sidebarGroup {
	if sec.docs == nil {
		return nil
	}
	if sec.Toc != "" {
		groups, err := s.tocSidebar(sec, current)
		sec.noteTocProblem(err)
		if err == nil {
			return groups
		}
	}
	paths, err := markdownFiles(sec.docs)
	if err != nil {
		return nil
	}
	byDir := map[string][]sidebarLink{}
	for _, p := range paths {
		dir := path.Dir(p)
		byDir[dir] = append(byDir[dir], sidebarLink{Title: path.Base(p), URL: sec.docURL(p, "", ""), Current: p == current})
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
