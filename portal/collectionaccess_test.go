package portal_test

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

// rolesFile is a configuration file whose portals and collections each name,
// in the extension field x-roles, the roles of the readers who may see them.
// A customer may see the collection Public and, of its portals, Pets and
// Status; a member of staff may see everything.
const rolesFile = `
portals:
  - name: Pets
    labels: [public]
    x-roles: [customer, staff]
    sections:
      - {title: API, type: spec, input: pets.yaml}
  - name: Orders
    labels: [public]
    x-roles: [staff]
    sections:
      - {title: API, type: spec, input: pets.yaml}
  - name: Billing
    labels: [internal]
    x-roles: [staff]
    sections:
      - {title: API, type: spec, input: pets.yaml}
  - name: Status
    type: link
    url: https://status.example.com/
    labels: [public]
    x-roles: [customer, staff]
collections:
  - name: Public
    matchLabels: [public]
    x-roles: [customer, staff]
  - name: Internal
    matchLabels: [internal]
    x-roles: [staff]
`

// allowedRoles is the program's type for the extension fields of a portal
// and of a collection.
type allowedRoles struct {
	Roles []string `yaml:"x-roles"`
}

// roleAccess is the access of a reader with one role: it allows the portals
// and the collections whose x-roles hold the role, and every section of an
// allowed portal.
type roleAccess string

func (role roleAccess) Portal(p portal.Portal) bool {
	ext, err := p.GetExt[allowedRoles]()
	return err == nil && slices.Contains(ext.Roles, string(role))
}

func (role roleAccess) Section(p portal.Portal, _ portal.Section) bool { return role.Portal(p) }

func (role roleAccess) Collection(c portal.Collection) bool {
	ext, err := c.GetExt[allowedRoles]()
	return err == nil && slices.Contains(ext.Roles, string(role))
}

// byRole is an access hook that reads the reader's role from the request's
// Role header.
func byRole(r *http.Request) (portal.Access, error) {
	return roleAccess(r.Header.Get("Role")), nil
}

// shown allows every portal and section, and the collections with the listed
// names.
type shown []string

func (shown) Portal(portal.Portal) bool                  { return true }
func (shown) Section(portal.Portal, portal.Section) bool { return true }
func (s shown) Collection(c portal.Collection) bool      { return slices.Contains(s, c.Name) }

func TestNewWithACollectionAccess(t *testing.T) {
	quiet(t)
	cfg := stages()
	for i := range cfg.Collections {
		cfg.Collections[i].Ext = map[string]any{"x-roles": []any{"staff"}}
	}
	cfg.Access = func(*http.Request) (portal.Access, error) {
		return shown{"AutoStage API", "Ingest APIs", "Prerelease"}, nil
	}
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(h, "/")
	if home := rec.Body.String(); rec.Code != http.StatusOK || !inOrder(home, "<h2>AutoStage API</h2>", "<h2>Ingest APIs</h2>", "<h2>Prerelease</h2>") {
		t.Errorf("/: %d %q", rec.Code, home)
	}
}

func TestRolesOpenCollectionsAndPortals(t *testing.T) {
	cfg := readExt(t, rolesFile)
	cfg.Access = byRole
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// A customer sees the collection Public, with only the portals open to customers.
	home := getWith(h, "/", "Role", "customer").Body.String()
	if !inOrder(home, "<h2>Public</h2>", link("pets", "Pets"), statusLink) {
		t.Errorf("a customer's home page: %q", home)
	}
	for _, hidden := range []string{"Internal", "Orders", "Billing"} {
		if strings.Contains(home, hidden) {
			t.Errorf("a customer's home page shows %s: %q", hidden, home)
		}
	}
	page := getWith(h, "/portals/pets/specs/api", "Role", "customer")
	if menu := page.Body.String(); page.Code != http.StatusOK || !inOrder(menu, `<span class="portal-menu-collection">Public</span>`, link("pets", "Pets"), statusLink) || strings.Contains(menu, "Internal") || strings.Contains(menu, "Orders") || strings.Contains(menu, "Billing") {
		t.Errorf("a customer's page of Pets: %d %q", page.Code, menu)
	}
	for _, path := range []string{"/portals/orders/specs/api", "/portals/billing/specs/api"} {
		if rec := getWith(h, path, "Role", "customer"); rec.Code != http.StatusNotFound {
			t.Errorf("a customer's %s: %d, want 404", path, rec.Code)
		}
	}

	// A member of staff sees both collections, each with all of its portals.
	home = getWith(h, "/", "Role", "staff").Body.String()
	if !inOrder(home, "<h2>Public</h2>", link("pets", "Pets"), link("orders", "Orders"), statusLink, "<h2>Internal</h2>", link("billing", "Billing")) {
		t.Errorf("a home page of staff: %q", home)
	}
	for _, path := range []string{"/portals/pets/specs/api", "/portals/orders/specs/api", "/portals/billing/specs/api"} {
		rec := getWith(h, path, "Role", "staff")
		if menu := rec.Body.String(); rec.Code != http.StatusOK || !inOrder(menu, `<span class="portal-menu-collection">Public</span>`, link("orders", "Orders"), `<span class="portal-menu-collection">Internal</span>`, link("billing", "Billing")) {
			t.Errorf("%s for staff: %d %q", path, rec.Code, menu)
		}
	}

	// A reader with another role, or with none, sees nothing.
	for _, role := range []string{"guest", ""} {
		if home := getWith(h, "/", "Role", role).Body.String(); !strings.Contains(home, "No portals are open to you.") || strings.Contains(home, "Public") {
			t.Errorf("the home page of the role %q: %q", role, home)
		}
		if rec := getWith(h, "/portals/pets/specs/api", "Role", role); rec.Code != http.StatusNotFound {
			t.Errorf("a page of Pets for the role %q: %d, want 404", role, rec.Code)
		}
	}
}

func TestAHiddenCollectionLeavesItsPortalsPages(t *testing.T) {
	quiet(t)
	cfg := stages()
	access := shown{"Ingest APIs", "Prerelease"}
	cfg.Access = func(*http.Request) (portal.Access, error) { return access, nil }
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Production is in AutoStage API alone: not listed, and still served.
	home := get(h, "/").Body.String()
	if !inOrder(home, "<h2>Ingest APIs</h2>", link("ingest", "Ingest"), "<h2>Prerelease</h2>", link("staging", "Staging")) || strings.Contains(home, "AutoStage API") || strings.Contains(home, "Production") {
		t.Errorf("/ without AutoStage API: %q", home)
	}
	rec := get(h, "/portals/production/specs/api")
	if menu := rec.Body.String(); rec.Code != http.StatusOK || strings.Contains(menu, "AutoStage API") || !inOrder(menu, `<span class="portal-menu-collection">Ingest APIs</span>`, link("ingest", "Ingest")) {
		t.Errorf("a page of Production: %d %q", rec.Code, menu)
	}
	// One listed portal opens at the home page, as with one collection.
	access = shown{"Ingest APIs"}
	if rec := get(h, "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/ingest/specs/api" {
		t.Errorf("/ with one shown collection: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	// No shown collection lists no portal, and is not a configuration without collections.
	access = shown{}
	if home := get(h, "/").Body.String(); !strings.Contains(home, "No portals are open to you.") || strings.Contains(home, "Staging") {
		t.Errorf("/ with no shown collection: %q", home)
	}
}

// scribbling shows every collection, and writes to each after recording it.
type scribbling struct{ asked []portal.Collection }

func (*scribbling) Portal(portal.Portal) bool                  { return true }
func (*scribbling) Section(portal.Portal, portal.Section) bool { return true }
func (w *scribbling) Collection(c portal.Collection) bool {
	w.asked = append(w.asked, c.Clone())
	c.MatchLabels[0] = "scribbled"
	c.Ext["x-roles"].([]any)[0] = "scribbled"
	c.Ext["x-added"] = true
	return true
}

func TestEachCollectionCallGetsItsOwnCollection(t *testing.T) {
	quiet(t)
	cfg := stages()
	for i := range cfg.Collections {
		cfg.Collections[i].Ext = map[string]any{"x-roles": []any{"staff"}}
	}
	want := stages().Collections
	for i := range want {
		want[i].Ext = map[string]any{"x-roles": []any{"staff"}}
	}
	hook := &scribbling{}
	cfg.Access = func(*http.Request) (portal.Access, error) { return hook, nil }
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// A later write to the configuration reaches no request.
	cfg.Collections[0].Ext["x-roles"] = []any{"changed after New"}
	cfg.Collections[0].MatchLabels[0] = "changed after New"
	for request := 1; request <= 2; request++ {
		hook.asked = nil
		home := get(h, "/").Body.String()
		if !reflect.DeepEqual(hook.asked, want) {
			t.Errorf("request %d: the access was asked about %+v, want each collection once, as configured: %+v", request, hook.asked, want)
		}
		if !inOrder(home, "<h2>AutoStage API</h2>", link("staging", "Staging"), "<h2>Ingest APIs</h2>", "<h2>Prerelease</h2>") {
			t.Errorf("request %d: /: %q", request, home)
		}
	}
}

func TestLibraryMenuHidesACollection(t *testing.T) {
	quiet(t)
	libs, err := portal.NewLibraries(stages())
	if err != nil {
		t.Fatal(err)
	}
	menu, collections := libs[0].Menu(shown{"Ingest APIs", "Prerelease"})
	ingest := portal.PortalLink{Name: "Ingest", URL: "/portals/ingest/specs/api"}
	staging := portal.PortalLink{Name: "Staging", URL: "/portals/staging/specs/api"}
	if !reflect.DeepEqual(menu, []portal.PortalLink{staging, ingest}) {
		t.Errorf("the menu: %+v", menu)
	}
	if want := []portal.MenuCollection{{Name: "Ingest APIs", Portals: []portal.PortalLink{ingest}}, {Name: "Prerelease", Portals: []portal.PortalLink{staging}}}; !reflect.DeepEqual(collections, want) {
		t.Errorf("the menu's collections: %+v, want %+v", collections, want)
	}
	// An access without a Collection method shows every collection.
	if _, collections := libs[0].Menu(named{"Staging", "Production", "Ingest"}); len(collections) != 3 {
		t.Errorf("the menu's collections for an access that is no collection access: %+v", collections)
	}
}

func TestReadConfigReadsACollectionsExtensionFields(t *testing.T) {
	cfg := readExt(t, rolesFile)
	want := []portal.Collection{
		{Name: "Public", MatchLabels: []string{"public"}, Ext: map[string]any{"x-roles": []any{"customer", "staff"}}},
		{Name: "Internal", MatchLabels: []string{"internal"}, Ext: map[string]any{"x-roles": []any{"staff"}}},
	}
	if !reflect.DeepEqual(cfg.Collections, want) {
		t.Errorf("the collections: %#v, want %#v", cfg.Collections, want)
	}
	plain := readExt(t, "portals:\n  - name: Pets\n    labels: [a]\n    sections:\n      - {title: API, type: spec, input: pets.yaml}\ncollections:\n  - {name: A, matchLabels: [a]}\n")
	if plain.Collections[0].Ext != nil {
		t.Errorf("a collection without extension fields: %#v, want nil", plain.Collections[0].Ext)
	}
}

func TestReadConfigRefusesOtherKeysOfACollection(t *testing.T) {
	for key, want := range map[string]string{
		"host: foobar":   "host",
		"X-roles: [a]":   "X-roles",
		"x_roles: [a]":   "x_roles",
		"x-: [a]":        "x-",
		"x-rate: .nan":   "x-rate",
		"x-rate: .inf":   "x-rate",
		"x-map: {1: a}":  "x-map",
		"labels: [a, b]": "labels",
	} {
		file := "collections:\n  - name: A\n    matchLabels: [a]\n    " + key + "\n"
		_, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}}, "environment.yaml")
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), `collection "A"`) {
			t.Errorf("%q: err = %v, want one naming the collection and %s", key, err, want)
		}
	}
	// YAML itself refuses a repeated key, before the collection has a name.
	repeated := "collections:\n  - name: A\n    matchLabels: [a]\n    x-roles: [a]\n    x-roles: [b]\n"
	if _, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(repeated)}}, "environment.yaml"); err == nil || !strings.Contains(err.Error(), "x-roles") {
		t.Errorf("a repeated key: err = %v, want one naming x-roles", err)
	}
}

func TestOneReadersCollectionsLeaveTheNextReadersAsTheyWere(t *testing.T) {
	quiet(t)
	cfg := stages()
	access := shown{"Prerelease"}
	cfg.Access = func(*http.Request) (portal.Access, error) { return access, nil }
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Staging is in AutoStage API and Prerelease; this reader sees it under Prerelease alone.
	if rec := get(h, "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/portals/staging/specs/api" {
		t.Errorf("/ with Prerelease alone: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	access = shown{"AutoStage API", "Ingest APIs", "Prerelease"}
	home := get(h, "/").Body.String()
	if !inOrder(home,
		"<h2>AutoStage API</h2>", link("staging", "Staging"), link("production", "Production"), link("ingest", "Ingest"),
		"<h2>Ingest APIs</h2>", link("ingest", "Ingest"),
		"<h2>Prerelease</h2>", link("staging", "Staging"),
	) || strings.Count(home, link("staging", "Staging")) != 2 {
		t.Errorf("/ for the next reader, who sees every collection: %q", home)
	}
}

func TestCollectionGetExt(t *testing.T) {
	c := portal.Collection{Name: "Public", MatchLabels: []string{"public"}, Ext: map[string]any{"x-roles": []any{"customer", "staff"}}}
	ext, err := c.GetExt[allowedRoles]()
	if err != nil || !reflect.DeepEqual(ext.Roles, []string{"customer", "staff"}) {
		t.Errorf("the extension fields: %+v, %v", ext, err)
	}
	// The result shares nothing with the collection.
	ext.Roles[0] = "changed"
	if got := c.Ext["x-roles"].([]any)[0]; got != "customer" {
		t.Errorf("a write to the result reached the collection: %v", got)
	}
	if ext, err := (portal.Collection{Name: "Plain"}).GetExt[allowedRoles](); err != nil || ext.Roles != nil {
		t.Errorf("a collection without extension fields: %+v, %v", ext, err)
	}
	type other struct {
		Host string `yaml:"x-host"`
	}
	if ext, err := c.GetExt[other](); err == nil || !strings.Contains(err.Error(), `collection "Public"`) || !strings.Contains(err.Error(), "x-roles") || ext != (other{}) {
		t.Errorf("a field the type lacks: %+v, %v, want an error naming the collection and the field", ext, err)
	}
	type wrong struct {
		Roles int `yaml:"x-roles"`
	}
	if ext, err := c.GetExt[wrong](); err == nil || !strings.Contains(err.Error(), `collection "Public"`) || ext != (wrong{}) {
		t.Errorf("a value that does not fit: %+v, %v", ext, err)
	}
	c.Ext["x-roles"] = make(chan int)
	if _, err := c.GetExt[map[string]any](); err == nil {
		t.Error("a value that YAML cannot write: no error")
	}
}

func TestACollectionsCloneSharesNothing(t *testing.T) {
	c := portal.Collection{Name: "Public", MatchLabels: []string{"public", "apis"}, Ext: map[string]any{
		"x-roles": []any{"customer"},
		"x-owner": map[string]any{"team": "platform"},
		"x-typed": []string{"set in code"},
	}}
	clone := c.Clone()
	if !reflect.DeepEqual(clone, c) {
		t.Fatalf("the clone %+v differs from %+v", clone, c)
	}
	clone.MatchLabels[0] = "changed"
	clone.Ext["x-roles"].([]any)[0] = "changed"
	clone.Ext["x-owner"].(map[string]any)["team"] = "changed"
	clone.Ext["x-typed"].([]string)[0] = "changed"
	clone.Ext["x-added"] = true
	want := portal.Collection{Name: "Public", MatchLabels: []string{"public", "apis"}, Ext: map[string]any{
		"x-roles": []any{"customer"},
		"x-owner": map[string]any{"team": "platform"},
		"x-typed": []string{"set in code"},
	}}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("a write to the clone reached the collection: %+v", c)
	}
	if plain := (portal.Collection{Name: "Plain"}).Clone(); plain.MatchLabels != nil || plain.Ext != nil {
		t.Errorf("the clone of a collection without labels and fields: %+v", plain)
	}
}
