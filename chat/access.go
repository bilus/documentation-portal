package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/bilus/documentation-portal/portal"
)

// pageAccess is the access that a chat page keeps from its load. For a
// reader whose access is portal.Everything, as without an access hook, it
// allows everything, so that the page follows each snapshot. Else it holds
// the titles of the sections visible to the reader, by portal, with a digest
// of each portal's configuration, so that a later snapshot that changes the
// portal closes it to the page until the page loads again. live-templ signs
// it into the page session, and the page's mount at the join, which gets no
// request, reads it there.
type pageAccess struct {
	All     bool                  `json:"all,omitempty"`
	Portals map[string]pagePortal `json:"portals,omitempty"` // by portal name
}

// pagePortal is a portal of a page access.
type pagePortal struct {
	Digest   string   `json:"digest"`   // of the portal's configuration at the page's load
	Sections []string `json:"sections"` // the titles of its visible sections
}

// Portal reports whether the page access allows p: everything, or one of
// its sections in a portal with p's name and configuration.
func (a pageAccess) Portal(p portal.Portal) bool {
	if a.All {
		return true
	}
	pp, ok := a.Portals[p.Name]
	return ok && len(pp.Sections) > 0 && pp.Digest == digest(p)
}

// Section reports whether the page access allows s, a section of p.
func (a pageAccess) Section(p portal.Portal, s portal.Section) bool {
	return a.All || a.Portal(p) && slices.Contains(a.Portals[p.Name].Sections, s.Title)
}

// digest returns a digest of p as the portal configuration gives it, with
// its labels and its sections, which changes with any change of them.
func digest(p portal.Portal) string {
	// A Portal holds strings and lists alone, which always marshal.
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// viewOf names the reader's view of the portal in lib, the portal's library
// limited to the reader's access: "all" when lib holds every section of the
// portal, as for every reader without an access hook, and else the digest of
// the portal's configuration with only the sections in lib, which changes
// with the set of those sections and with their configuration.
func viewOf(lib *portal.Library) string {
	visible, p := lib.Sections(), lib.Portal()
	if len(visible) == len(p.Sections) {
		return "all"
	}
	var sections []portal.Section
	for _, s := range p.Sections {
		// Two sections of a portal never share a title, since their slugs come from their titles.
		if slices.ContainsFunc(visible, func(l portal.SectionLink) bool { return l.Title == s.Title }) {
			sections = append(sections, s)
		}
	}
	p.Sections = sections
	return digest(p)
}

// pageAccessOf returns the page access of a reader with access over the
// portals of agents: everything for portal.Everything, else the titles of the
// sections of each portal visible to the reader, with the portal's digest.
func pageAccessOf(access portal.Access, agents []*portalAgent) pageAccess {
	if access == portal.Everything {
		return pageAccess{All: true}
	}
	page := pageAccess{Portals: map[string]pagePortal{}}
	for _, a := range agents {
		lib, ok := a.lib.For(access)
		if !ok {
			continue
		}
		pp := pagePortal{Digest: digest(a.lib.Portal())}
		for _, s := range lib.Sections() {
			pp.Sections = append(pp.Sections, s.Title)
		}
		page.Portals[lib.Name()] = pp
	}
	return page
}

// readPageAccess returns the page access in value, a page's session value,
// or an access that hides every portal for a value that does not hold one.
func readPageAccess(value string) pageAccess {
	var page pageAccess
	if err := json.Unmarshal([]byte(value), &page); err != nil {
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
