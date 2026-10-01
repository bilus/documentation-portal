package portal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"strings"
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
// the file at current marked, or the toc file's error.
func (s *site) tocSidebar(current string) ([]sidebarGroup, error) {
	entries, err := readToc(s.root, s.tocPath)
	if err != nil {
		return nil, err
	}
	return s.tocGroups(entries, current), nil
}

// readToc reads the toc file at p of fsys and returns its entries, or an
// error for a file that is missing, reached through a symlink or not JSON,
// that has no items, or that holds an entry of a type other than item, group
// and divider, an entry without a title, or an item without a uri.
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
	if err := json.Unmarshal(data, &toc); err != nil {
		return nil, err
	}
	if toc.Items == nil {
		return nil, errors.New("no items")
	}
	return toc.Items, checkEntries(toc.Items)
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

// tocGroups lays out entries as the sidebar's groups: the top-level items
// before the first group or divider in an untitled group, a group as a
// titled group, a divider as the title of a group for the top-level items
// after it, and the entries of a group inside a group in their place. Top-
// level items after a group form an untitled group of their own. It leaves
// out an item that no page serves, and a group left without links.
func (s *site) tocGroups(entries []tocEntry, current string) []sidebarGroup {
	var groups []sidebarGroup
	var open *sidebarGroup // the group that takes the next top-level items
	flush := func() {
		if open != nil && len(open.Links) > 0 {
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
			open.Links = append(open.Links, s.tocLinks([]tocEntry{e}, current)...)
		case "divider":
			flush()
			open = &sidebarGroup{Title: e.Title}
		case "group":
			flush()
			if links := s.tocLinks(e.Items, current); len(links) > 0 {
				groups = append(groups, sidebarGroup{Title: e.Title, Links: links})
			}
		}
	}
	flush()
	return groups
}

// tocLinks returns the links of the items among entries, and of the items
// of their groups in their place, leaving out an item that no page serves.
func (s *site) tocLinks(entries []tocEntry, current string) []sidebarLink {
	var links []sidebarLink
	for _, e := range entries {
		switch e.Type {
		case "item":
			if link, ok := s.tocLink(e.Title, e.URI, current); ok {
				links = append(links, link)
			}
		case "group":
			links = append(links, s.tocLinks(e.Items, current)...)
		}
	}
	return links
}

// tocLink returns the sidebar link titled title for uri, marked when it
// links the document page of current, or false when no page serves uri. A
// uri is an http or https URL, or a path from the documentation root to a
// markdown file of the content directory or to the configured spec.
func (s *site) tocLink(title, uri, current string) (sidebarLink, bool) {
	u, err := url.Parse(uri)
	switch {
	case err != nil:
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
	if page, ok := s.specURL(target); ok {
		return sidebarLink{Title: title, URL: page}, true
	}
	if doc, ok := s.docAt(target); ok {
		page := &url.URL{Path: "/docs/" + doc, RawQuery: u.RawQuery, Fragment: u.Fragment}
		return sidebarLink{Title: title, URL: page.String(), Current: doc == current}, true
	}
	return sidebarLink{}, false
}
