package portal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// tocEntry is an entry of a toc file: an item that links a page, a group of
// entries, or a divider that starts a section.
type tocEntry struct {
	Type  string     `json:"type"`
	Title string     `json:"title"`
	URI   string     `json:"uri"`
	Items []tocEntry `json:"items"`
}

// tocSidebar returns the document sidebar that the toc file of the docs
// section sec lays out, with the section's file at current marked, and the
// set of the section's markdown files among the sidebar's links, or an error
// for a toc file that is missing or invalid or that names no page the portal
// serves, and for a failed listing of the section's markdown files.
func (s *site) tocSidebar(sec *section, current string) ([]sidebarGroup, map[string]bool, error) {
	entries, err := readToc(s.root, sec.Toc)
	if err != nil {
		return nil, nil, err
	}
	pages, err := s.newTocPages(sec, current)
	if err != nil {
		return nil, nil, err
	}
	groups := pages.groups(entries)
	for _, g := range groups {
		for _, l := range g.Links {
			// link writes a page of the portal as a path, another site as a URL.
			if strings.HasPrefix(l.URL, "/") {
				return groups, pages.linked, nil
			}
		}
	}
	return nil, nil, errNoPage
}

// errNoPage means a toc file whose entries name no page of the portal.
var errNoPage = errors.New("names no page that the portal serves")

// readToc reads the toc file at p of fsys and returns its entries, or an
// error and no entries for a file that is missing, reached through a symlink
// or not JSON, that has no items, or that holds an entry of a type other than
// item, group and divider, an entry without a title, or an item without a
// uri. A UTF-8 byte order mark at the start of the file is no error.
func readToc(fsys fs.FS, p string) ([]tocEntry, error) {
	if _, err := fs.Lstat(fsys, p); err != nil {
		return nil, err
	}
	if !regularFile(fsys, p) {
		return nil, errors.New("not a regular file reached through no symlink")
	}
	data, err := fs.ReadFile(fsys, p)
	if err != nil {
		return nil, err
	}
	var toc struct {
		Items []tocEntry `json:"items"`
	}
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte("\ufeff")), &toc); err != nil {
		return nil, err
	}
	if toc.Items == nil {
		return nil, errors.New("no items")
	}
	if err := checkEntries(toc.Items); err != nil {
		return nil, err
	}
	return toc.Items, nil
}

// checkEntries reports the first of entries, or of their groups' entries,
// without a title, of a type other than item, group and divider, or an item
// without a uri.
func checkEntries(entries []tocEntry) error {
	for _, e := range entries {
		switch {
		case e.Title == "":
			return fmt.Errorf("an entry of type %q has no title", e.Type)
		case e.Type == "item" && e.URI == "":
			return fmt.Errorf("item %q has no uri", e.Title)
		case e.Type == "group":
			if err := checkEntries(e.Items); err != nil {
				return err
			}
		case e.Type != "item" && e.Type != "divider":
			return fmt.Errorf("entry %q has the unknown type %q", e.Title, e.Type)
		}
	}
	return nil
}

// tocPages resolves the uris of a toc file against the pages that the
// portal serves, for the sidebar of one page. It lists the markdown files of
// each docs section, and checks, reads and parses each spec, at most once.
type tocPages struct {
	s       *site
	sec     *section                     // the docs section of the page that shows the sidebar
	current string                       // the markdown file of that page, or ""
	docs    map[*section]map[string]bool // the markdown files of the docs sections listed so far
	linked  map[string]bool              // the markdown files of sec among the links so far
	specs   map[*section]*tocSpec        // the spec sections that the uris named so far
}

// tocSpec is what tocPages found out about the spec of a spec section.
type tocSpec struct {
	served bool       // whether a regular file is at the spec path
	loaded bool       // whether root holds
	root   *yaml.Node // the published spec's root, or nil when it does not load
}

// newTocPages lists the markdown files of the docs section sec, for the
// sidebar of the page of its markdown file at current, or refuses a spec
// section.
func (s *site) newTocPages(sec *section, current string) (*tocPages, error) {
	if sec.docs == nil {
		return nil, fmt.Errorf("section %q has no content directory", sec.Title)
	}
	paths, err := markdownFiles(sec.docs)
	if err != nil {
		return nil, err
	}
	p := &tocPages{s: s, sec: sec, current: current, docs: map[*section]map[string]bool{}, linked: map[string]bool{}, specs: map[*section]*tocSpec{}}
	p.docs[sec] = setOf(paths)
	return p, nil
}

// setOf returns the set of paths.
func setOf(paths []string) map[string]bool {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	return set
}

// groups lays out entries as the sidebar's groups: the top-level items
// before the first group or divider in an untitled group, a group as a
// titled group, a divider as the title of a group for the top-level items
// after it, and the entries of a group inside a group in their place. Top-
// level items after a group form an untitled group of their own. It leaves
// out an item that no page serves and a group left without links, and keeps
// a divider's title with no item after it.
func (p *tocPages) groups(entries []tocEntry) []sidebarGroup {
	var groups []sidebarGroup
	var open *sidebarGroup // the group that takes the next top-level items
	flush := func() {
		if open != nil && (open.Title != "" || len(open.Links) > 0) {
			groups = append(groups, *open)
		}
		open = nil
	}
	for _, e := range entries {
		switch e.Type {
		case "item":
			if open == nil {
				open = &sidebarGroup{}
			}
			open.Links = append(open.Links, p.links([]tocEntry{e})...)
		case "divider":
			flush()
			open = &sidebarGroup{Title: e.Title}
		case "group":
			flush()
			if links := p.links(e.Items); len(links) > 0 {
				groups = append(groups, sidebarGroup{Title: e.Title, Links: links})
			}
		}
	}
	flush()
	return groups
}

// links returns the links of the items among entries, and of the items of
// their groups in their place, leaving out an item that no page serves.
func (p *tocPages) links(entries []tocEntry) []sidebarLink {
	var links []sidebarLink
	for _, e := range entries {
		switch e.Type {
		case "item":
			if link, ok := p.link(e.Title, e.URI); ok {
				links = append(links, link)
			}
		case "group":
			links = append(links, p.links(e.Items)...)
		}
	}
	return links
}

// link returns the sidebar link titled title for uri, marked when it links
// the current page, or false when title is empty or no page serves uri. A
// uri is an http or https URL, or a path from the documentation root to a
// markdown file of a docs section or to the spec of a spec section. A link to
// a markdown file of sec adds the file to linked.
func (p *tocPages) link(title, uri string) (sidebarLink, bool) {
	u, err := url.Parse(uri)
	switch {
	case title == "" || err != nil:
		return sidebarLink{}, false
	case (u.Scheme == "http" || u.Scheme == "https") && u.Host != "":
		return sidebarLink{Title: title, URL: uri}, true
	case u.Scheme != "" || u.Host != "" || u.Path == "":
		return sidebarLink{}, false
	}
	target := strings.TrimPrefix(path.Clean(u.Path), "/")
	if target == ".." || strings.HasPrefix(target, "../") {
		return sidebarLink{}, false
	}
	if page, ok := p.specURL(target); ok {
		return sidebarLink{Title: title, URL: page}, true
	}
	if sec, doc, ok := p.docAt(target); ok {
		if sec == p.sec {
			p.linked[doc] = true
		}
		return sidebarLink{Title: title, URL: sec.docURL(doc, u.RawQuery, u.Fragment), Current: sec == p.sec && doc == p.current}, true
	}
	return sidebarLink{}, false
}

// docAt returns the docs section and the path of the markdown file at target
// as site.docAt does with the section of the page that shows the sidebar
// preferred, and with the markdown files of each docs section listed at most
// once. A section whose listing fails serves no entry.
func (p *tocPages) docAt(target string) (*section, string, bool) {
	holds := func(sec *section) (string, bool) {
		doc, ok := sec.contentPath(target)
		if !ok {
			return "", false
		}
		docs, listed := p.docs[sec]
		if !listed {
			paths, err := markdownFiles(sec.docs)
			if err != nil {
				paths = nil
			}
			docs = setOf(paths)
			p.docs[sec] = docs
		}
		return doc, docs[doc]
	}
	if doc, ok := holds(p.sec); ok {
		return p.sec, doc, true
	}
	for _, sec := range p.s.sections {
		if doc, ok := holds(sec); ok {
			return sec, doc, true
		}
	}
	return nil, "", false
}

// specURL returns the viewer page URL for target as site.specURL does, with
// each spec checked, read and parsed at most once.
func (p *tocPages) specURL(target string) (string, bool) {
	sec, pointer, ok := p.s.specPart(target)
	if !ok {
		return "", false
	}
	spec, checked := p.specs[sec]
	if !checked {
		info, err := fs.Stat(p.s.root, sec.Input)
		spec = &tocSpec{served: err == nil && info.Mode().IsRegular()}
		p.specs[sec] = spec
	}
	if !spec.served {
		return "", false
	}
	if pointer == "" {
		return sec.viewerURL(""), true
	}
	if !spec.loaded {
		spec.loaded = true
		var doc yaml.Node
		if sp, err := p.s.loadSpec(sec.Input); err == nil && yaml.Unmarshal(sp.Raw, &doc) == nil && len(doc.Content) > 0 {
			spec.root = doc.Content[0]
		}
	}
	fragment, _ := routeIn(spec.root, pointer)
	return sec.viewerURL(fragment), true
}
