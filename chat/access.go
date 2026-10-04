package chat

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/bilus/documentation-portal/portal"
)

// pageAccess is the access that a chat page keeps from its load: the titles
// of the sections visible to its reader, by the name of their portal.
// live-templ signs it into the page's session, and the page's mount at the
// socket's join, which gets no request, reads it there.
type pageAccess map[string][]string

// Portal reports whether the page access names a section of p.
func (a pageAccess) Portal(p portal.Portal) bool {
	return len(a[p.Name]) > 0
}

// Section reports whether the page access names s, a section of p.
func (a pageAccess) Section(p portal.Portal, s portal.Section) bool {
	return slices.Contains(a[p.Name], s.Title)
}

// pageAccessOf returns the page access of a reader with access over the
// portals of agents: the titles of the sections of each portal visible to
// the reader.
func pageAccessOf(access portal.Access, agents []*portalAgent) pageAccess {
	page := pageAccess{}
	for _, a := range agents {
		lib, ok := a.lib.For(access)
		if !ok {
			continue
		}
		for _, s := range lib.Sections() {
			page[lib.Name()] = append(page[lib.Name()], s.Title)
		}
	}
	return page
}

// readPageAccess returns the page access in value, a page's session value,
// or an access that hides every portal for a value that does not hold one.
func readPageAccess(value string) pageAccess {
	var page pageAccess
	if err := json.Unmarshal([]byte(value), &page); err != nil || page == nil {
		return pageAccess{}
	}
	return page
}

// sessionOf returns the session of a chat page for r: the client of r, as
// clientOf names it, and the page access of r's reader over the chat's
// portals, from portal.AccessOf.
func (c *Chat) sessionOf(r *http.Request) (map[string]string, error) {
	session, err := clientOf(r)
	if err != nil {
		return nil, err
	}
	access, err := json.Marshal(pageAccessOf(portal.AccessOf(r), c.currentAgents()))
	if err != nil {
		return nil, err
	}
	session["access"] = string(access)
	return session, nil
}
