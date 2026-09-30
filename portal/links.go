package portal

import (
	"net/url"
	"path"
	"slices"
	"strings"
)

// linkURL returns the URL of the page that serves the link target of dest, a
// link in the markdown file at docPath, or false when the link leads nowhere.
// It returns dest unchanged when dest has a scheme, a host or no path, or does
// not parse.
func (s *site) linkURL(docPath string, dest []byte) ([]byte, bool) {
	u, err := url.Parse(string(dest))
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return dest, true
	}
	targets := []string{strings.TrimPrefix(path.Clean(u.Path), "/")}
	if !strings.HasPrefix(u.Path, "/") {
		targets = append(targets, path.Join(s.docsPath, path.Dir(docPath), u.Path))
	}
	for _, target := range targets {
		if target == ".." || strings.HasPrefix(target, "../") {
			continue
		}
		if page, ok := s.specURL(target); ok {
			return []byte(page), true
		}
		if doc, ok := s.docAt(target); ok {
			return []byte((&url.URL{Path: "/docs/" + doc, RawQuery: u.RawQuery, Fragment: u.Fragment}).String()), true
		}
	}
	return nil, false
}

// docAt returns the path in the content directory of the markdown file at
// target, a path inside the documentation root, or false when no document
// page serves it.
func (s *site) docAt(target string) (string, bool) {
	doc, ok := target, s.docsPath == "."
	if !ok {
		doc, ok = strings.CutPrefix(target, s.docsPath+"/")
	}
	if !ok || s.docs == nil {
		return "", false
	}
	paths, err := markdownFiles(s.docs)
	return doc, err == nil && slices.Contains(paths, doc)
}

// specURL returns the viewer page URL for target, a path inside the
// documentation root that names the configured spec or, in Stoplight's form,
// a part of it: at the operation route for an operation of the published
// spec, else at the spec's start. It reports false for any other target.
func (s *site) specURL(target string) (string, bool) {
	// HOLE(2): match target against the spec path, and route a Stoplight
	// operation link with operationRoute
	return "", false
}

// operationRoute returns the operation route of pointer in spec, such as
// /operations/registerDevice for paths/~1devices/post, or false when spec has
// no such operation.
func operationRoute(spec []byte, pointer string) (string, bool) {
	// HOLE(2): unescape the path and the method of pointer, and look up the
	// operation's operationId in spec
	return "", false
}
