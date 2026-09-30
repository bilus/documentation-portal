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
	h, err := portal.New(portal.Config{Root: root, SpecPath: "apis/pets.yaml", DocsPath: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	body := get(h, "/docs/guide/links.md").Body.String()
	hrefs := linkHrefs(body)
	for text, want := range map[string]string{
		"root":     "/docs/guide-oauth.md",
		"slash":    "/docs/guide-oauth.md",
		"fragment": "/docs/guide-oauth.md#tokens",
		"sibling":  "/docs/guide/intro.md",
		"up":       "/docs/guide-oauth.md",
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
	t.Skip("HOLE(2): point a link to the configured spec or one of its operations at the viewer page")
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
	h, err := portal.New(portal.Config{Root: root, SpecPath: "apis/pets.yaml", DocsPath: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	body := get(h, "/docs/links.md").Body.String()
	hrefs := linkHrefs(body)
	for text, want := range map[string]string{
		"spec":              "/specs/apis/pets.yaml",
		"slash":             "/specs/apis/pets.yaml",
		"operation":         "/specs/apis/pets.yaml#/operations/showPetById",
		"no operationId":    "/specs/apis/pets.yaml#/paths/pets/post",
		"unpublished":       "/specs/apis/pets.yaml",
		"missing operation": "/specs/apis/pets.yaml",
		"schema":            "/specs/apis/pets.yaml",
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
