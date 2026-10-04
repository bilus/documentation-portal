package portal

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// titled allows the portals and sections with the listed names and titles.
type titled []string

func (t titled) Portal(p Portal) bool             { return slices.Contains(t, p.Name) }
func (t titled) Section(_ Portal, s Section) bool { return slices.Contains(t, s.Title) }

// threeSites returns the sites of the portals Pets, with the sections API
// and Guides, Store, with the section Orders, and Vault, with the section
// Gold, each with the others as its portals.
func threeSites() []*site {
	newSite := func(name string, titles ...string) *site {
		s := &site{config: Portal{Name: name}, name: name, slug: strings.ToLower(name)}
		for _, title := range titles {
			sec := Section{Title: title, Type: SpecSection, Input: strings.ToLower(title) + ".yaml"}
			s.config.Sections = append(s.config.Sections, sec)
			s.sections = append(s.sections, &section{Section: sec, config: sec, slug: strings.ToLower(title)})
		}
		return s
	}
	sites := []*site{newSite("Pets", "API", "Guides"), newSite("Store", "Orders"), newSite("Vault", "Gold")}
	for _, s := range sites {
		s.portals = sites
	}
	return sites
}

// titlesOf returns the titles of the sections of s.
func titlesOf(s *site) []string {
	var titles []string
	for _, sec := range s.sections {
		titles = append(titles, sec.Title)
	}
	return titles
}

func TestVisibleTo(t *testing.T) {
	pets := threeSites()[0]
	v, ok := pets.visibleTo(titled{"Pets", "Guides"})
	if !ok || v == pets || !slices.Equal(titlesOf(v), []string{"Guides"}) || v.sections[0] != pets.sections[1] || v.slug != "pets" {
		t.Errorf("Pets with Guides alone: %+v, %v", v, ok)
	}
	if !slices.Equal(titlesOf(pets), []string{"API", "Guides"}) {
		t.Errorf("visibleTo changed the site it copies: %v", titlesOf(pets))
	}
	if v.whole != pets || pets.whole != nil {
		t.Errorf("the copy keeps %p as the whole site, want %p", v.whole, pets)
	}
	if again, ok := v.visibleTo(titled{"Pets", "Guides"}); !ok || again.whole != pets {
		t.Errorf("a copy of a copy keeps %+v as the whole site, want the first site", again)
	}
	for name, access := range map[string]Access{
		"a hidden portal":                    titled{"API", "Guides"},
		"a portal without a visible section": titled{"Pets"},
		"no access":                          nil,
		"nothing":                            nothing{},
	} {
		if v, ok := pets.visibleTo(access); ok || v != nil {
			t.Errorf("%s: %+v, %v, want no site", name, v, ok)
		}
	}
	if v, ok := pets.visibleTo(Everything); !ok || !slices.Equal(titlesOf(v), []string{"API", "Guides"}) {
		t.Errorf("Everything: %+v, %v", v, ok)
	}
}

func TestVisibleSites(t *testing.T) {
	sites := threeSites()
	visible := visibleSites(sites, titled{"Pets", "Guides", "Vault", "Gold"})
	if len(visible) != 2 || visible[0].name != "Pets" || visible[1].name != "Vault" || !slices.Equal(titlesOf(visible[0]), []string{"Guides"}) {
		t.Fatalf("the visible sites: %+v", visible)
	}
	for _, v := range visible {
		if len(v.portals) != 2 || v.portals[0] != visible[0] || v.portals[1] != visible[1] {
			t.Errorf("%s's portals: %+v, want the visible sites", v.name, v.portals)
		}
	}
	if len(sites[0].portals) != 3 || len(sites[0].sections) != 2 {
		t.Errorf("visibleSites changed the sites: %+v", sites[0])
	}
	if visible := visibleSites(sites, nothing{}); len(visible) != 0 {
		t.Errorf("nothing: %+v", visible)
	}
}

func TestViewFor(t *testing.T) {
	rt := &router{sites: threeSites()}
	access := titled{"Store", "Orders", "Vault"}
	v := rt.viewFor(access)
	if got, ok := v.access.(titled); !ok || !slices.Equal(got, access) || len(v.sites) != 1 || v.sites[0].name != "Store" {
		t.Errorf("the view of a reader of Store: %+v", v)
	}
	if v := rt.viewFor(Everything); v.access != Everything || len(v.sites) != 3 {
		t.Errorf("the view of Everything: %+v", v)
	}
	if v := rt.viewFor(nil); v.access != nil || len(v.sites) != 0 {
		t.Errorf("the view of no access: %+v", v)
	}
}

func TestReaderAccess(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	calls := 0
	allow := titled{"Pets", "API"}
	for name, tc := range map[string]struct {
		hook func(*http.Request) (Access, error)
		want Access
		logs string
	}{
		"an access": {func(*http.Request) (Access, error) { calls++; return allow, nil }, allow, ""},
		"an error": {func(*http.Request) (Access, error) {
			calls++
			return allow, errors.New("the token expired")
		}, nothing{}, "access hook: the token expired"},
		"no access": {func(*http.Request) (Access, error) { calls++; return nil, nil }, nothing{}, "access hook: no access"},
		"no hook":   {nil, Everything, ""},
	} {
		logged.Reset()
		calls = 0
		got := (&router{access: tc.hook}).readerAccess(r)
		if gotAllow, ok := got.(titled); ok {
			if !slices.Equal(gotAllow, allow) {
				t.Errorf("%s: %v", name, got)
			}
		} else if got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
		if tc.hook != nil && calls != 1 {
			t.Errorf("%s: %d calls of the hook, want one", name, calls)
		}
		if line := logged.String(); tc.logs == "" && line != "" || !strings.Contains(line, tc.logs) {
			t.Errorf("%s: the log %q, want %q", name, line, tc.logs)
		}
	}
}

func TestPrivateWriter(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"a status first": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			w.Write([]byte("tea"))
		},
		"a write first": func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("tea")) },
		"a deleted header": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Del("Cache-Control")
			http.Error(w, "gone", http.StatusNotFound)
		},
		"a public header": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Write([]byte("tea"))
		},
	} {
		rec := httptest.NewRecorder()
		h(&privateWriter{ResponseWriter: rec}, httptest.NewRequest(http.MethodGet, "/", nil))
		if got := rec.Result().Header.Get("Cache-Control"); got != "private" {
			t.Errorf("%s: Cache-Control %q, want private", name, got)
		}
	}
	rec := httptest.NewRecorder()
	(&privateWriter{ResponseWriter: rec}).Write([]byte("tea"))
	if rec.Code != http.StatusOK || rec.Body.String() != "tea" {
		t.Errorf("a write without a status: %d %q", rec.Code, rec.Body)
	}
	if err := http.NewResponseController(&privateWriter{ResponseWriter: rec}).Flush(); err != nil {
		t.Errorf("a flush through the response controller: %v", err)
	}
}

func TestAccessOfANilView(t *testing.T) {
	for name, v := range map[string]*view{"a nil view": nil, "a view without an access": {}} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), viewKey{}, v))
		if got := AccessOf(r); got == nil || got.Portal(Portal{Name: "Pets"}) {
			t.Errorf("%s: %v, want an access that allows nothing", name, got)
		}
	}
}
