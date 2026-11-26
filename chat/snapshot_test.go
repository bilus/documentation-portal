package chat

import (
	"net/url"
	"testing"
	"testing/fstest"

	"github.com/bilus/live-templ/interpreter"

	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

// A page whose route was built before a reload takes its menu's collections
// from the reloaded snapshot, as it takes the menu's portals.
func TestTheChatMenuFollowsAReload(t *testing.T) {
	api := []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}}
	libsOf := func(collection string) []*portal.Library {
		libs, err := portal.NewLibraries(portal.Config{
			Root:        fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths: {}\n")}},
			Portals:     []portal.Portal{{Name: "Pets", Labels: []string{"apis"}, Sections: api}, {Name: "Store", Labels: []string{"apis"}, Sections: api}},
			Collections: []portal.Collection{{Name: collection, MatchLabels: []string{"apis"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return libs
	}
	c, err := New(Config{Model: fakemodel.New("opus", nil), Libraries: libsOf("Old name")})
	if err != nil {
		t.Fatal(err)
	}
	before := c.currentAgents()[0]
	if err := c.Reload(libsOf("New name")); err != nil {
		t.Fatal(err)
	}
	u := &url.URL{Path: "/portals/pets/chat"}
	p, err := c.mount(interpreter.NewCtx(t.Context(), u.Path, u, map[string]string{"access": `{"all":true}`}, true), before)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Collections) != 1 || p.Collections[0].Name != "New name" || len(p.Collections[0].Links) != 2 {
		t.Errorf("the menu of a page mounted after a reload: %+v, want the collection New name with both portals", p.Collections)
	}
}
