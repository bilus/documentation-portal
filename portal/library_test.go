package portal_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

func newLibrary(t *testing.T) *portal.Library {
	t.Helper()
	spec := "openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n" +
		"  /pets:\n" +
		"    get:\n      operationId: listPets\n      summary: List the pets\n" +
		"    post:\n      summary: Add a pet\n" +
		"    delete:\n      operationId: deleteAllPets\n      summary: Delete every pet\n      x-doNotPublish:\n        - main\n" +
		"components:\n  schemas:\n    Pet:\n      properties:\n        name:\n          type: string\n" +
		"        secretField:\n          type: string\n          x-doNotPublish:\n            - main\n"
	root := fstest.MapFS{
		"api.yaml":        {Data: []byte(spec)},
		"private.md":      {Data: []byte("# hunter2\n")},
		"docs/a.md":       {Data: []byte("# Getting started\n\nList the pets with GET /pets.\n")},
		"docs/.draft.md":  {Data: []byte("# Draft\n")},
		"docs/leak.md":    {Data: []byte("../private.md"), Mode: fs.ModeSymlink},
		"docs/guide/b.md": {Data: []byte("No heading here.\n")},
	}
	lib, err := portal.NewLibrary(portal.Config{Root: root, Sections: sections("api.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func TestLibraryDocuments(t *testing.T) {
	lib := newLibrary(t)
	docs, err := lib.Documents()
	if err != nil {
		t.Fatal(err)
	}
	want := []portal.Document{{Path: "a.md", Title: "Getting started", URL: "/docs/a.md"}, {Path: "guide/b.md", Title: "guide/b.md", URL: "/docs/guide/b.md"}}
	if len(docs) != len(want) || docs[0] != want[0] || docs[1] != want[1] {
		t.Errorf("documents: %+v, want %+v", docs, want)
	}
	if text, err := lib.ReadDocument("a.md"); err != nil || !strings.Contains(text, "GET /pets") {
		t.Errorf("a.md: %q, %v", text, err)
	}
	for _, path := range []string{".draft.md", "leak.md", "../private.md", "missing.md"} {
		if text, err := lib.ReadDocument(path); err == nil {
			t.Errorf("%s: %q, want an error", path, text)
		}
	}
}

func TestLibraryOperations(t *testing.T) {
	ops, err := newLibrary(t).Operations()
	if err != nil {
		t.Fatal(err)
	}
	want := []portal.Operation{
		{Method: "get", Path: "/pets", OperationID: "listPets", Summary: "List the pets", Pointer: "paths/~1pets/get", URL: "/specs/api.yaml#/operations/listPets"},
		{Method: "post", Path: "/pets", Summary: "Add a pet", Pointer: "paths/~1pets/post", URL: "/specs/api.yaml#/paths/pets/post"},
	}
	if len(ops) != len(want) || ops[0] != want[0] || ops[1] != want[1] {
		t.Errorf("operations: %+v, want %+v", ops, want)
	}
}

func TestLibrarySpecPart(t *testing.T) {
	lib := newLibrary(t)
	if part, err := lib.SpecPart("api", "paths/~1pets/get"); err != nil || !strings.Contains(part, "listPets") {
		t.Errorf("paths/~1pets/get: %q, %v", part, err)
	}
	if part, err := lib.SpecPart("api", "components/schemas/Pet"); err != nil || !strings.Contains(part, "name") || strings.Contains(part, "secretField") {
		t.Errorf("components/schemas/Pet: %q, %v", part, err)
	}
	for _, pointer := range []string{"paths/~1pets/delete", "components/schemas/Cat", ""} {
		if part, err := lib.SpecPart("api", pointer); err == nil {
			t.Errorf("%q: %q, want an error", pointer, part)
		}
	}
}

func TestLibraryReadsGuidesAsTheirPagesShowThem(t *testing.T) {
	root := fstest.MapFS{
		"api.yaml": {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths: {}\n")},
		"docs/setup.md": {Data: []byte("# Setup\n\n<!-- internal: POST /admin/reset wipes accounts -->\n\n" +
			"Read [the reference][ref] first.\n\n[ref]: ../api.yaml\n[draft]: https://internal.example.com \"an internal title\"\n[//]: # (a hidden note)\n\n" +
			"<div style=\"display:none\">a hidden block</div>\n")},
		"docs/install.md": {Data: []byte("## Before you start\n\n```sh\n# install the CLI\n```\n\nInstall\n=======\n\nRun the installer.\n")},
		"docs/closed.md":  {Data: []byte("# Closed title #\n")},
	}
	lib, err := portal.NewLibrary(portal.Config{Root: root, Sections: sections("api.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	text, err := lib.ReadDocument("setup.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"admin/reset", "internal.example.com", "an internal title", "a hidden note", "a hidden block"} {
		if strings.Contains(text, hidden) {
			t.Errorf("the page leaves out %q, the library does not: %q", hidden, text)
		}
		if matches, err := lib.Search(hidden, 10); err != nil || len(matches) > 0 {
			t.Errorf("search %q: %+v, %v", hidden, matches, err)
		}
	}
	if want := "# Setup\n\nRead [the reference](/specs/api.yaml) first."; text != want {
		t.Errorf("text %q, want %q", text, want)
	}
	docs, err := lib.Documents()
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, d := range docs {
		titles[d.Path] = d.Title
	}
	if titles["install.md"] != "Install" || titles["closed.md"] != "Closed title" || titles["setup.md"] != "Setup" {
		t.Errorf("titles %v", titles)
	}
}

func TestLibraryReadsEveryPointerItGives(t *testing.T) {
	spec := "openapi: 3.1.0\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n" +
		"  /pets:\n    $ref: '#/components/pathItems/Pets'\n" +
		"  /owners:\n    get:\n      summary: List the owners\n      parameters:\n        - name: ownerId\n          in: query\n" +
		"  /v~1/items:\n    get:\n      summary: List the items\n" +
		"  /loop:\n    $ref: '#/components/pathItems/Loop'\n" +
		"components:\n  pathItems:\n    Pets:\n      get:\n        summary: List the pets\n" +
		"    Loop:\n      $ref: '#/components/pathItems/Loop'\n" +
		"  schemas:\n    Pet: &pet\n      properties:\n        name:\n          type: string\n    Dog: *pet\n" +
		"    Cat:\n      allOf:\n        - $ref: '#/components/schemas/Pet'\n        - description: A cat\n"
	lib, err := portal.NewLibrary(portal.Config{Root: fstest.MapFS{"api.yaml": {Data: []byte(spec)}}, Sections: sections("api.yaml", "", "")})
	if err != nil {
		t.Fatal(err)
	}
	ops, err := lib.Operations()
	if err != nil || len(ops) != 3 {
		t.Fatalf("operations: %+v, %v", ops, err)
	}
	for _, op := range ops {
		if part, err := lib.SpecPart(op.Spec, op.Pointer); err != nil || !strings.Contains(part, op.Summary) {
			t.Errorf("%s: %q, %v", op.Pointer, part, err)
		}
	}
	matches, err := lib.Search("name", 25)
	if err != nil {
		t.Fatal(err)
	}
	var where []string
	for _, m := range matches {
		where = append(where, m.Where)
		if _, err := lib.SpecPart(m.Spec, m.Where); err != nil {
			t.Errorf("search gave %s, which reads as %v", m.Where, err)
		}
	}
	for _, want := range []string{"paths/~1owners/get/parameters/0/name", "components/schemas/Dog/properties/name"} {
		if !slices.Contains(where, want) {
			t.Errorf("no match at %s among %v", want, where)
		}
	}
	if dog, err := lib.SpecPart("api", "components/schemas/Dog"); err != nil || strings.ContainsAny(dog, "&*") || !strings.Contains(dog, "name:") {
		t.Errorf("components/schemas/Dog: %q, %v", dog, err)
	}
	if part, err := lib.SpecPart("api", "components/schemas/Cat/allOf/1/description"); err != nil || !strings.Contains(part, "A cat") {
		t.Errorf("an allOf entry: %q, %v", part, err)
	}
	if part, err := lib.SpecPart("api", "paths/~1loop/get"); err == nil {
		t.Errorf("a $ref cycle: %q", part)
	}
}

func TestLibrarySearchFindsTheSpecPastManyGuideMatches(t *testing.T) {
	root := fstest.MapFS{
		"api.yaml":     {Data: []byte("openapi: 3.0.3\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      summary: List the pets\n")},
		"docs/many.md": {Data: []byte(strings.Repeat("Pets are here.\n\n", 30))},
	}
	lib, err := portal.NewLibrary(portal.Config{Root: root, Sections: sections("api.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := lib.Search("pets", 10)
	if err != nil {
		t.Fatal(err)
	}
	inSpec := 0
	for _, m := range matches {
		if !strings.HasPrefix(m.Where, "many.md:") {
			inSpec++
		}
	}
	if len(matches) != 10 || inSpec == 0 {
		t.Errorf("%d matches, %d of them in the spec", len(matches), inSpec)
	}
}

func TestLibrarySearch(t *testing.T) {
	lib := newLibrary(t)
	matches, err := lib.Search("PETS", 10)
	if err != nil {
		t.Fatal(err)
	}
	var where []string
	for _, m := range matches {
		where = append(where, m.Where)
	}
	for _, want := range []string{"a.md:3", "paths/~1pets", "paths/~1pets/get/summary"} {
		if !strings.Contains(strings.Join(where, " "), want) {
			t.Errorf("no match at %s among %v", want, where)
		}
	}
	for _, query := range []string{"secretField", "deleteAllPets", "hunter2", "Draft"} {
		if matches, err := lib.Search(query, 10); err != nil || len(matches) > 0 {
			t.Errorf("%s: %+v, %v", query, matches, err)
		}
	}
	if matches, err := lib.Search("pets", 2); err != nil || len(matches) != 2 {
		t.Errorf("limit 2: %d matches, %v", len(matches), err)
	}
	if lib.Title() != "Pets" {
		t.Errorf("title %q", lib.Title())
	}
}
