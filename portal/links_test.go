package portal_test

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

var anchor = regexp.MustCompile(`<a href="([^"]*)"[^>]*>([^<]*)</a>`)

// linkHrefs returns the href of each link in body, by the link's text.
func linkHrefs(body string) map[string]string {
	hrefs := map[string]string{}
	for _, m := range anchor.FindAllStringSubmatch(body, -1) {
		hrefs[m[2]] = m[1]
	}
	return hrefs
}

func TestDocLinks(t *testing.T) {
	page := "# Links\n\n" +
		"- [root](docs/guide-oauth.md)\n" +
		"- [slash](/docs/guide-oauth.md)\n" +
		"- [fragment](docs/guide-oauth.md#tokens)\n" +
		"- [sibling](intro.md)\n" +
		"- [up](../guide-oauth.md)\n" +
		"- [anchor](#setup)\n" +
		"- [absolute](https://example.com/a?b=c)\n" +
		"- [mail](mailto:team@example.com)\n" +
		"- [outside](../../../secret.md)\n" +
		"- [slash only](/intro.md)\n" +
		"- [unserved](README.md)\n" +
		"- [hidden](docs/.drafts/draft.md)\n" +
		"- [missing](docs/missing.md)\n"
	root := fstest.MapFS{
		"apis/pets.yaml":                 {Data: []byte(pets)},
		"README.md":                      {Data: []byte("# Outside the content directory\n")},
		"docs/guide-oauth.md":            {Data: []byte("# OAuth\n")},
		"docs/guide/links.md":            {Data: []byte(page)},
		"docs/guide/intro.md":            {Data: []byte("# Intro\n")},
		"docs/guide/docs/guide-oauth.md": {Data: []byte("# Not the root's\n")},
		"docs/.drafts/draft.md":          {Data: []byte("# Draft\n")},
	}
	h, err := portal.New(portal.Config{Root: root, Sections: sections("apis/pets.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	body := get(h, "/docs/documents/guide/links.md").Body.String()
	hrefs := linkHrefs(body)
	for text, want := range map[string]string{
		"root":     "/docs/documents/guide-oauth.md",
		"slash":    "/docs/documents/guide-oauth.md",
		"fragment": "/docs/documents/guide-oauth.md#tokens",
		"sibling":  "/docs/documents/guide/intro.md",
		"up":       "/docs/documents/guide-oauth.md",
		"anchor":   "#setup",
		"absolute": "https://example.com/a?b=c",
		"mail":     "mailto:team@example.com",
	} {
		if hrefs[text] != want {
			t.Errorf("%s: href %q, want %q", text, hrefs[text], want)
		}
	}
	for _, text := range []string{"outside", "slash only", "unserved", "hidden", "missing"} {
		if href, ok := hrefs[text]; ok {
			t.Errorf("%s leads to %q, want no link", text, href)
		}
		if !strings.Contains(body, text) {
			t.Errorf("%s: the text is gone", text)
		}
	}
}

func TestSpecLinks(t *testing.T) {
	spec := "openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n" +
		"  /pets:\n" +
		"    post:\n      summary: Add a pet\n" +
		"    delete:\n      operationId: deleteAllPets\n      x-doNotPublish:\n        - main\n" +
		"  /pets/{petId}:\n" +
		"    get:\n      operationId: showPetById\n"
	page := "# Spec links\n\n" +
		"- [spec](apis/pets.yaml)\n" +
		"- [slash](/apis/pets.yaml)\n" +
		"- [operation](apis/pets.yaml/paths/~1pets~1{petId}/get)\n" +
		"- [no operationId](apis/pets.yaml/paths/~1pets/post)\n" +
		"- [unpublished](apis/pets.yaml/paths/~1pets/delete)\n" +
		"- [missing operation](apis/pets.yaml/paths/~1cats/get)\n" +
		"- [schema](apis/pets.yaml/components/schemas/Pet)\n" +
		"- [other spec](apis/other.yaml)\n"
	root := fstest.MapFS{
		"apis/pets.yaml":  {Data: []byte(spec)},
		"apis/other.yaml": {Data: []byte(pets)},
		"docs/links.md":   {Data: []byte(page)},
	}
	h, err := portal.New(portal.Config{Root: root, Sections: sections("apis/pets.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	body := get(h, "/docs/documents/links.md").Body.String()
	hrefs := linkHrefs(body)
	for text, want := range map[string]string{
		"spec":              "/specs/api",
		"slash":             "/specs/api",
		"operation":         "/specs/api#/operations/showPetById",
		"no operationId":    "/specs/api#/paths/pets/post",
		"unpublished":       "/specs/api",
		"missing operation": "/specs/api",
		"schema":            "/specs/api",
	} {
		if hrefs[text] != want {
			t.Errorf("%s: href %q, want %q", text, hrefs[text], want)
		}
	}
	if href, ok := hrefs["other spec"]; ok {
		t.Errorf("other spec leads to %q, want no link", href)
	}
	if strings.Contains(body, "deleteAllPets") {
		t.Error("the page names the unpublished operation")
	}
}

func TestDocLinkWalk(t *testing.T) {
	page := "# Walk\n\n" +
		"First [missing](docs/missing.md) then [second](docs/guide-oauth.md) then ![diagram](diagram.png).\n\n" +
		"[![inner](diagram.png)](docs/guide-oauth.md)\n"
	root := fstest.MapFS{
		"apis/pets.yaml":      {Data: []byte(pets)},
		"docs/guide-oauth.md": {Data: []byte("# OAuth\n")},
		"docs/walk.md":        {Data: []byte(page)},
		"docs/diagram.png":    {Data: []byte("png")},
	}
	h, err := portal.New(portal.Config{Root: root, Sections: sections("apis/pets.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	body := get(h, "/docs/documents/walk.md").Body.String()
	prose := body[strings.Index(body, "sl-markdown-viewer"):]
	if href := linkHrefs(prose)["second"]; href != "/docs/documents/guide-oauth.md" {
		t.Errorf("the link after one that leads nowhere: href %q", href)
	}
	if n := strings.Count(prose, `src="/raw/documents/diagram.png"`); n != 2 {
		t.Errorf("%d images under /raw/, want the one after the links and the one inside a link:\n%s", n, prose)
	}
	if n := strings.Count(prose, `href="/docs/documents/guide-oauth.md"`); n != 2 {
		t.Errorf("%d links to the document, want 2:\n%s", n, prose)
	}
}

func TestDocLinkTargets(t *testing.T) {
	page := "# Targets\n\n" +
		"- [directory](docs/guide/)\n" +
		"- [image](docs/guide/diagram.png)\n" +
		"- [dot](./docs/guide-oauth.md)\n" +
		"- [host only](//cdn.example.com/x.md)\n" +
		"- [root dots](/../docs/guide-oauth.md)\n"
	root := fstest.MapFS{
		"apis/pets.yaml":                 {Data: []byte(pets)},
		"docs/guide-oauth.md":            {Data: []byte("# OAuth\n")},
		"docs/guide/p.md":                {Data: []byte(page)},
		"docs/guide/diagram.png":         {Data: []byte("png")},
		"docs/guide/docs/guide-oauth.md": {Data: []byte("# Not the root's\n")},
	}
	h, err := portal.New(portal.Config{Root: root, Sections: sections("apis/pets.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	hrefs := linkHrefs(get(h, "/docs/documents/guide/p.md").Body.String())
	for text, want := range map[string]string{"dot": "/docs/documents/guide-oauth.md", "host only": "//cdn.example.com/x.md", "root dots": "/docs/documents/guide-oauth.md"} {
		if hrefs[text] != want {
			t.Errorf("%s: href %q, want %q", text, hrefs[text], want)
		}
	}
	for _, text := range []string{"directory", "image"} {
		if href, ok := hrefs[text]; ok {
			t.Errorf("%s leads to %q, want no link", text, href)
		}
	}
}

func TestDocLinkEscapes(t *testing.T) {
	page := "# Escapes\n\n" +
		"- [escape](docs/guide\\_oauth.md)\n" +
		"- [entity](docs/guide&#95;oauth.md)\n" +
		"- [parens](docs/a\\(1\\).md)\n" +
		"- [ampersand](docs/r&amp;d.md)\n" +
		"- [accent](docs/guide_oauth.md#café)\n" +
		"- [up to the spec](../apis/pets.yaml)\n" +
		"- [case](docs/Guide_OAuth.md)\n" +
		"- [see *the* guide](docs/missing.md)\n"
	root := fstest.MapFS{
		"apis/pets.yaml":      {Data: []byte(pets)},
		"docs/guide_oauth.md": {Data: []byte("# OAuth\n")},
		"docs/a(1).md":        {Data: []byte("# One\n")},
		"docs/r&d.md":         {Data: []byte("# R and D\n")},
		"docs/links.md":       {Data: []byte(page)},
	}
	h, err := portal.New(portal.Config{Root: root, Sections: sections("apis/pets.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	body := get(h, "/docs/documents/links.md").Body.String()
	hrefs := linkHrefs(body)
	for text, want := range map[string]string{
		"escape":         "/docs/documents/guide_oauth.md",
		"entity":         "/docs/documents/guide_oauth.md",
		"parens":         "/docs/documents/a%281%29.md",
		"ampersand":      "/docs/documents/r&amp;d.md",
		"accent":         "/docs/documents/guide_oauth.md#caf%C3%A9",
		"up to the spec": "/specs/api",
	} {
		if hrefs[text] != want {
			t.Errorf("%s: href %q, want %q", text, hrefs[text], want)
		}
	}
	if href, ok := hrefs["case"]; ok {
		t.Errorf("case leads to %q, want no link", href)
	}
	if !strings.Contains(body, "see <em>the</em> guide") {
		t.Error("a link that leads nowhere lost the order of its text")
	}
}

func TestSpecLinkToMissingSpec(t *testing.T) {
	root := fstest.MapFS{"docs/links.md": {Data: []byte("- [spec](api.yaml)\n- [operation](api.yaml/paths/~1pets/get)\n")}}
	h, err := portal.New(portal.Config{Root: root, Sections: sections("api.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	hrefs := linkHrefs(get(h, "/docs/documents/links.md").Body.String())
	for _, text := range []string{"spec", "operation"} {
		if href, ok := hrefs[text]; ok {
			t.Errorf("%s leads to %q, want no link", text, href)
		}
	}
}

func TestSpecLinkToSpecDirectory(t *testing.T) {
	root := fstest.MapFS{
		"api.yaml/readme.txt": {Data: []byte("not a spec\n")},
		"docs/links.md":       {Data: []byte("- [spec](api.yaml)\n")},
	}
	h, err := portal.New(portal.Config{Root: root, Sections: sections("api.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	if href, ok := linkHrefs(get(h, "/docs/documents/links.md").Body.String())["spec"]; ok {
		t.Errorf("spec leads to %q, want no link", href)
	}
}
