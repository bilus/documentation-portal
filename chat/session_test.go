package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/bilus/documentation-portal/internal/fakeaccess"
	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

func TestPageAccess(t *testing.T) {
	access := pageAccess{"Pet Shop": {"Orders", "Guides"}, "Garden": {"API"}, "Vault": nil}
	shop := portal.Portal{Name: "Pet Shop"}
	for _, tc := range []struct {
		portal              portal.Portal
		section             portal.Section
		portalOK, sectionOK bool
	}{
		{shop, portal.Section{Title: "Orders"}, true, true},
		{shop, portal.Section{Title: "API"}, true, false},
		{portal.Portal{Name: "Garden"}, portal.Section{Title: "Orders"}, true, false},
		{portal.Portal{Name: "Vault"}, portal.Section{Title: "API"}, false, false},
		{portal.Portal{Name: "Other"}, portal.Section{Title: "API"}, false, false},
	} {
		if got := access.Portal(tc.portal); got != tc.portalOK {
			t.Errorf("Portal(%s): %v", tc.portal.Name, got)
		}
		if got := access.Section(tc.portal, tc.section); got != tc.sectionOK {
			t.Errorf("Section(%s, %s): %v", tc.portal.Name, tc.section.Title, got)
		}
	}
}

func TestPageAccessOfAndBack(t *testing.T) {
	c, _ := newAccessChat(t, fakemodel.New("opus", nil))
	page := pageAccessOf(fakeaccess.Parse(customer), c.currentAgents())
	want := pageAccess{"Pet Shop": {"Orders", "Guides"}, "Garden": {"API"}}
	if !reflect.DeepEqual(page, want) {
		t.Errorf("the customer's page access: %v, want %v", page, want)
	}
	value, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if back := readPageAccess(string(value)); !reflect.DeepEqual(back, want) {
		t.Errorf("read back: %v, want %v", back, want)
	}
	for _, value := range []string{"", "{", "null", `["Pet Shop"]`, `{"Pet Shop": "Orders"}`} {
		if back := readPageAccess(value); len(back) != 0 {
			t.Errorf("%q: %v, want an access that hides every portal", value, back)
		}
	}
	if every := pageAccessOf(portal.Everything, c.currentAgents()); len(every) != 3 || len(every["Pet Shop"]) != 4 {
		t.Errorf("Everything: %v", every)
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
	r := httptest.NewRequest(http.MethodGet, "/session", nil)
	r.RemoteAddr = "192.0.2.7:5555"
	r.Header.Set(fakeaccess.Header, customer)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if session["client"] != "192.0.2.7" || !reflect.DeepEqual(readPageAccess(session["access"]), pageAccess{"Pet Shop": {"Orders", "Guides"}, "Garden": {"API"}}) {
		t.Errorf("the session through a portal handler with a hook: %v", session)
	}
	outside, err := c.sessionOf(httptest.NewRequest(http.MethodGet, "/portals/garden/chat", nil))
	if err != nil || len(readPageAccess(outside["access"])) != 0 {
		t.Errorf("the session of a request served by no portal handler: %v, %v, want an access to no portal", outside, err)
	}
	if every := readPageAccess(sessionThroughPortal(t, c)["access"]); len(every) != 3 {
		t.Errorf("the session through a portal handler without a hook: %v, want every portal", every)
	}
}
