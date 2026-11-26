package portal_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

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
