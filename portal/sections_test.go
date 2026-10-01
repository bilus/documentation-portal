package portal_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

const store = "openapi: 3.1.0\ninfo:\n  title: Store\n  version: 1.0.0\npaths:\n  /orders:\n    get:\n      operationId: listOrders\n      summary: List the orders\n"

// newSections returns the portal of root with sections, or fails the test.
func newSections(t *testing.T, root fstest.MapFS, sections ...portal.Section) http.Handler {
	t.Helper()
	h, err := portal.New(portal.Config{Root: root, Sections: sections})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestSpecSections(t *testing.T) {
	h := newSections(t, fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}, "store.yaml": {Data: []byte(store)}},
		portal.Section{Title: "Pets", Type: portal.SpecSection, Input: "./apis/pets.yaml"},
		portal.Section{Title: "Store API", Type: portal.SpecSection, Input: "store.yaml"},
		portal.Section{Title: "Gone", Type: portal.SpecSection, Input: "gone.yaml"},
	)
	if rec := get(h, "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/specs/pets" {
		t.Errorf("/: %d to %q, want the first section's page", rec.Code, rec.Header().Get("Location"))
	}
	for slug, want := range map[string]struct{ title, raw string }{"pets": {"Pets", pets}, "store-api": {"Store", store}} {
		page := get(h, "/specs/"+slug).Body.String()
		if !strings.Contains(page, "<title>"+want.title+"</title>") || !strings.Contains(page, `apiDescriptionUrl="/api/specs/`+slug+`"`) {
			t.Errorf("/specs/%s: %q", slug, page)
		}
		if raw := get(h, "/api/specs/"+slug); raw.Code != http.StatusOK || raw.Body.String() != want.raw {
			t.Errorf("/api/specs/%s: %d %q", slug, raw.Code, raw.Body)
		}
	}
	nav := get(h, "/specs/store-api").Body.String()
	p, s, g := strings.Index(nav, `<a href="/specs/pets">Pets</a>`), strings.Index(nav, `<a href="/specs/store-api">Store API</a>`), strings.Index(nav, `<a href="/specs/gone">Gone</a>`)
	if p < 0 || s < p || g < s {
		t.Errorf("the navigation bar does not link the sections in order: %q", nav)
	}
	if rec := get(h, "/specs/gone"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "gone.yaml") {
		t.Errorf("a spec section without its file: %d %q", rec.Code, rec.Body)
	}
	for _, path := range []string{"/specs/apis/pets.yaml", "/specs/store.yaml", "/api/specs/store.yaml", "/specs/other", "/api/specs/other", "/specs/Pets"} {
		if rec := get(h, path); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "openapi") {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body)
		}
	}
}

func TestSectionChecks(t *testing.T) {
	root := fstest.MapFS{"api.yaml": {Data: []byte(pets)}, "docs/a.md": {Data: []byte("# A\n")}}
	spec := portal.Section{Title: "API", Type: portal.SpecSection, Input: "api.yaml"}
	docs := portal.Section{Title: "Guides", Type: portal.DocsSection, Input: "docs"}
	with := func(s portal.Section, change func(*portal.Section)) portal.Section {
		change(&s)
		return s
	}
	for name, tc := range map[string]struct {
		sections []portal.Section
		want     string
	}{
		"no sections":     {nil, "no sections"},
		"no title":        {[]portal.Section{with(spec, func(s *portal.Section) { s.Title = "" })}, "title"},
		"an empty slug":   {[]portal.Section{with(spec, func(s *portal.Section) { s.Title = "--" })}, `"--"`},
		"no input":        {[]portal.Section{with(spec, func(s *portal.Section) { s.Input = "" })}, "input"},
		"no docs input":   {[]portal.Section{with(docs, func(s *portal.Section) { s.Input = "./" })}, "input"},
		"an unknown type": {[]portal.Section{with(spec, func(s *portal.Section) { s.Type = "openapi" })}, "openapi"},
		"one slug twice":  {[]portal.Section{spec, with(docs, func(s *portal.Section) { s.Title = "API!" })}, `"api"`},
		"a spec outside":  {[]portal.Section{with(spec, func(s *portal.Section) { s.Input = "../api.yaml" })}, "../api.yaml"},
		"docs outside":    {[]portal.Section{spec, with(docs, func(s *portal.Section) { s.Input = "./../docs/" })}, "../docs"},
		"a file as docs":  {[]portal.Section{spec, with(docs, func(s *portal.Section) { s.Input = "docs/a.md" })}, "docs/a.md"},
		"a toc outside":   {[]portal.Section{spec, with(docs, func(s *portal.Section) { s.Toc = "../toc.json" })}, "../toc.json"},
		"a spec's toc":    {[]portal.Section{with(spec, func(s *portal.Section) { s.Toc = "toc.json" })}, "toc"},
		"a later problem": {[]portal.Section{spec, docs, with(docs, func(s *portal.Section) { s.Title, s.Input = "More", "missing" })}, "missing"},
	} {
		_, err := portal.New(portal.Config{Root: root, Sections: tc.sections})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming %s", name, err, tc.want)
		}
	}
	h := newSections(t, root,
		with(docs, func(s *portal.Section) { s.Input, s.Toc = "./docs/", "./toc.json" }),
		with(spec, func(s *portal.Section) { s.Input = "./api.yaml" }),
	)
	if rec := get(h, "/specs/api"); rec.Code != http.StatusOK {
		t.Errorf("a spec input with ./ in front: %d", rec.Code)
	}
	newSections(t, root, docs)
}

func TestLinksToSpecSections(t *testing.T) {
	page := "# Links\n\n- [pets](apis/pets.yaml)\n- [store](store.yaml)\n- [orders](store.yaml/paths/~1orders/get)\n- [up](../store.yaml)\n"
	root := fstest.MapFS{
		"apis/pets.yaml": {Data: []byte(pets)},
		"store.yaml":     {Data: []byte(store)},
		"docs/links.md":  {Data: []byte(page)},
		"toc.json":       {Data: []byte(`{"items": [{"type": "item", "title": "Orders", "uri": "store.yaml/paths/~1orders/get"}, {"type": "item", "title": "Links", "uri": "docs/links.md"}]}`)},
	}
	h := newSections(t, root,
		portal.Section{Title: "Pets", Type: portal.SpecSection, Input: "apis/pets.yaml"},
		portal.Section{Title: "Store", Type: portal.SpecSection, Input: "store.yaml"},
		portal.Section{Title: "Store again", Type: portal.SpecSection, Input: "./store.yaml"},
		portal.Section{Title: "Documents", Type: portal.DocsSection, Input: "docs", Toc: "toc.json"},
	)
	body := get(h, "/docs/documents/links.md").Body.String()
	hrefs := linkHrefs(body)
	for text, want := range map[string]string{
		"pets":   "/specs/pets",
		"store":  "/specs/store",
		"orders": "/specs/store#/operations/listOrders",
		"up":     "/specs/store",
	} {
		if hrefs[text] != want {
			t.Errorf("%s: href %q, want %q", text, hrefs[text], want)
		}
	}
	if !strings.Contains(body, `href="/specs/store#/operations/listOrders"><div title="Orders"`) {
		t.Errorf("the toc entry does not open the operation in its spec section: %q", body)
	}
}

func TestDocsSections(t *testing.T) {
	root := fstest.MapFS{
		"api.yaml":            {Data: []byte(pets)},
		"guides/start.md":     {Data: []byte("# Start\n\n![flow](img/flow.png)\n")},
		"guides/img/flow.png": {Data: []byte("\x89PNG\r\n\x1a\n")},
		"ops/runbook.md":      {Data: []byte("# Runbook\n")},
		"ops/.draft.md":       {Data: []byte("# Draft\n")},
		"ops-toc.json":        {Data: []byte(`{"items": [{"type": "item", "title": "The runbook", "uri": "ops/runbook.md"}]}`)},
	}
	h := newSections(t, root,
		portal.Section{Title: "Guides", Type: portal.DocsSection, Input: "./guides/"},
		portal.Section{Title: "API", Type: portal.SpecSection, Input: "api.yaml"},
		portal.Section{Title: "Operations Manual", Type: portal.DocsSection, Input: "ops", Toc: "ops-toc.json"},
	)
	if rec := get(h, "/"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/docs/guides/" {
		t.Errorf("/: %d to %q, want the first section's document list", rec.Code, rec.Header().Get("Location"))
	}
	guides := get(h, "/docs/guides/").Body.String()
	if !strings.Contains(guides, `href="/docs/guides/start.md"`) || strings.Contains(guides, "runbook") || !strings.Contains(guides, ">Guides</h4>") {
		t.Errorf("the Guides list: %q", guides)
	}
	if page := get(h, "/docs/guides/start.md").Body.String(); !strings.Contains(page, `src="/raw/guides/img/flow.png"`) {
		t.Errorf("the image of a guide: %q", page)
	}
	if raw := get(h, "/raw/guides/img/flow.png"); raw.Code != http.StatusOK || raw.Header().Get("Content-Type") != "image/png" {
		t.Errorf("/raw/guides/img/flow.png: %d %s", raw.Code, raw.Header().Get("Content-Type"))
	}
	ops := get(h, "/docs/operations-manual/").Body.String()
	if !strings.Contains(ops, `href="/docs/operations-manual/runbook.md"><div title="The runbook"`) || !strings.Contains(ops, ">Operations Manual</h4>") || strings.Contains(ops, "start.md") {
		t.Errorf("the Operations Manual list: %q", ops)
	}
	nav := get(h, "/docs/operations-manual/runbook.md").Body.String()
	g, a, o := strings.Index(nav, `<a href="/docs/guides/">Guides</a>`), strings.Index(nav, `<a href="/specs/api">API</a>`), strings.Index(nav, `<a href="/docs/operations-manual/">Operations Manual</a>`)
	if g < 0 || a < g || o < a {
		t.Errorf("the navigation bar does not link the sections in order: %q", nav)
	}
	for _, path := range []string{"/docs/", "/docs/start.md", "/raw/img/flow.png", "/docs/api/", "/docs/guides/runbook.md", "/docs/operations-manual/start.md",
		"/raw/operations-manual/img/flow.png", "/docs/operations-manual/.draft.md", "/docs/other/", "/raw/other/img/flow.png"} {
		if rec := get(h, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestLinksAcrossDocsSections(t *testing.T) {
	page := "# Links\n\n- [runbook](ops/runbook.md)\n- [up](../ops/runbook.md)\n- [sibling](next.md)\n- [inner](deep/inner.md)\n"
	root := fstest.MapFS{
		"api.yaml":             {Data: []byte(pets)},
		"guides/links.md":      {Data: []byte(page)},
		"guides/next.md":       {Data: []byte("# Next\n")},
		"guides/deep/inner.md": {Data: []byte("# Inner\n\n[back](../links.md)\n")},
		"ops/runbook.md":       {Data: []byte("# Runbook\n")},
	}
	h := newSections(t, root,
		portal.Section{Title: "API", Type: portal.SpecSection, Input: "api.yaml"},
		portal.Section{Title: "Guides", Type: portal.DocsSection, Input: "guides"},
		portal.Section{Title: "Ops", Type: portal.DocsSection, Input: "ops"},
		portal.Section{Title: "Deep", Type: portal.DocsSection, Input: "guides/deep"},
	)
	hrefs := linkHrefs(get(h, "/docs/guides/links.md").Body.String())
	for text, want := range map[string]string{
		"runbook": "/docs/ops/runbook.md",
		"up":      "/docs/ops/runbook.md",
		"sibling": "/docs/guides/next.md",
		"inner":   "/docs/guides/deep/inner.md",
	} {
		if hrefs[text] != want {
			t.Errorf("%s: href %q, want %q", text, hrefs[text], want)
		}
	}
	if href := linkHrefs(get(h, "/docs/deep/inner.md").Body.String())["back"]; href != "/docs/guides/links.md" {
		t.Errorf("back: href %q, want /docs/guides/links.md", href)
	}
	if list := get(h, "/docs/deep/").Body.String(); !strings.Contains(list, `href="/docs/deep/inner.md"`) {
		t.Errorf("a docs section inside another does not list its own files: %q", list)
	}
}

func TestTocAcrossSections(t *testing.T) {
	root := fstest.MapFS{
		"api.yaml":    {Data: []byte(pets)},
		"guides/a.md": {Data: []byte("# A\n")},
		"guides/b.md": {Data: []byte("# B in Guides\n")},
		"ops/b.md":    {Data: []byte("# B\n")},
		"guides.json": {Data: []byte(`{"items": [{"type": "item", "title": "A", "uri": "guides/a.md"}, {"type": "item", "title": "B", "uri": "ops/b.md"}, {"type": "item", "title": "Pets", "uri": "api.yaml"}]}`)},
		"ops.json":    {Data: []byte(`{"items": [{"type": "item", "title": "Only B", "uri": "ops/b.md"}]}`)},
	}
	h := newSections(t, root,
		portal.Section{Title: "Guides", Type: portal.DocsSection, Input: "guides", Toc: "guides.json"},
		portal.Section{Title: "API", Type: portal.SpecSection, Input: "api.yaml"},
		portal.Section{Title: "Ops", Type: portal.DocsSection, Input: "ops", Toc: "ops.json"},
	)
	page := get(h, "/docs/guides/a.md").Body.String()
	for _, want := range []string{
		`href="/docs/guides/a.md"><div title="A" class="sl-flex sl-items-center sl-h-md sl-pr-4 sl-pl-4 sl-bg-primary-tint`,
		`href="/docs/ops/b.md"><div title="B"`,
		`href="/specs/api"><div title="Pets"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the Guides sidebar lacks %s", want)
		}
	}
	if page := get(h, "/docs/guides/b.md").Body.String(); !strings.Contains(page, `href="/docs/ops/b.md"><div title="B" class="sl-flex sl-items-center sl-h-md sl-pr-4 sl-pl-4 sl-bg-canvas-100`) {
		t.Errorf("Ops' b.md is marked current on the page of Guides' b.md: %q", page)
	}
	if page := get(h, "/docs/ops/b.md").Body.String(); !strings.Contains(page, `title="Only B"`) || strings.Contains(page, `title="Pets"`) {
		t.Errorf("the Ops sidebar is not its own toc file's: %q", page)
	}
}

func TestLibraryReadsEverySection(t *testing.T) {
	petsOps := "openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n"
	root := fstest.MapFS{
		"pets.yaml":   {Data: []byte(petsOps)},
		"store.yaml":  {Data: []byte(store)},
		"guides/a.md": {Data: []byte("# Start\n\nOrders come from GET /orders.\n")},
		"ops/b.md":    {Data: []byte("# Runbook\n")},
	}
	lib, err := portal.NewLibrary(portal.Config{Root: root, Sections: []portal.Section{
		{Title: "Pets", Type: portal.SpecSection, Input: "pets.yaml"},
		{Title: "Guides", Type: portal.DocsSection, Input: "guides"},
		{Title: "Store", Type: portal.SpecSection, Input: "store.yaml"},
		{Title: "Ops", Type: portal.DocsSection, Input: "ops"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := lib.Sections(), []portal.SectionLink{{Title: "Pets", URL: "/specs/pets"}, {Title: "Guides", URL: "/docs/guides/"}, {Title: "Store", URL: "/specs/store"}, {Title: "Ops", URL: "/docs/ops/"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("sections: %+v, want %+v", got, want)
	}
	if lib.Title() != "Pets" {
		t.Errorf("title %q, want the first spec section's", lib.Title())
	}
	docs, err := lib.Documents()
	if want := []portal.Document{{Path: "guides/a.md", Title: "Start", URL: "/docs/guides/a.md"}, {Path: "ops/b.md", Title: "Runbook", URL: "/docs/ops/b.md"}}; err != nil || !reflect.DeepEqual(docs, want) {
		t.Errorf("documents: %+v, %v, want %+v", docs, err, want)
	}
	if text, err := lib.ReadDocument("ops/b.md"); err != nil || !strings.Contains(text, "Runbook") {
		t.Errorf("ops/b.md: %q, %v", text, err)
	}
	if text, err := lib.ReadDocument("b.md"); err == nil {
		t.Errorf("b.md: %q, want an error", text)
	}
	ops, err := lib.Operations()
	want := []portal.Operation{
		{Spec: "pets", Method: "get", Path: "/pets", OperationID: "listPets", Pointer: "paths/~1pets/get", URL: "/specs/pets#/operations/listPets"},
		{Spec: "store", Method: "get", Path: "/orders", OperationID: "listOrders", Summary: "List the orders", Pointer: "paths/~1orders/get", URL: "/specs/store#/operations/listOrders"},
	}
	if err != nil || !reflect.DeepEqual(ops, want) {
		t.Errorf("operations: %+v, %v, want %+v", ops, err, want)
	}
	if part, err := lib.SpecPart("store", "paths/~1orders/get"); err != nil || !strings.Contains(part, "listOrders") {
		t.Errorf("store's part: %q, %v", part, err)
	}
	for _, spec := range []string{"pets", "guides", "", "Store"} {
		if part, err := lib.SpecPart(spec, "paths/~1orders/get"); err == nil {
			t.Errorf("%q: %q, want an error", spec, part)
		}
	}
	matches, err := lib.Search("orders", 10)
	if err != nil {
		t.Fatal(err)
	}
	var inGuide, inStore bool
	for _, m := range matches {
		inGuide = inGuide || m.Spec == "" && m.Where == "guides/a.md:3" && m.URL == "/docs/guides/a.md"
		inStore = inStore || m.Spec == "store" && strings.HasPrefix(m.Where, "paths/~1orders") && m.URL == "/specs/store"
		if m.Spec == "store" {
			if _, err := lib.SpecPart(m.Spec, m.Where); err != nil {
				t.Errorf("a match's place does not read back: %+v: %v", m, err)
			}
		}
	}
	if !inGuide || !inStore {
		t.Errorf("matches: %+v, want one in guides/a.md and one in the Store spec", matches)
	}
}

func TestNestedSectionsKeepTheirOwnPages(t *testing.T) {
	root := fstest.MapFS{
		"guides/a.md":            {Data: []byte("# A\n\n[inner](deep/inner.md)\n")},
		"guides/deep/inner.md":   {Data: []byte("# Inner\n\n[sibling](sibling.md)\n\n[up](../a.md)\n")},
		"guides/deep/sibling.md": {Data: []byte("# Sibling\n")},
		"deep.json":              {Data: []byte(`{"items": [{"type": "item", "title": "Inner", "uri": "guides/deep/inner.md"}, {"type": "item", "title": "A", "uri": "guides/a.md"}]}`)},
	}
	h := newSections(t, root,
		portal.Section{Title: "Guides", Type: portal.DocsSection, Input: "guides"},
		portal.Section{Title: "Deep", Type: portal.DocsSection, Input: "guides/deep", Toc: "deep.json"},
	)
	page := get(h, "/docs/deep/inner.md").Body.String()
	if hrefs := linkHrefs(page); hrefs["sibling"] != "/docs/deep/sibling.md" || hrefs["up"] != "/docs/guides/a.md" {
		t.Errorf("links on Deep's page: %q", hrefs)
	}
	for _, want := range []string{
		`href="/docs/deep/inner.md"><div title="Inner" class="sl-flex sl-items-center sl-h-md sl-pr-4 sl-pl-4 sl-bg-primary-tint`,
		`href="/docs/guides/a.md"><div title="A"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("Deep's sidebar lacks %s", want)
		}
	}
	if href := linkHrefs(get(h, "/docs/guides/a.md").Body.String())["inner"]; href != "/docs/guides/deep/inner.md" {
		t.Errorf("inner on Guides' page: href %q, want Guides' own page", href)
	}
}
