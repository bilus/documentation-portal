//go:build e2e

package e2e

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/oauth2-proxy/mockoidc"

	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/signin"
)

func TestReaderSignsInAndReturnsToTheFirstPage(t *testing.T) {
	t.Skip("HOLE(2): sign the reader in through the mock OpenID Connect provider and return to the first page")
	o, err := mockoidc.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer o.Shutdown()
	o.QueueUser(&mockoidc.MockUser{Subject: "ada", PreferredUsername: "Ada", Email: "ada@example.com", EmailVerified: true})

	cfg := portal.Config{Root: os.DirFS("../testdata"), Portals: petsPortal(sections("specs/petstore-3.1.yaml", "docs", ""))}
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c, err := chat.New(chat.Config{Model: fakemodel.New("opus", nil), Libraries: libs, Reader: signin.ReaderID})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chat, cfg.Account = c.Routes(), signin.AccountLinks
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	defer srv.Close()
	g, err := signin.New(context.Background(), signin.Config{
		Issuer:       o.Issuer(),
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		CallbackURL:  "http://" + srv.Listener.Addr().String() + "/auth/callback",
		Key:          []byte("the session key of the browser test, 32 bytes"),
	}, h)
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = g
	srv.Start()

	browser := newBrowser(t)
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	var location, links, signedOut string
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/portals/pets/docs/documents/"),
		chromedp.WaitVisible(`.portal-account`, chromedp.ByQuery),
		chromedp.Location(&location),
		chromedp.Text(`.portal-account`, &links, chromedp.ByQuery),
		chromedp.Navigate(srv.URL+"/portals/pets/chat"),
		chromedp.WaitVisible(`.phx-connected`, chromedp.ByQuery),
		chromedp.Click(`.portal-account a`, chromedp.ByQuery),
		chromedp.WaitVisible(`//h1[text()="You have signed out"]`, chromedp.BySearch),
		chromedp.Text(`body`, &signedOut, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("the reader's sign-in: %v", err)
	}
	if location != srv.URL+"/portals/pets/docs/documents/" {
		t.Errorf("the sign-in ended at %s, want the first page", location)
	}
	if got := strings.Join(strings.Fields(links), " "); got != "Ada Sign out" {
		t.Errorf("the account links show %q, want Ada and Sign out", got)
	}
	if !strings.Contains(signedOut, "Sign in again") {
		t.Errorf("the signed-out page says %q", signedOut)
	}
}
