package portal

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Library reads what the portal publishes, for a program that answers
// questions about it: the published specs of the spec sections and the listed
// markdown files of the docs sections, each with the URL of its page.
type Library struct{ s *site }

// NewLibrary returns the published documentation of cfg, or the error that
// New gives for cfg.
func NewLibrary(cfg Config) (*Library, error) {
	sections, err := openSections(cfg)
	if err != nil {
		return nil, err
	}
	return &Library{s: newSite(cfg, sections)}, nil
}

// SectionLink links a section's page from the navigation bar.
type SectionLink struct {
	Title string
	URL   string
}

// Sections returns a link to the page of each section, in the order of the
// portal configuration.
func (l *Library) Sections() []SectionLink {
	links := make([]SectionLink, 0, len(l.s.sections))
	for _, sec := range l.s.sections {
		links = append(links, SectionLink{Title: sec.Title, URL: sec.pageURL()})
	}
	return links
}

// Title returns the title of the first spec section's published spec, or ""
// without a spec section and while the spec cannot be read.
func (l *Library) Title() string {
	sec, ok := l.s.firstSpec()
	if !ok {
		return ""
	}
	sp, err := l.s.loadSpec(sec.Input)
	if err != nil {
		return ""
	}
	return sp.Title
}

// Document is a markdown file that a document list shows.
type Document struct {
	Path  string `json:"path"`  // from the documentation root
	Title string `json:"title"` // its first level-one heading, or its path
	URL   string `json:"url"`   // its document page
}

// Documents lists the markdown files that the document lists show, each
// once: a file that two docs sections hold belongs to the first, whose page
// links to it open.
func (l *Library) Documents() ([]Document, error) {
	pages, err := l.pages()
	if err != nil {
		return nil, err
	}
	docs := make([]Document, 0, len(pages))
	for _, pg := range pages {
		docs = append(docs, pg.Document)
	}
	return docs, nil
}

// ReadDocument returns the text of the document page of the markdown file at
// target, a path from the documentation root, in markdown's notation, or an
// error for any path that no document page serves. The text holds only what
// the page shows: no HTML comments, raw HTML or unused link definitions.
func (l *Library) ReadDocument(target string) (string, error) {
	sec, doc, ok := l.s.docAt(target)
	if !ok {
		return "", errNoDoc
	}
	src, err := sec.readDoc(doc)
	if err != nil {
		return "", err
	}
	text, _, err := l.render(sec, src, doc)
	return text, err
}

// docText is the text of a document's page.
type docText struct {
	Document
	text string
}

// pages returns the text of the document page of each markdown file that a
// document list shows, each once, under its path from the documentation
// root: a file that two docs sections hold belongs to the first.
func (l *Library) pages() ([]docText, error) {
	var pages []docText
	seen := map[string]bool{}
	for _, sec := range l.s.sections {
		if sec.Type != DocsSection {
			continue
		}
		paths, err := markdownFiles(sec.docs)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			rooted := path.Join(sec.Input, p)
			if seen[rooted] {
				continue
			}
			seen[rooted] = true
			src, err := fs.ReadFile(sec.docs, p)
			if err != nil {
				return nil, err
			}
			text, title, err := l.render(sec, src, p)
			if err != nil {
				return nil, err
			}
			if title == "" {
				title = rooted
			}
			pages = append(pages, docText{Document{Path: rooted, Title: title, URL: sec.docURL(p, "", "")}, text})
		}
	}
	return pages, nil
}

// render returns the text and the title of the document page of src, the
// markdown file at p of the docs section sec.
func (l *Library) render(sec *section, src []byte, p string) (text, title string, err error) {
	h, err := l.s.renderMarkdown(sec, src, p)
	if err != nil {
		return "", "", err
	}
	text, title = pageText(string(h))
	return text, title, nil
}

// Operation is an operation of a published spec.
type Operation struct {
	Spec        string `json:"spec,omitempty"` // the slug of its spec section
	Method      string `json:"method"`
	Path        string `json:"path"`
	OperationID string `json:"operationId,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Pointer     string `json:"pointer"` // in Stoplight's form, such as paths/~1pets/get
	URL         string `json:"url"`     // the viewer page at the operation
}

// Operations lists the operations of the published spec of each spec
// section, in the order of the sections and of each spec's paths. A spec that
// two spec sections name counts once, for the first, and a spec that does
// not load has no operations: its viewer page shows why.
func (l *Library) Operations() ([]Operation, error) {
	var ops []Operation
	for _, sec := range l.specSections() {
		root, err := l.specRoot(sec)
		if err != nil {
			continue
		}
		paths := mappingValue(root, "paths")
		if paths == nil || paths.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(paths.Content); i += 2 {
			p := paths.Content[i].Value
			item := pathItem(root, p)
			for _, method := range methods {
				op := mappingValue(item, method)
				if op == nil || op.Kind != yaml.MappingNode {
					continue
				}
				ops = append(ops, Operation{
					Spec:        sec.slug,
					Method:      method,
					Path:        p,
					OperationID: scalarValue(op, "operationId"),
					Summary:     scalarValue(op, "summary"),
					Pointer:     "paths/" + escapeToken(p) + "/" + method,
					URL:         sec.viewerURL(route(op, p, method)),
				})
			}
		}
	}
	return ops, nil
}

// specSections returns the spec sections without the later of two that name
// one spec.
func (l *Library) specSections() []*section {
	var specs []*section
	seen := map[string]bool{}
	for _, sec := range l.s.sections {
		if sec.Type == SpecSection && !seen[sec.Input] {
			seen[sec.Input] = true
			specs = append(specs, sec)
		}
	}
	return specs
}

// SpecPart returns the part at pointer, such as paths/~1pets/get or
// components/schemas/Pet, of the published spec of the spec section whose
// slug is spec, as YAML. The pointer may index a sequence, and may go on past
// a mapping that holds an internal $ref, as Operations' pointer does for a
// path item that is a $ref.
func (l *Library) SpecPart(spec, pointer string) (string, error) {
	sec, ok := l.s.specFor(spec)
	if !ok {
		return "", fmt.Errorf("no spec section has the slug %q", spec)
	}
	root, err := l.specRoot(sec)
	if err != nil {
		return "", err
	}
	node := partAt(root, pointer)
	if pointer == "" || node == nil {
		return "", fmt.Errorf("no part of the spec %q at %q", spec, pointer)
	}
	out, err := yaml.Marshal(node)
	return string(out), err
}

// partAt returns the node at pointer below root, or nil.
func partAt(root *yaml.Node, pointer string) *yaml.Node {
	n := root
	for _, token := range strings.Split(pointer, "/") {
		if n = child(root, n, unescapeToken(token)); n == nil {
			return nil
		}
	}
	return n
}

// child returns the item of n at key: a sequence's element by its index, or
// a mapping's value. A mapping without key that holds an internal $ref looks
// key up in the $ref's target; a few hops end any cycle.
func child(root, n *yaml.Node, key string) *yaml.Node {
	for hops := 0; n != nil && hops < 8; hops++ {
		switch n.Kind {
		case yaml.SequenceNode:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(n.Content) || strconv.Itoa(i) != key {
				return nil
			}
			return n.Content[i]
		case yaml.MappingNode:
			if v := mappingValue(n, key); v != nil {
				return v
			}
			ref := mappingValue(n, "$ref")
			if ref == nil {
				return nil
			}
			n = refTarget(root, ref.Value)
		default:
			return nil
		}
	}
	return nil
}

// Match is a line of a markdown file, or a key or value of a published spec,
// that holds a query.
type Match struct {
	Spec  string `json:"spec,omitempty"` // the slug of the spec section of a match in a spec
	Where string `json:"where"`          // path:line of a markdown file from the documentation root, or a pointer into the spec
	Text  string `json:"text"`
	URL   string `json:"url"` // the page that shows it
}

// Search returns up to limit matches of query, ignoring case: in the text of
// the document pages, then in the published specs, in the order of the spec
// sections. When both hold more matches than fit, the pages take half of
// limit, rounded up, and the specs the rest. A spec that does not load has no
// matches.
func (l *Library) Search(query string, limit int) ([]Match, error) {
	q := strings.ToLower(query)
	if q == "" || limit <= 0 {
		return nil, nil
	}
	pages, err := l.pages()
	if err != nil {
		return nil, err
	}
	var inDocs []Match
	for _, pg := range pages {
		for i, line := range strings.Split(pg.text, "\n") {
			if len(inDocs) < limit && strings.Contains(strings.ToLower(line), q) {
				inDocs = append(inDocs, Match{Where: pg.Path + ":" + strconv.Itoa(i+1), Text: clip(line), URL: pg.URL})
			}
		}
	}
	var inSpec []Match
	for _, sec := range l.specSections() {
		root, err := l.specRoot(sec)
		if err != nil {
			continue
		}
		if len(inSpec) >= limit {
			break
		}
		walk(root, "", func(pointer, text string) bool {
			if strings.Contains(strings.ToLower(text), q) {
				inSpec = append(inSpec, Match{Spec: sec.slug, Where: pointer, Text: clip(text), URL: sec.viewerURL("")})
			}
			return len(inSpec) < limit
		})
	}
	n := min(len(inDocs), max(limit-len(inSpec), (limit+1)/2))
	return append(inDocs[:n], inSpec[:min(len(inSpec), limit-n)]...), nil
}

// walk calls visit with the pointer and the text of every mapping key and
// scalar below n, until visit returns false, and reports whether it got to
// the end.
func walk(n *yaml.Node, pointer string, visit func(pointer, text string) bool) bool {
	join := func(token string) string {
		if pointer == "" {
			return token
		}
		return pointer + "/" + token
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			p := join(escapeToken(n.Content[i].Value))
			if !visit(p, n.Content[i].Value) || !walk(n.Content[i+1], p, visit) {
				return false
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			if !walk(c, join(strconv.Itoa(i)), visit) {
				return false
			}
		}
	case yaml.ScalarNode:
		return visit(pointer, n.Value)
	}
	return true
}

// clip trims s to one short line of at most 200 characters.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= 200 {
		return s
	}
	return string([]rune(s)[:200]) + "..."
}

// specRoot returns the root node of the published spec of the spec section
// sec, with each alias replaced by a copy of its anchor's node, as when the
// publication rules removed a part.
func (l *Library) specRoot(sec *section) (*yaml.Node, error) {
	sp, err := l.s.loadSpec(sec.Input)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(sp.Raw, &doc); err != nil {
		return nil, err
	}
	budget := maxCopies
	if err := expandAliases(&doc, &budget); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, errors.New("the spec is empty")
	}
	return doc.Content[0], nil
}
