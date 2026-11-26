package chat

import (
	"errors"
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

func TestAClearedChatAnswersNoQuestion(t *testing.T) {
	c := newChat(t, fakemodel.New("opus", []fakemodel.Exchange{{Reply: "an answer"}}), Limits{})
	slug := c.currentAgents()[0].lib.Slug()
	c.Clear()
	if _, err := c.Ask(t.Context(), slug, portal.Everything, AskerOf("", "client"), "conv", "How?"); !errors.Is(err, ErrNoPortal) {
		t.Errorf("a question after Clear: err = %v, want ErrNoPortal", err)
	}
	if menu := c.menuOf(portal.Everything); len(menu.Portals) != 0 {
		t.Errorf("the menu after Clear: %+v", menu)
	}
	if err := c.Reload([]*portal.Library{library(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ask(t.Context(), slug, portal.Everything, AskerOf("", "client"), "conv", "How?"); err != nil {
		t.Errorf("a question after the next Reload: %v", err)
	}
}
