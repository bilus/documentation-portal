//go:build e2e

package e2e

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"gocloud.dev/blob/fileblob"

	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/source"
)

func TestReaderOpensAPreviewAndLeavesIt(t *testing.T) {
	dir := t.TempDir()
	writeFolder(t, dir, "# Guide\n\nThe published version of the guide.\n")
	writeFolder(t, filepath.Join(dir, "previews", "pr-1"), "# Guide\n\nThe preview version of the guide.\n")
	bucket, err := fileblob.OpenBucket(dir, &fileblob.Options{Metadata: fileblob.MetadataDontWrite})
	if err != nil {
		t.Fatal(err)
	}
	defer bucket.Close()
	src, err := source.NewBucket(bucket, 0).Without("previews")
	if err != nil {
		t.Fatal(err)
	}
	location, err := source.NewBucket(bucket, 0).Folder("previews")
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
	snap, err := source.Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	first, err := build(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	reloader := source.NewReloader(t.Context(), src, snap.Listing, first, 0, build)
	folder := func(name string) (source.Source, error) { return location.Folder(name) }
	previews := source.NewPreviews(t.Context(), folder, build, 0, time.Hour, 10)
	srv := httptest.NewServer(portal.WithPreviews(reloader, portal.PreviewsConfig{Open: previews.Handler}))
	defer srv.Close()

	browser := newBrowser(t)
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	var opened, banner, preview, left, published string
	var noBanner bool
	err = chromedp.Run(ctx,
		// A CI job's link to a changed page of the preview.
		chromedp.Navigate(srv.URL+"/previews/pr-1/portals/pets/docs/guides/guide.md"),
		chromedp.WaitVisible(`.portal-banner`, chromedp.ByQuery),
		chromedp.Location(&opened),
		chromedp.Text(`.portal-banner`, &banner, chromedp.ByQuery),
		chromedp.Text(`.sl-markdown-viewer`, &preview, chromedp.ByQuery),
		// The way out leads to /, which opens the published portal's first
		// section, the API's viewer page.
		chromedp.Click(`.portal-banner a`, chromedp.ByQuery),
		chromedp.WaitVisible(`elements-api`, chromedp.ByQuery),
		chromedp.Location(&left),
		chromedp.Navigate(srv.URL+"/portals/pets/docs/guides/guide.md"),
		chromedp.WaitVisible(`.sl-markdown-viewer`, chromedp.ByQuery),
		chromedp.Text(`.sl-markdown-viewer`, &published, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('.portal-banner') === null`, &noBanner),
	)
	if err != nil {
		t.Fatalf("the preview in the browser: %v", err)
	}
	if !strings.HasSuffix(opened, "/portals/pets/docs/guides/guide.md") {
		t.Errorf("the link opened %s, want the guide's page", opened)
	}
	if !strings.Contains(banner, "pr-1") || !strings.Contains(banner, "Leave the preview") {
		t.Errorf("the banner says %q, want the folder's name and a link out of the preview", banner)
	}
	if !strings.Contains(preview, "The preview version") {
		t.Errorf("the preview's guide says %q", preview)
	}
	if !strings.HasSuffix(left, "/portals/pets/specs/api") {
		t.Errorf("the way out of the preview opened %s, want the published portal's first section", left)
	}
	if !strings.Contains(published, "The published version") || !noBanner {
		t.Errorf("after leaving the preview, the guide says %q, with a banner %v", published, !noBanner)
	}
}
