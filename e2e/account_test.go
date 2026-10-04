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
	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

func TestReaderSeesTheAccountLinks(t *testing.T) {
	cfg := portal.Config{Root: os.DirFS("../testdata"), Portals: []portal.Portal{
		{Name: "Pets", Sections: sections("specs/petstore-3.1.yaml", "docs", "")},
		{Name: "Store", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "specs/petstore-3.0.yaml"}}},
	}}
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c, err := chat.New(chat.Config{Model: fakemodel.New("opus", nil), Libraries: libs})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chat = c.Routes()
	cfg.Account = func(*http.Request) []portal.AccountLink {
		return []portal.AccountLink{{Label: "Ada"}, {Label: "Sign out", URL: "/auth/sign-out"}}
	}
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	browser := newBrowser(t)
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	// atTheEnd reports whether the account links end the navigation bar,
	// at its right edge, right after the portal menu, which stays on the
	// right as without links.
	const atTheEnd = `(() => {
		const nav = document.querySelector('.portal-nav').getBoundingClientRect();
		const links = document.querySelector('.portal-account').getBoundingClientRect();
		const menu = document.querySelector('.portal-menu').getBoundingClientRect();
		return nav.right - links.right < 40 && links.left >= menu.right && links.left - menu.right < 40;
	})()`
	var docLinks, chatLinks, signOut string
	var docEnd, chatEnd, ok bool
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/portals/pets/docs/documents/"),
		chromedp.WaitVisible(`.portal-account`, chromedp.ByQuery),
		chromedp.Text(`.portal-account`, &docLinks, chromedp.ByQuery),
		chromedp.Evaluate(atTheEnd, &docEnd),
		chromedp.Navigate(srv.URL+"/portals/pets/chat"),
		chromedp.WaitVisible(`.phx-connected`, chromedp.ByQuery),
		chromedp.Text(`.portal-account`, &chatLinks, chromedp.ByQuery),
		chromedp.Evaluate(atTheEnd, &chatEnd),
		chromedp.AttributeValue(`.portal-account a`, "href", &signOut, &ok, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("the account links: %v", err)
	}
	for name, text := range map[string]string{"the document list": docLinks, "the joined chat page": chatLinks} {
		if strings.Join(strings.Fields(text), " ") != "Ada Sign out" {
			t.Errorf("%s shows the account links %q, want Ada and Sign out", name, text)
		}
	}
	if !docEnd || !chatEnd {
		t.Errorf("the account links are not at the right end of the navigation bar: the document list %v, the chat page %v", docEnd, chatEnd)
	}
	if signOut != "/auth/sign-out" {
		t.Errorf("the chat page's sign-out link opens %q", signOut)
	}
}
