package chat

import (
	"net/http"

	"github.com/bilus/documentation-portal/portal"
)

// pageAccess is the access that a chat page keeps from its load: the titles
// of the sections visible to its reader, by the name of their portal.
// live-templ signs it into the page's session, and the page's mount at the
// socket's join, which gets no request, reads it there.
type pageAccess map[string][]string

// Portal reports whether the page access names a section of p.
func (a pageAccess) Portal(p portal.Portal) bool {
	// HOLE(2): report whether a holds a title under p's name
	return false
}

// Section reports whether the page access names s, a section of p.
func (a pageAccess) Section(p portal.Portal, s portal.Section) bool {
	// HOLE(2): report whether a holds s's title under p's name
	return false
}

// pageAccessOf returns the page access of a reader with access over the
// portals of agents: the titles of the sections of each portal visible to
// the reader.
func pageAccessOf(access portal.Access, agents []*portalAgent) pageAccess {
	// HOLE(2): list the sections of each agent's library left by Library.For(access)
	return pageAccess{}
}

// readPageAccess returns the page access in value, a page's session value,
// or an access that hides every portal for a value that does not hold one.
func readPageAccess(value string) pageAccess {
	// HOLE(2): decode value, as sessionOf encodes it
	return pageAccess{}
}

// sessionOf returns the session of a chat page for r: the client of r, as
// clientOf names it, and the page access of r's reader over the chat's
// portals, from portal.AccessOf.
func (c *Chat) sessionOf(r *http.Request) (map[string]string, error) {
	session, err := clientOf(r)
	if err != nil {
		return nil, err
	}
	// HOLE(2): add the page access of portal.AccessOf(r) over the current agents, encoded, under "access"
	return session, nil
}
