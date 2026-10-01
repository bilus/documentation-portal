//go:build e2e

package e2e

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/bilus/documentation-portal/portal"
)

func TestOperationLinkOpensOperation(t *testing.T) {
	h, err := portal.New(portal.Config{Root: os.DirFS("../testdata"), Sections: sections("specs/petstore-3.1.yaml", "docs", "")})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	browser := newBrowser(t)
	pageText(t, browser, srv.URL+"/docs/documents/README.md", "Sample documents")
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	err = chromedp.Run(ctx,
		chromedp.Click(`//a[text()="the operation that shows a pet"]`, chromedp.BySearch),
		chromedp.WaitVisible(`//*[text()="microchipId"]`, chromedp.BySearch),
	)
	if err != nil {
		t.Fatalf("the link does not open the operation: %v", err)
	}
}
