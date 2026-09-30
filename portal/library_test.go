package portal_test

import (
	"io/fs"
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
	lib, err := portal.NewLibrary(portal.Config{Root: root, SpecPath: "api.yaml", DocsPath: "docs"})
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
	if part, err := lib.SpecPart("paths/~1pets/get"); err != nil || !strings.Contains(part, "listPets") {
		t.Errorf("paths/~1pets/get: %q, %v", part, err)
	}
	if part, err := lib.SpecPart("components/schemas/Pet"); err != nil || !strings.Contains(part, "name") || strings.Contains(part, "secretField") {
		t.Errorf("components/schemas/Pet: %q, %v", part, err)
	}
	for _, pointer := range []string{"paths/~1pets/delete", "components/schemas/Cat", ""} {
		if part, err := lib.SpecPart(pointer); err == nil {
			t.Errorf("%q: %q, want an error", pointer, part)
		}
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
