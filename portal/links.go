package portal

import (
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// linkURL returns the URL of the page that serves the link target of dest, a
// link in the markdown file at docPath, or false when the link leads nowhere.
// It returns dest unchanged when dest has a scheme, a host or no path, and
// false when dest does not parse.
func (s *site) linkURL(docPath string, dest []byte) ([]byte, bool) {
	u, err := url.Parse(string(dest))
	if err != nil {
		return nil, false
	}
	if u.Scheme != "" || u.Host != "" || u.Path == "" {
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
	target = path.Clean(target)
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
// spec, else at the overview. It reports false for any other target.
func (s *site) specURL(target string) (string, bool) {
	target = path.Clean(target)
	pointer, inside := strings.CutPrefix(target, s.specPath+"/")
	if !inside && target != s.specPath {
		return "", false
	}
	u := url.URL{Path: "/specs/" + s.specPath}
	if inside {
		if sp, err := s.loadSpec(s.specPath); err == nil {
			u.Fragment, _ = operationRoute(sp.Raw, pointer)
		}
	}
	return u.String(), true
}

// operationRoute returns the operation route of pointer in spec, such as
// /operations/registerDevice for paths/~1devices/post, or false when spec has
// no such operation.
func operationRoute(spec []byte, pointer string) (string, bool) {
	parts := strings.Split(pointer, "/")
	if len(parts) != 3 || parts[0] != "paths" || !slices.Contains(methods, parts[2]) {
		return "", false
	}
	p := strings.ReplaceAll(strings.ReplaceAll(parts[1], "~1", "/"), "~0", "~")
	var doc yaml.Node
	if yaml.Unmarshal(spec, &doc) != nil || len(doc.Content) == 0 {
		return "", false
	}
	op := mappingValue(mappingValue(mappingValue(doc.Content[0], "paths"), p), parts[2])
	if op == nil || op.Kind != yaml.MappingNode {
		return "", false
	}
	if id := mappingValue(op, "operationId"); id != nil && id.Kind == yaml.ScalarNode && id.Value != "" {
		return "/operations/" + id.Value, true
	}
	return "/paths/" + elementsSlug(p) + "/" + parts[2], true
}

// methods are the keys of the operations in a path item.
var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// mappingValue returns the value of key in the mapping n, or nil.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

var (
	slugChars = regexp.MustCompile(`[/{}\s]`)
	dashes    = regexp.MustCompile(`-{2,}`)
)

// elementsSlug turns path p into the slug of Stoplight Elements' routes for an
// operation without an operationId.
func elementsSlug(p string) string {
	slug := slugChars.ReplaceAllString(p, "-")
	if run := dashes.FindStringIndex(slug); run != nil {
		slug = slug[:run[0]] + "-" + slug[run[1]:]
	}
	return strings.TrimSuffix(strings.TrimPrefix(slug, "-"), "-")
}
