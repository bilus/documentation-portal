package portal_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/bilus/documentation-portal/portal"
)

// portalExt is a program's own type for the extension fields of its portals.
type portalExt struct {
	Host  string   `yaml:"x-host"`
	Tiers []string `yaml:"x-tiers"`
	Owner struct {
		Team string `yaml:"team"`
	} `yaml:"x-owner"`
}

const extFile = `
portals:
  - name: Foo
    type: link
    url: https://foo.example.com/
    x-host: foobar
    x-tiers: [gold, silver]
    x-owner: {team: platform}
  - name: Pets
    labels: [apis]
    sections:
      - {title: API, type: spec, input: pets.yaml}
`

func readExt(t *testing.T, file string) portal.Config {
	t.Helper()
	cfg, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}, "pets.yaml": {Data: []byte(pets)}}, "environment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNewWithExtensionFields(t *testing.T) {
	cfg := linked()
	cfg.Portals[0].Ext = map[string]any{"x-host": "foobar"}
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/portals/pets/specs/api"); rec.Code != http.StatusOK {
		t.Errorf("a page of a portal with extension fields: %d", rec.Code)
	}
}

func TestReadConfigReadsExtensionFields(t *testing.T) {
	cfg := readExt(t, extFile)
	want := map[string]any{"x-host": "foobar", "x-tiers": []any{"gold", "silver"}, "x-owner": map[string]any{"team": "platform"}}
	if !reflect.DeepEqual(cfg.Portals[0].Ext, want) {
		t.Errorf("the extension fields: %#v, want %#v", cfg.Portals[0].Ext, want)
	}
	if cfg.Portals[0].Name != "Foo" || cfg.Portals[0].Type != portal.LinkPortal || cfg.Portals[0].URL != "https://foo.example.com/" {
		t.Errorf("the portal beside its extension fields: %+v", cfg.Portals[0])
	}
	if cfg.Portals[1].Ext != nil {
		t.Errorf("a portal without extension fields: %#v, want nil", cfg.Portals[1].Ext)
	}
	if _, err := portal.New(cfg); err != nil {
		t.Errorf("the portal handler: %v", err)
	}
}

func TestReadConfigStillRefusesOtherKeys(t *testing.T) {
	for key, want := range map[string]string{
		"host: foobar":             "host",
		"X-host: foobar":           "X-host",
		"xhost: foobar":            "xhost",
		"x_host: foobar":           "x_host",
		"x-: foobar":               `"x-"`,
		"x-host: {1: a}":           "x-host",
		"x-host: a\n    x-host: b": "x-host",
	} {
		file := "portals:\n  - name: Pets\n    " + key + "\n    sections:\n      - {title: API, type: spec, input: pets.yaml}\n"
		_, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}}, "environment.yaml")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want one naming %s", key, err, want)
		}
	}
	// An x- key anywhere else is an unknown key, as before.
	for _, file := range []string{
		"x-host: foobar\nportals: []\n",
		"portals:\n  - name: Pets\n    sections:\n      - {title: API, type: spec, input: pets.yaml, x-host: foobar}\n",
		"collections:\n  - {name: A, matchLabels: [a], x-host: foobar}\n",
	} {
		if _, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}}, "environment.yaml"); err == nil || !strings.Contains(err.Error(), "x-host") {
			t.Errorf("%q: err = %v, want the key refused", file, err)
		}
	}
}

func TestGetExt(t *testing.T) {
	cfg := readExt(t, extFile)
	ext, err := cfg.Portals[0].GetExt[portalExt]()
	if err != nil {
		t.Fatal(err)
	}
	if ext.Host != "foobar" || !reflect.DeepEqual(ext.Tiers, []string{"gold", "silver"}) || ext.Owner.Team != "platform" {
		t.Errorf("the extension fields: %+v", ext)
	}
	if none, err := cfg.Portals[1].GetExt[portalExt](); err != nil || !reflect.DeepEqual(none, portalExt{}) {
		t.Errorf("a portal without extension fields: %+v, %v, want the zero value", none, err)
	}
	inCode := portal.Portal{Name: "Bar", Ext: map[string]any{"x-host": "bar"}}
	if ext, err := inCode.GetExt[portalExt](); err != nil || ext.Host != "bar" {
		t.Errorf("extension fields set in code: %+v, %v", ext, err)
	}
	type hostAlone struct {
		Host string `yaml:"x-host"`
	}
	if _, err := cfg.Portals[0].GetExt[hostAlone](); err == nil || !strings.Contains(err.Error(), "x-tiers") && !strings.Contains(err.Error(), "x-owner") {
		t.Errorf("a type without two of the fields: err = %v, want one naming a field it lacks", err)
	}
	type hostNumber struct {
		Host int `yaml:"x-host"`
	}
	lone := portal.Portal{Name: "Foo", Ext: map[string]any{"x-host": "foobar"}}
	if _, err := lone.GetExt[hostNumber](); err == nil || !strings.Contains(err.Error(), "Foo") {
		t.Errorf("a value that does not fit the field: err = %v, want one naming the portal", err)
	}
	if ext, err := cfg.Portals[0].GetExt[map[string]any](); err != nil || ext["x-host"] != "foobar" {
		t.Errorf("the extension fields as a map: %v, %v", ext, err)
	}
}

func TestGetExtLeavesThePortalAsItWas(t *testing.T) {
	p := portal.Portal{Name: "Foo", Ext: map[string]any{"x-tiers": []any{"gold"}}}
	ext, err := p.GetExt[map[string]any]()
	if err != nil {
		t.Fatal(err)
	}
	ext["x-tiers"].([]any)[0] = "changed"
	ext["x-new"] = true
	if !reflect.DeepEqual(p.Ext, map[string]any{"x-tiers": []any{"gold"}}) {
		t.Errorf("a write to GetExt's result shows in the portal: %#v", p.Ext)
	}
}

func TestTheAccessHookGetsTheExtensionFields(t *testing.T) {
	cfg := readExt(t, extFile)
	access := &askedAbout{}
	cfg.Access = func(*http.Request) (portal.Access, error) { return access, nil }
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	get(h, "/")
	if len(access.portals) == 0 {
		t.Fatal("the hook was not asked")
	}
	if ext, err := access.portals[0].GetExt[portalExt](); err != nil || ext.Host != "foobar" {
		t.Errorf("the hook's portal: %+v, %v, want its extension fields", ext, err)
	}
}

func TestClone(t *testing.T) {
	p := portal.Portal{
		Name:     "Pets",
		Labels:   []string{"apis"},
		Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "pets.yaml", Labels: []string{"public"}}},
		Ext:      map[string]any{"x-host": "foobar", "x-tiers": []any{"gold", map[string]any{"name": "silver"}}, "x-owner": map[string]any{"team": "platform"}},
	}
	c := p.Clone()
	if !reflect.DeepEqual(c, p) {
		t.Fatalf("the clone %+v differs from %+v", c, p)
	}
	c.Labels[0] = "changed"
	c.Sections[0].Labels[0] = "changed"
	c.Sections[0].Title = "changed"
	c.Ext["x-host"] = "changed"
	c.Ext["x-new"] = true
	c.Ext["x-tiers"].([]any)[0] = "changed"
	c.Ext["x-tiers"].([]any)[1].(map[string]any)["name"] = "changed"
	c.Ext["x-owner"].(map[string]any)["team"] = "changed"
	want := portal.Portal{
		Name:     "Pets",
		Labels:   []string{"apis"},
		Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "pets.yaml", Labels: []string{"public"}}},
		Ext:      map[string]any{"x-host": "foobar", "x-tiers": []any{"gold", map[string]any{"name": "silver"}}, "x-owner": map[string]any{"team": "platform"}},
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("a write to the clone shows in the portal: %+v", p)
	}
	if bare := (portal.Portal{Name: "Bare"}).Clone(); bare.Labels != nil || bare.Sections != nil || bare.Ext != nil {
		t.Errorf("the clone of a portal without lists: %+v, want nil lists", bare)
	}
	s := portal.Section{Title: "API", Labels: []string{"public"}}
	sc := s.Clone()
	sc.Labels[0] = "changed"
	if s.Labels[0] != "public" {
		t.Error("a write to a section's clone shows in the section")
	}
}

// scribbler allows everything, records the portals and sections it is asked
// about, and then writes over every part of them.
type scribbler struct {
	portals  []portal.Portal
	sections []portal.Section
	owners   []portal.Portal // the portal of each section call
}

func (w *scribbler) Portal(p portal.Portal) bool {
	w.portals = append(w.portals, p.Clone())
	p.Ext["x-host"] = "scribbled"
	p.Ext["x-tiers"].([]any)[0] = "scribbled"
	p.Labels[0] = "scribbled"
	p.Sections[0].Labels[0] = "scribbled"
	return true
}

func (w *scribbler) Section(p portal.Portal, s portal.Section) bool {
	w.sections = append(w.sections, s.Clone())
	w.owners = append(w.owners, p.Clone())
	s.Labels[0] = "scribbled"
	p.Ext["x-host"] = "scribbled by a section call"
	p.Labels[0] = "scribbled by a section call"
	return true
}

func TestAHooksWritesReachNoOtherCall(t *testing.T) {
	newConfig := func() portal.Config {
		cfg := linked()
		cfg.Portals = cfg.Portals[:1]
		cfg.Portals[0].Sections[0].Labels = []string{"public"}
		cfg.Portals[0].Ext = map[string]any{"x-host": "foobar", "x-tiers": []any{"gold"}}
		return cfg
	}
	cfg, want := newConfig(), newConfig()
	hook := &scribbler{}
	cfg.Access = func(*http.Request) (portal.Access, error) { return hook, nil }
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if rec := get(h, "/portals/pets/specs/api"); rec.Code != http.StatusOK {
			t.Fatalf("a page: %d", rec.Code)
		}
	}
	if len(hook.portals) != 3 || len(hook.sections) != 3 {
		t.Fatalf("the hook was asked about %d portals and %d sections, want 3 of each", len(hook.portals), len(hook.sections))
	}
	for i := range 3 {
		if !reflect.DeepEqual(hook.owners[i], want.Portals[0]) {
			t.Errorf("request %d: the section call got the portal %+v, which the portal call wrote to", i+1, hook.owners[i])
		}
		if !reflect.DeepEqual(hook.portals[i], want.Portals[0]) || !reflect.DeepEqual(hook.sections[i], want.Portals[0].Sections[0]) {
			t.Errorf("request %d: the hook got %+v and %+v, want the portal as configured", i+1, hook.portals[i], hook.sections[i])
		}
	}
	if !reflect.DeepEqual(cfg.Portals, want.Portals) {
		t.Errorf("the hook's writes show in the program's configuration: %+v", cfg.Portals)
	}
	// A write to the configuration after New reaches no page.
	cfg.Portals[0].Ext["x-host"] = "late"
	cfg.Portals[0].Labels[0] = "late"
	get(h, "/portals/pets/specs/api")
	if got := hook.portals[3]; !reflect.DeepEqual(got, want.Portals[0]) {
		t.Errorf("the hook got %+v after a write to the configuration, want the portal as New read it", got)
	}
	libs, err := portal.NewLibraries(want)
	if err != nil {
		t.Fatal(err)
	}
	libs[0].Portal().Ext["x-host"] = "scribbled"
	if got := libs[0].Portal(); !reflect.DeepEqual(got, want.Portals[0]) {
		t.Errorf("a write to Library.Portal's result shows in the library: %+v", got)
	}
}

func TestReadConfigReadsExtensionFieldsOfAPortalWithSections(t *testing.T) {
	cfg := readExt(t, "portals:\n  - name: Pets\n    x-host: foobar\n    sections:\n      - {title: API, type: spec, input: pets.yaml}\n")
	if ext, err := cfg.Portals[0].GetExt[portalExt](); err != nil || ext.Host != "foobar" {
		t.Errorf("a portal with sections: %+v, %v", ext, err)
	}
}

func TestReadConfigRefusesValuesJSONCannotWrite(t *testing.T) {
	for value, want := range map[string]string{
		".nan":            "not finite",
		".inf":            "not finite",
		"-.inf":           "not finite",
		"[1, [.nan]]":     "not finite",
		"{a: {b: .inf}}":  "not finite",
		"[{1: a}]":        "not a string",
		"{a: {1: b}}":     "not a string",
		"[[{a: {~: b}}]]": "not a string",
	} {
		file := "portals:\n  - name: Pets\n    type: link\n    url: /pets\n    x-ok: fine\n    x-value: " + value + "\n"
		_, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}}, "environment.yaml")
		if err == nil || !strings.Contains(err.Error(), "x-value") || !strings.Contains(err.Error(), want) {
			t.Errorf("x-value: %s: err = %v, want one naming the field and %q", value, err, want)
		}
	}
}

func TestReadConfigNamesEveryUnknownKey(t *testing.T) {
	for keys, want := range map[string]string{
		"x-a: 1\n    zeta: 2":               `unknown key "zeta"`,
		"alpha: 1\n    x-z: 2":              `unknown key "alpha"`,
		"zeta: 1\n    x-m: 2\n    alpha: 3": `unknown keys "alpha", "zeta"`,
	} {
		file := "portals:\n  - name: Pets\n    type: link\n    url: /pets\n    " + keys + "\n"
		for range 5 {
			_, err := portal.ReadConfig(fstest.MapFS{"environment.yaml": {Data: []byte(file)}}, "environment.yaml")
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%q: err = %v, want %s", keys, err, want)
			}
		}
	}
}

func TestGetExtFailsCleanly(t *testing.T) {
	type twoFields struct {
		Host  string `yaml:"x-a-host"`
		Count int    `yaml:"x-b-count"`
	}
	p := portal.Portal{Name: "Foo", Ext: map[string]any{"x-a-host": "foobar", "x-b-count": "many"}}
	if ext, err := p.GetExt[twoFields](); err == nil || ext != (twoFields{}) {
		t.Errorf("a value that does not fit: %+v, %v, want the zero value and an error", ext, err)
	}
	for name, value := range map[string]any{"a channel": make(chan int), "a function": func() {}} {
		p := portal.Portal{Name: "Foo", Ext: map[string]any{"x-host": value}}
		if ext, err := p.GetExt[portalExt](); err == nil || !strings.Contains(err.Error(), "Foo") || !reflect.DeepEqual(ext, portalExt{}) {
			t.Errorf("%s: %+v, %v, want the zero value and an error naming the portal", name, ext, err)
		}
	}
}

// twoSections allows everything, and writes to the portal of each section
// call after recording it.
type twoSections struct{ owners []portal.Portal }

func (w *twoSections) Portal(portal.Portal) bool { return true }
func (w *twoSections) Section(p portal.Portal, s portal.Section) bool {
	w.owners = append(w.owners, p.Clone())
	p.Labels[0] = "scribbled"
	return true
}

func TestEachSectionCallGetsItsOwnPortal(t *testing.T) {
	cfg := linked()
	cfg.Portals = cfg.Portals[:1]
	cfg.Portals[0].Sections = append(cfg.Portals[0].Sections, portal.Section{Title: "Other", Type: portal.SpecSection, Input: "pets.yaml"})
	hook := &twoSections{}
	cfg.Access = func(*http.Request) (portal.Access, error) { return hook, nil }
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	get(h, "/portals/pets/specs/api")
	if len(hook.owners) != 2 || hook.owners[1].Labels[0] != "apis" {
		t.Errorf("the second section call got the portal %+v, which the first wrote to", hook.owners)
	}
}

func TestCloneCopiesValuesSetInCode(t *testing.T) {
	type owner struct {
		Team  string
		Tags  []string
		Since time.Time
	}
	since := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	self := map[string]any{"name": "loop"}
	self["self"] = self
	p := portal.Portal{Name: "Pets", Ext: map[string]any{
		"x-list":    []string{"a", "b"},
		"x-map":     map[string]string{"k": "v"},
		"x-nested":  map[string][]int{"n": {1, 2}},
		"x-pointer": &owner{Team: "platform", Tags: []string{"gold"}, Since: since},
		"x-struct":  owner{Team: "data", Tags: []string{"silver"}},
		"x-array":   [1][]string{{"in an array"}},
		"x-self":    self,
		"x-nil":     nil,
	}}
	c := p.Clone()
	if !reflect.DeepEqual(c.Ext["x-list"], p.Ext["x-list"]) || !reflect.DeepEqual(c.Ext["x-pointer"], p.Ext["x-pointer"]) || !reflect.DeepEqual(c.Ext["x-struct"], p.Ext["x-struct"]) || !reflect.DeepEqual(c.Ext["x-nested"], p.Ext["x-nested"]) {
		t.Fatalf("the clone %+v differs from %+v", c.Ext, p.Ext)
	}
	c.Ext["x-list"].([]string)[0] = "changed"
	c.Ext["x-map"].(map[string]string)["k"] = "changed"
	c.Ext["x-nested"].(map[string][]int)["n"][0] = 99
	c.Ext["x-pointer"].(*owner).Team = "changed"
	c.Ext["x-pointer"].(*owner).Tags[0] = "changed"
	c.Ext["x-struct"].(owner).Tags[0] = "changed"
	arr := c.Ext["x-array"].([1][]string)
	arr[0][0] = "changed"
	c.Ext["x-self"].(map[string]any)["name"] = "changed"
	if got := p.Ext["x-list"].([]string)[0]; got != "a" {
		t.Errorf("a []string is shared: %q", got)
	}
	if got := p.Ext["x-map"].(map[string]string)["k"]; got != "v" {
		t.Errorf("a map[string]string is shared: %q", got)
	}
	if got := p.Ext["x-nested"].(map[string][]int)["n"][0]; got != 1 {
		t.Errorf("a list inside a typed map is shared: %d", got)
	}
	if o := p.Ext["x-pointer"].(*owner); o.Team != "platform" || o.Tags[0] != "gold" || !o.Since.Equal(since) {
		t.Errorf("a pointer's struct is shared: %+v", o)
	}
	if got := p.Ext["x-struct"].(owner).Tags[0]; got != "silver" {
		t.Errorf("a struct's list is shared: %q", got)
	}
	if got := p.Ext["x-array"].([1][]string)[0][0]; got != "in an array" {
		t.Errorf("a list inside an array is shared: %q", got)
	}
	if self["name"] != "loop" {
		t.Errorf("a map that holds itself is shared: %v", self["name"])
	}
	// The copy of a map that holds itself holds itself, not the original.
	loop := c.Ext["x-self"].(map[string]any)
	if inner, ok := loop["self"].(map[string]any); !ok || inner["name"] != "changed" {
		t.Errorf("the clone of a map that holds itself does not hold itself: %v", loop["self"])
	}
	if v, ok := c.Ext["x-nil"]; !ok || v != nil {
		t.Errorf("a nil value: %v, %v", v, ok)
	}
}
