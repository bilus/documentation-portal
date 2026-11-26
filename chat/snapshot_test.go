package chat

import (
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

// A page loaded after a reload takes its menu's collections and portals
// from the reloaded snapshot.
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
	if err := c.Reload(libsOf("New name")); err != nil {
		t.Fatal(err)
	}
	menu := c.menuOf(portal.Everything)
	if len(menu.Collections) != 1 || menu.Collections[0].Name != "New name" || len(menu.Collections[0].Portals) != 2 {
		t.Errorf("the menu after a reload: %+v, want the collection New name with both portals", menu.Collections)
	}
}
