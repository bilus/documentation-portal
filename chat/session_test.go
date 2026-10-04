package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/bilus/documentation-portal/internal/fakeaccess"
	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

func TestPageAccess(t *testing.T) {
	shop := portal.Portal{Name: "Pet Shop", Sections: []portal.Section{{Title: "Orders"}, {Title: "API"}}}
	vault := portal.Portal{Name: "Vault", Sections: []portal.Section{{Title: "API"}}}
	access := pageAccess{Portals: map[string]pagePortal{
		"Pet Shop": {Digest: digest(shop), Sections: []string{"Orders"}},
		"Vault":    {Digest: digest(vault)},
	}}
	relabelled := shop
	relabelled.Labels = []string{"staff"}
	for _, tc := range []struct {
		name                string
		portal              portal.Portal
		section             portal.Section
		portalOK, sectionOK bool
	}{
		{"a visible section", shop, portal.Section{Title: "Orders"}, true, true},
		{"a hidden section", shop, portal.Section{Title: "API"}, true, false},
		{"a portal whose configuration changed", relabelled, portal.Section{Title: "Orders"}, false, false},
		{"a portal without a visible section", vault, portal.Section{Title: "API"}, false, false},
		{"a portal missing from the access", portal.Portal{Name: "Other"}, portal.Section{Title: "API"}, false, false},
	} {
		if got := access.Portal(tc.portal); got != tc.portalOK {
			t.Errorf("%s: Portal: %v", tc.name, got)
		}
		if got := access.Section(tc.portal, tc.section); got != tc.sectionOK {
			t.Errorf("%s: Section: %v", tc.name, got)
		}
	}
	if all := (pageAccess{All: true}); !all.Portal(relabelled) || !all.Section(vault, portal.Section{Title: "Gold"}) {
		t.Error("an access to everything hides a portal or a section")
	}
}

// visibleIn returns the titles of the sections of agents' portals that
// access allows, by the name of their portal.
func visibleIn(access portal.Access, agents []*portalAgent) map[string][]string {
	visible := map[string][]string{}
	for _, a := range agents {
		p := a.lib.Portal()
		for _, s := range p.Sections {
			if access.Portal(p) && access.Section(p, s) {
				visible[p.Name] = append(visible[p.Name], s.Title)
			}
		}
	}
	return visible
}

func TestPageAccessOfAndBack(t *testing.T) {
	c, _ := newAccessChat(t, fakemodel.New("opus", nil))
	agents := c.currentAgents()
	for _, tc := range []struct {
		reader string
		want   map[string][]string
	}{
		{customer, map[string][]string{"Pet Shop": {"Orders", "Guides"}, "Garden": {"API"}}},
		{"Garden/API", map[string][]string{"Garden": {"API"}}},
		{"", map[string][]string{}},
	} {
		page := pageAccessOf(fakeaccess.Parse(tc.reader), agents)
		if got := visibleIn(page, agents); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: the page access allows %v, want %v", tc.reader, got, tc.want)
		}
		value, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		if got := visibleIn(readPageAccess(string(value)), agents); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: read back, the page access allows %v, want %v", tc.reader, got, tc.want)
		}
	}
	for _, value := range []string{"", "{", "null", `["Pet Shop"]`, `{"Pet Shop": ["Orders"]}`, `{"all": "yes"}`} {
		if got := visibleIn(readPageAccess(value), agents); len(got) != 0 {
			t.Errorf("%q: the page access allows %v, want nothing", value, got)
		}
	}
	every := pageAccessOf(portal.Everything, agents)
	if got := visibleIn(every, agents); len(got) != 3 || !slices.Equal(got["Pet Shop"], []string{"API", "Orders", "Guides", "Internal"}) {
		t.Errorf("Everything: the page access allows %v", got)
	}
}

func TestSessionOf(t *testing.T) {
	var c *Chat
	var session map[string]string
	keep := portal.Route{Pattern: "GET /session", Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var err error
		if session, err = c.sessionOf(r); err != nil {
			t.Error(err)
		}
	})}
	c, h := newAccessChat(t, fakemodel.New("opus", nil), keep)
	agents := c.currentAgents()
	r := httptest.NewRequest(http.MethodGet, "/session", nil)
	r.RemoteAddr = "192.0.2.7:5555"
	r.Header.Set(fakeaccess.Header, customer)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got := visibleIn(readPageAccess(session["access"]), agents); session["client"] != "192.0.2.7" || !reflect.DeepEqual(got, map[string][]string{"Pet Shop": {"Orders", "Guides"}, "Garden": {"API"}}) {
		t.Errorf("the session through a portal handler with a hook: %v, which allows %v", session, got)
	}
	outside, err := c.sessionOf(httptest.NewRequest(http.MethodGet, "/portals/garden/chat", nil))
	if got := visibleIn(readPageAccess(outside["access"]), agents); err != nil || len(got) != 0 {
		t.Errorf("the session of a request served by no portal handler: %v, %v, which allows %v, want nothing", outside, err, got)
	}
	if every := readPageAccess(sessionThroughPortal(t, c)["access"]); !every.All {
		t.Errorf("the session through a portal handler without a hook: %+v, want everything", every)
	}
}
