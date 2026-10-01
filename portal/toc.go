package portal

import "io/fs"

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
	// HOLE(1): read and check the toc file
	return nil, nil
}

// tocGroups lays out entries as the sidebar's groups: the top-level items
// before the first group or divider in an untitled group, a group as a
// titled group, a divider as the title of a group for the top-level items
// after it, and the entries of a group inside a group in their place. It
// leaves out an item that no page serves.
func (s *site) tocGroups(entries []tocEntry, current string) []sidebarGroup {
	// HOLE(1): lay out the entries as sidebar groups
	return nil
}

// tocLink returns the sidebar link titled title for uri, marked when it
// links the document page of current, or false when no page serves uri. A
// uri is an http or https URL, or a path from the documentation root to a
// markdown file of the content directory or to the configured spec.
func (s *site) tocLink(title, uri, current string) (sidebarLink, bool) {
	// HOLE(1): resolve uri against the documentation root
	return sidebarLink{}, false
}
