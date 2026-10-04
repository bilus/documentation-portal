// Package fakeaccess holds the stub hook, the access hook of the tests: it
// reads the sections visible to a request's reader from the request's
// Sections header, and the previews open to the reader from its Previews
// header.
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

// PreviewsHeader is the request header with the preview folders open to the
// reader, separated by commas, such as "pr-1, pr-2".
const PreviewsHeader = "Previews"

// Hook returns the access in r's Sections and Previews headers. A request
// without them sees no portal and opens no preview.
func Hook(r *http.Request) (portal.Access, error) {
	var previews []string
	for folder := range strings.SplitSeq(r.Header.Get(PreviewsHeader), ",") {
		if folder = strings.TrimSpace(folder); folder != "" {
			previews = append(previews, folder)
		}
	}
	return Reader{Sections: Parse(r.Header.Get(Header)), Previews: previews}, nil
}

// Reader is the access of the stub hook: the sections that it lists and the
// preview folders.
type Reader struct {
	Sections
	Previews []string
}

// Preview reports whether the list of previews names the folder.
func (a Reader) Preview(folder string) bool { return slices.Contains(a.Previews, folder) }

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
