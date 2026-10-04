// Package fakeaccess holds the stub hook, the access hook of the tests: it
// reads the sections visible to a request's reader from the request's
// Sections header.
package fakeaccess

import (
	"net/http"
	"slices"
	"strings"

	"github.com/bilus/documentation-portal/portal"
)

// Header is the request header with the sections visible to the reader: each
// as its portal's name and its title with a slash between, separated by
// commas, such as "Pet Shop/API, Store/API".
const Header = "Sections"

// Hook returns the access in r's Sections header. A request without the
// header sees no portal.
func Hook(r *http.Request) (portal.Access, error) {
	return Parse(r.Header.Get(Header)), nil
}

// Sections is the access to the sections that it lists by the name of their
// portal, as their titles.
type Sections map[string][]string

// Parse returns the access in list, a value of the Sections header.
func Parse(list string) Sections {
	visible := Sections{}
	for item := range strings.SplitSeq(list, ",") {
		if name, title, ok := strings.Cut(strings.TrimSpace(item), "/"); ok {
			visible[name] = append(visible[name], title)
		}
	}
	return visible
}

// Portal reports whether the list names a section of p.
func (v Sections) Portal(p portal.Portal) bool { return len(v[p.Name]) > 0 }

// Section reports whether the list names s, a section of p.
func (v Sections) Section(p portal.Portal, s portal.Section) bool {
	return slices.Contains(v[p.Name], s.Title)
}
