package portal

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAssetsInNamesMissingFiles(t *testing.T) {
	_, err := assetsIn(fstest.MapFS{"elements/.gitkeep": {}, "elements/styles.min.css": {Data: []byte("/**/")}, "elements/LICENSE": {Data: []byte("Apache-2.0")}})
	if err == nil || !strings.Contains(err.Error(), "web-components.min.js") || strings.Contains(err.Error(), "styles.min.css") || !strings.Contains(err.Error(), "make setup") {
		t.Errorf("err = %v, want it to name web-components.min.js alone and make setup", err)
	}

	_, err = assetsIn(fstest.MapFS{
		"elements/web-components.min.js": {Data: []byte("//")},
		"elements/styles.min.css":        {Data: []byte("/**/")},
	})
	if err == nil || !strings.Contains(err.Error(), "LICENSE") {
		t.Errorf("err = %v, want it to name LICENSE", err)
	}

	dir, err := assetsIn(fstest.MapFS{
		"elements/web-components.min.js": {Data: []byte("//")},
		"elements/styles.min.css":        {Data: []byte("/**/")},
		"elements/LICENSE":               {Data: []byte("Apache-2.0")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(dir, "web-components.min.js"); err != nil {
		t.Errorf("the returned directory does not hold the script: %v", err)
	}
}

// specSection returns a spec section titled API for input.
func specSection(input string) Section {
	return Section{Title: "API", Type: SpecSection, Input: input}
}

// docsSection returns a docs section titled Documents for input, with the
// toc path toc.
func docsSection(input, toc string) Section {
	return Section{Title: "Documents", Type: DocsSection, Input: input, Toc: toc}
}

func TestOpenSections(t *testing.T) {
	root := fstest.MapFS{"apis/pets.yaml": {Data: []byte("openapi: 3.1.0\n")}, "docs/a.md": {Data: []byte("# A\n")}}
	if sections, err := openSections(root, []Section{specSection("apis/pets.yaml")}); err != nil || len(sections) != 1 || sections[0].docs != nil {
		t.Errorf("valid config: %v, %v", sections, err)
	}
	if _, err := openSections(nil, []Section{specSection("apis/pets.yaml")}); err == nil {
		t.Error("nil Root accepted")
	}
	for _, path := range []string{".", "", "/etc/passwd", "../pets.yaml", "apis/../../pets.yaml"} {
		_, err := openSections(root, []Section{specSection(path)})
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%q: err = %v", path, err)
		}
	}
	for path, file := range map[string]string{"docs": "a.md", ".": "docs/a.md", "docs/": "a.md"} {
		sections, err := openSections(root, []Section{specSection("apis/pets.yaml"), docsSection(path, "")})
		if err != nil {
			t.Errorf("content directory path %q: %v", path, err)
		} else if _, err := fs.Stat(sections[1].docs, file); err != nil {
			t.Errorf("content directory %q: %v", path, err)
		}
	}
	for _, path := range []string{"missing", "docs/a.md", "/docs", "../docs"} {
		_, err := openSections(root, []Section{specSection("apis/pets.yaml"), docsSection(path, "")})
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("content directory path %q: err = %v", path, err)
		}
	}
}

func TestOpenSectionsRefusesSymlinkedDocsPath(t *testing.T) {
	link := func(target string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink}
	}
	root := fstest.MapFS{
		"api.yaml":             {Data: []byte("openapi: 3.1.0\n")},
		"elsewhere/a.md":       {Data: []byte("# A\n")},
		"elsewhere/sub/b.md":   {Data: []byte("# B\n")},
		"docs":                 link("elsewhere"),
		"up":                   link("elsewhere"),
		"real/guides/intro.md": {Data: []byte("# Intro\n")},
	}
	open := func(path string) error {
		_, err := openSections(root, []Section{specSection("api.yaml"), docsSection(path, "")})
		return err
	}
	for _, path := range []string{"docs", "up/sub"} {
		if err := open(path); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("content directory path %q: err = %v, want a refusal", path, err)
		}
	}
	if err := open("real/guides"); err != nil {
		t.Errorf("real/guides: %v", err)
	}
	if err := open("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: err = %v, want the cause fs.ErrNotExist", err)
	}
}

func TestOpenSectionsChecksTocPath(t *testing.T) {
	root := fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.0.3\n")}, "docs/a.md": {Data: []byte("# A\n")}}
	for _, p := range []string{"../toc.json", "/toc.json", ".", "nav/../toc.json", "nav//toc.json"} {
		if _, err := openSections(root, []Section{specSection("api.yaml"), docsSection("docs", p)}); err == nil || !strings.Contains(err.Error(), "toc path") {
			t.Errorf("%q: err = %v, want one about the toc path", p, err)
		}
	}
	// A missing toc file is no error at startup: the sidebar falls back.
	for _, p := range []string{"", "toc.json", "nav/toc.json"} {
		if _, err := openSections(root, []Section{specSection("api.yaml"), docsSection("docs", p)}); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	// A toc file lays out the sidebar of the document pages, which need a
	// content directory.
	spec := specSection("api.yaml")
	spec.Toc = "toc.json"
	if _, err := openSections(root, []Section{spec}); err == nil || !strings.Contains(err.Error(), "content directory") {
		t.Errorf("a toc on a spec section: err = %v", err)
	}
}

func TestSlugOf(t *testing.T) {
	for title, want := range map[string]string{
		"API":             "api",
		"Getting Started": "getting-started",
		"Store API":       "store-api",
		"  REST / gRPC  ": "rest-grpc",
		"v2.0 API":        "v2-0-api",
		"C++ SDK":         "c-sdk",
		"API!":            "api",
		"a__b":            "a-b",
		"Café":            "café",
		"ガイド":             "ガイド",
		"--":              "",
		"":                "",
	} {
		if got := slugOf(title); got != want {
			t.Errorf("slugOf(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestOpenSectionsChecksEverySection(t *testing.T) {
	root := fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.1.0\n")}, "docs/a.md": {Data: []byte("# A\n")}, "ops/b.md": {Data: []byte("# B\n")}}
	spec, docs := specSection("api.yaml"), docsSection("docs", "")
	with := func(s Section, change func(*Section)) Section {
		change(&s)
		return s
	}
	for name, tc := range map[string]struct {
		sections []Section
		want     string
	}{
		"no sections":       {nil, "no sections"},
		"no title":          {[]Section{spec, with(docs, func(s *Section) { s.Title = "" })}, "section 2 has no title"},
		"an empty slug":     {[]Section{with(spec, func(s *Section) { s.Title = "--" })}, `"--"`},
		"no input":          {[]Section{with(spec, func(s *Section) { s.Input = "" })}, "input"},
		"only ./":           {[]Section{with(docs, func(s *Section) { s.Input = "./" })}, "input"},
		"only /":            {[]Section{with(docs, func(s *Section) { s.Input = "/" })}, "input"},
		"an unknown type":   {[]Section{with(spec, func(s *Section) { s.Type = "openapi" })}, `"openapi"`},
		"no type":           {[]Section{with(spec, func(s *Section) { s.Type = "" })}, "type"},
		"one slug twice":    {[]Section{spec, with(docs, func(s *Section) { s.Title = "API!" })}, `"api"`},
		"a spec's toc":      {[]Section{with(spec, func(s *Section) { s.Toc = "toc.json" })}, "content directory"},
		"a toc of ./":       {[]Section{with(docs, func(s *Section) { s.Toc = "./" })}, "toc path"},
		"a later docs miss": {[]Section{spec, docs, docsSection("missing", "")}, "missing"},
		"a later bad toc":   {[]Section{spec, docs, with(docsSection("ops", ""), func(s *Section) { s.Title, s.Toc = "Ops", "../toc.json" })}, "../toc.json"},
	} {
		if _, err := openSections(root, tc.sections); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming %s", name, err, tc.want)
		}
	}

	sections, err := openSections(root, []Section{
		with(docs, func(s *Section) { s.Input, s.Toc = "./docs/", "./toc.json" }),
		with(spec, func(s *Section) { s.Title, s.Input = "Store API", "./api.yaml" }),
		with(docsSection("ops/", ""), func(s *Section) { s.Title = "Ops" }),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct{ slug, input, toc, file string }{
		{"documents", "docs", "toc.json", "a.md"},
		{"store-api", "api.yaml", "", ""},
		{"ops", "ops", "", "b.md"},
	} {
		sec := sections[i]
		if sec.slug != want.slug || sec.Input != want.input || sec.Toc != want.toc {
			t.Errorf("section %d: %q, %q, %q, want %+v", i, sec.slug, sec.Input, sec.Toc, want)
		}
		if want.file == "" {
			if sec.docs != nil {
				t.Errorf("section %d: a content directory for a spec section", i)
			}
		} else if sec.docs == nil {
			t.Errorf("section %d: no content directory", i)
		} else if _, err := fs.Stat(sec.docs, want.file); err != nil {
			t.Errorf("section %d: %v", i, err)
		}
	}
	if _, err := openSections(root, []Section{docs}); err != nil {
		t.Errorf("docs without a spec: %v", err)
	}
}

func TestSlugOfKeepsDigitsOfAnyScript(t *testing.T) {
	if got := slugOf("Version ٣"); got != "version-٣" {
		t.Errorf(`slugOf("Version ٣") = %q, want "version-٣"`, got)
	}
}

func TestOpenSectionsRefusesTwoTrailingSlashes(t *testing.T) {
	root := fstest.MapFS{"docs/a.md": {Data: []byte("# A\n")}}
	if _, err := openSections(root, []Section{docsSection("docs//", "")}); err == nil || !strings.Contains(err.Error(), "docs/") {
		t.Errorf("docs//: err = %v, want a refusal", err)
	}
}

func TestOpenPortals(t *testing.T) {
	root := fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.1.0\n")}, "docs/a.md": {Data: []byte("# A\n")}}
	api := []Section{specSection("api.yaml")}
	sites, err := openPortals(Config{Root: root, Portals: []Portal{
		{Name: "Pet Shop", Sections: api},
		{Name: "Store", Sections: []Section{specSection("api.yaml"), docsSection("docs", "")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 || sites[0].name != "Pet Shop" || sites[0].slug != "pet-shop" || len(sites[0].sections) != 1 ||
		sites[1].slug != "store" || len(sites[1].sections) != 2 || sites[1].sections[1].docs == nil {
		t.Errorf("sites: %+v", sites)
	}
	for name, tc := range map[string]struct {
		cfg  Config
		want string
	}{
		"no root":        {Config{Portals: []Portal{{Name: "Pets", Sections: api}}}, "documentation root"},
		"no portals":     {Config{Root: root}, "no portals"},
		"no name":        {Config{Root: root, Portals: []Portal{{Name: "Pets", Sections: api}, {Sections: api}}}, "portal 2 has no name"},
		"an empty slug":  {Config{Root: root, Portals: []Portal{{Name: "--", Sections: api}}}, `"--"`},
		"one slug twice": {Config{Root: root, Portals: []Portal{{Name: "Pets", Sections: api}, {Name: "PETS", Sections: api}}}, `"pets"`},
		"a later section": {Config{Root: root, Portals: []Portal{{Name: "Pets", Sections: api}, {Name: "Store", Sections: []Section{docsSection("missing", "")}}}},
			`portal "Store": section "Documents"`},
	} {
		if _, err := openPortals(tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming %s", name, err, tc.want)
		}
	}
}

func TestOpenPortalsRefusesASlugTwoPortalsApart(t *testing.T) {
	root := fstest.MapFS{"api.yaml": {Data: []byte("openapi: 3.1.0\n")}}
	api := []Section{specSection("api.yaml")}
	if _, err := openPortals(Config{Root: root, Portals: []Portal{{Name: "Pets", Sections: api}, {Name: "Store", Sections: api}, {Name: "PETS", Sections: api}}}); err == nil || !strings.Contains(err.Error(), `"pets"`) {
		t.Errorf("err = %v, want one naming the slug pets", err)
	}
}
