package portal

import (
	"io/fs"
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
	doc, ok := s.contentPath(target)
	if !ok || s.docs == nil {
		return "", false
	}
	paths, err := markdownFiles(s.docs)
	return doc, err == nil && slices.Contains(paths, doc)
}

// contentPath returns target, a path inside the documentation root, as a
// path in the content directory, or false for a target outside it or
// without a content directory.
func (s *site) contentPath(target string) (string, bool) {
	target = path.Clean(target)
	switch s.docsPath {
	case "":
		return "", false
	case ".":
		return target, true
	}
	return strings.CutPrefix(target, s.docsPath+"/")
}

// specURL returns the viewer page URL for target, a path inside the
// documentation root that names the configured spec or, in Stoplight's form,
// a part of it: at the operation route for an operation of the published
// spec, else at the overview. It reports false for any other target, and
// while no regular file is at the spec path.
func (s *site) specURL(target string) (string, bool) {
	pointer, ok := s.specPart(target)
	if !ok {
		return "", false
	}
	if info, err := fs.Stat(s.root, s.specPath); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	var fragment string
	if pointer != "" {
		if sp, err := s.loadSpec(s.specPath); err == nil {
			fragment, _ = operationRoute(sp.Raw, pointer)
		}
	}
	return s.viewerURL(fragment), true
}

// specPart reports whether target, a path inside the documentation root,
// names the configured spec or, in Stoplight's form, a part of it, and
// returns the part's pointer, or "" for the spec itself.
func (s *site) specPart(target string) (string, bool) {
	target = path.Clean(target)
	if pointer, inside := strings.CutPrefix(target, s.specPath+"/"); inside {
		return pointer, true
	}
	return "", target == s.specPath
}

// viewerURL returns the viewer page's URL with fragment, an operation route
// or "".
func (s *site) viewerURL(fragment string) string {
	return (&url.URL{Path: "/specs/" + s.specPath, Fragment: fragment}).String()
}

// operationRoute returns the operation route of pointer in spec, such as
// /operations/registerDevice for paths/~1devices/post, or false when spec has
// no such operation.
func operationRoute(spec []byte, pointer string) (string, bool) {
	var doc yaml.Node
	if yaml.Unmarshal(spec, &doc) != nil || len(doc.Content) == 0 {
		return "", false
	}
	return routeIn(doc.Content[0], pointer)
}

// routeIn returns the operation route of pointer in root, the root node of a
// spec, or false when root is nil or has no such operation.
func routeIn(root *yaml.Node, pointer string) (string, bool) {
	parts := strings.Split(pointer, "/")
	if root == nil || len(parts) != 3 || parts[0] != "paths" || !slices.Contains(methods, parts[2]) {
		return "", false
	}
	p := unescapeToken(parts[1])
	op := mappingValue(pathItem(root, p), parts[2])
	if op == nil || op.Kind != yaml.MappingNode {
		return "", false
	}
	return route(op, p, parts[2]), true
}

// pathItem returns the path item of path p in root, the spec's root node,
// following the item's $ref, or nil.
func pathItem(root *yaml.Node, p string) *yaml.Node {
	item := mappingValue(mappingValue(root, "paths"), p)
	// A path item may be a $ref to another; a few hops end any cycle.
	for hops := 0; hops < 8; hops++ {
		ref := mappingValue(item, "$ref")
		if ref == nil {
			break
		}
		item = refTarget(root, ref.Value)
	}
	return item
}

// route returns the operation route of op, the operation of method on path p.
func route(op *yaml.Node, p, method string) string {
	if id := scalarValue(op, "operationId"); id != "" {
		return "/operations/" + id
	}
	return "/paths/" + elementsSlug(p) + "/" + method
}

// scalarValue returns the value of key in the mapping n when it is a scalar
// other than null, or "".
func scalarValue(n *yaml.Node, key string) string {
	if v := mappingValue(n, key); v != nil && v.Kind == yaml.ScalarNode && v.ShortTag() != "!!null" {
		return v.Value
	}
	return ""
}

// unescapeToken turns a token of a JSON pointer back into the key it names.
func unescapeToken(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
}

// escapeToken turns a key into a token of a JSON pointer.
func escapeToken(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// refTarget returns the node in root that ref names, when ref is a reference
// inside the document such as #/components/pathItems/Pets, or nil.
func refTarget(root *yaml.Node, ref string) *yaml.Node {
	pointer, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil
	}
	n := root
	for _, token := range strings.Split(pointer, "/") {
		n = mappingValue(n, unescapeToken(token))
	}
	return n
}

// methods are the keys of the operations in a path item.
var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// mappingValue returns the value of key in the mapping n, or nil. It returns
// an alias as its anchor's node.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if v := n.Content[i+1]; n.Content[i].Value == key {
			if v.Kind == yaml.AliasNode {
				v = v.Alias
			}
			return v
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
