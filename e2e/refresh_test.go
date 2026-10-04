//go:build e2e

package e2e

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocloud.dev/blob/fileblob"

	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/source"
)

// writeFolder writes the configuration file, a guide and the sample spec to
// dir, as a bucket folder.
func writeFolder(t *testing.T, dir, guide string) {
	t.Helper()
	spec, err := os.ReadFile("../testdata/specs/petstore-3.1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"environment.yaml": "portals:\n  - name: Pets\n    sections:\n      - title: API\n        type: spec\n        input: specs/pets.yaml\n      - title: Guides\n        type: docs\n        input: docs\n",
		"docs/guide.md":    guide,
		"specs/pets.yaml":  string(spec),
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRefreshShowsTheNewDocument(t *testing.T) {
	dir := t.TempDir()
	writeFolder(t, dir, "# Guide\n\nThe first version of the guide.\n")
	bucket, err := fileblob.OpenBucket(dir, &fileblob.Options{Metadata: fileblob.MetadataDontWrite})
	if err != nil {
		t.Fatal(err)
	}
	defer bucket.Close()
	src := source.NewBucket(bucket, 0)
	snap, err := source.Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	build := func(root fs.FS) (http.Handler, error) {
		cfg, err := portal.ReadConfig(root, "environment.yaml")
		if err != nil {
			return nil, err
		}
		return portal.New(cfg)
	}
	first, err := build(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(source.NewReloader(t.Context(), src, snap.Listing, first, 20*time.Millisecond, build))
	defer srv.Close()

	browser := newBrowser(t)
	if text := pageText(t, browser, srv.URL+"/portals/pets/docs/guides/guide.md", "Guide"); !strings.Contains(text, "The first version") {
		t.Fatalf("the first version is not shown: %q", text)
	}
	writeFolder(t, dir, "# Guide\n\nThe second version of the guide.\n")
	for start := time.Now(); ; time.Sleep(50 * time.Millisecond) {
		if text := pageText(t, browser, srv.URL+"/portals/pets/docs/guides/guide.md", "Guide"); strings.Contains(text, "The second version") {
			break
		}
		if time.Since(start) > 10*time.Second {
			t.Fatal("the second version did not show within ten seconds")
		}
	}
}
