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

// tocSidebar returns the document sidebar that the toc file lays out, with
// the file at current marked, or an error for a toc file that is missing or
// invalid or that names no page the portal serves, and for a failed listing
// of the markdown files.
func (s *site) tocSidebar(current string) ([]sidebarGroup, error) {
	entries, err := readToc(s.root, s.tocPath)
	if err != nil {
		return nil, err
	}
	pages, err := s.newTocPages(current)
	if err != nil {
		return nil, err
	}
	groups := pages.groups(entries)
	for _, g := range groups {
		for _, l := range g.Links {
			// link writes a page of the portal as a path, another site as a URL.
			if strings.HasPrefix(l.URL, "/") {
				return groups, nil
			}
		}
	}
	return nil, errors.New("names no page that the portal serves")
}

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
// portal serves, for the sidebar of one page. It lists the markdown files
// once, and checks, reads and parses the spec at most once.
type tocPages struct {
	s           *site
	current     string          // the markdown file of the page that shows the sidebar
	docs        map[string]bool // the markdown files that the document list shows
	specChecked bool            // whether specServed holds
	specServed  bool            // whether a regular file is at the spec path
	specLoaded  bool            // whether specRoot holds
	specRoot    *yaml.Node      // the published spec's root, or nil when it does not load
}

// newTocPages lists the markdown files of the content directory, for the
// sidebar of the page of the markdown file at current.
func (s *site) newTocPages(current string) (*tocPages, error) {
	docs := map[string]bool{}
	if s.docs != nil {
		paths, err := markdownFiles(s.docs)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			docs[p] = true
		}
	}
	return &tocPages{s: s, current: current, docs: docs}, nil
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
// uri is an http or
// https URL, or a path from the documentation root to a markdown file of the
// content directory or to the configured spec.
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
	if doc, ok := p.s.contentPath(target); ok && p.docs[doc] {
		page := &url.URL{Path: "/docs/" + doc, RawQuery: u.RawQuery, Fragment: u.Fragment}
		return sidebarLink{Title: title, URL: page.String(), Current: doc == p.current}, true
	}
	return sidebarLink{}, false
}

// specURL returns the viewer page URL for target as site.specURL does, with
// the spec checked, read and parsed at most once.
func (p *tocPages) specURL(target string) (string, bool) {
	pointer, ok := p.s.specPart(target)
	if !ok {
		return "", false
	}
	if !p.specChecked {
		p.specChecked = true
		info, err := fs.Stat(p.s.root, p.s.specPath)
		p.specServed = err == nil && info.Mode().IsRegular()
	}
	if !p.specServed {
		return "", false
	}
	if pointer == "" {
		return p.s.viewerURL(""), true
	}
	if !p.specLoaded {
		p.specLoaded = true
		var doc yaml.Node
		if sp, err := p.s.loadSpec(p.s.specPath); err == nil && yaml.Unmarshal(sp.Raw, &doc) == nil && len(doc.Content) > 0 {
			p.specRoot = doc.Content[0]
		}
	}
	fragment, _ := routeIn(p.specRoot, pointer)
	return p.s.viewerURL(fragment), true
}
