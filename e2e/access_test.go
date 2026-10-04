//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/internal/fakeaccess"
	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

func TestReaderSeesOnlyTheVisibleSections(t *testing.T) {
	t.Skip("HOLE(2): answer each reader from its visible sections")
	cfg := portal.Config{Root: os.DirFS("../testdata"), Portals: []portal.Portal{
		{Name: "Pets", Sections: sections("specs/petstore-3.1.yaml", "docs", "")},
		{Name: "Store", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "specs/petstore-3.0.yaml"}}},
	}}
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m := fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "questions from customers about this API.", Call: &fakemodel.Call{Name: "list_operations", Args: map[string]any{}}},
		{Match: `{"operations":null}`, Reply: "The guides describe no operations."},
	})
	c, err := chat.New(chat.Config{Model: m, Libraries: libs})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chat, cfg.Access = c.Routes(), fakeaccess.Hook
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Every request of the browser, the chat's socket included, comes from
	// the reader who may see the documents of Pets alone.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set(fakeaccess.Header, "Pets/Documents")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	browser := newBrowser(t)
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	var location, nav, notFound, heading, chatNav string
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`.portal-nav`, chromedp.ByQuery),
		chromedp.Location(&location),
		chromedp.Text(`.portal-nav`, &nav, chromedp.ByQuery),
		chromedp.Navigate(srv.URL+"/portals/pets/specs/api"),
		chromedp.Text(`h1`, &notFound, chromedp.ByQuery),
		chromedp.Navigate(srv.URL+"/portals/pets/chat"),
		chromedp.WaitVisible(`.phx-connected`, chromedp.ByQuery),
		chromedp.Text(`h1`, &heading, chromedp.ByQuery),
		chromedp.Text(`.portal-nav`, &chatNav, chromedp.ByQuery),
		chromedp.SendKeys(`textarea[name=question]`, "Which operations are there?", chromedp.ByQuery),
		chromedp.Click(`button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible(`//p[text()="The guides describe no operations."]`, chromedp.BySearch),
	)
	if err != nil {
		t.Fatalf("the reader's pages: %v", err)
	}
	if !strings.HasSuffix(location, "/portals/pets/docs/documents/") {
		t.Errorf("/ opened %s, want the reader's only section", location)
	}
	for name, text := range map[string]string{"the document list": nav, "the chat page": chatNav} {
		if !strings.Contains(text, "Documents") || strings.Contains(text, "API") || strings.Contains(text, "Store") {
			t.Errorf("the navigation bar of %s: %q, want the documents alone", name, text)
		}
	}
	if notFound != "Spec not found" {
		t.Errorf("the hidden spec section's page says %q, want Spec not found", notFound)
	}
	if heading != "Ask about the API" {
		t.Errorf("the chat page's heading %q names a hidden API", heading)
	}
}
