//go:build e2e

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// newBrowser starts a headless Chrome for the test, like the library's
// browser tests: the one named by CHROME_BIN, else one found by chromedp.
func newBrowser(t *testing.T) context.Context {
	t.Helper()
	opts := chromedp.DefaultExecAllocatorOptions[:]
	if bin := os.Getenv("CHROME_BIN"); bin != "" {
		opts = append(opts, chromedp.ExecPath(bin))
	}
	if os.Geteuid() == 0 {
		// Chrome will not start its sandbox as root, as in a cloud container.
		opts = append(opts, chromedp.NoSandbox)
	}
	actx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(cancel)
	ctx, cancel := chromedp.NewContext(actx)
	t.Cleanup(cancel)
	if err := chromedp.Run(ctx); err != nil {
		t.Fatalf("start the browser: %v", err)
	}
	return ctx
}

func TestTwoReadersSeeTheirOwnSectionsInTheBrowser(t *testing.T) {
	p := newPortalTest(t)
	p.start(t)

	browser := newBrowser(t)
	ctx, cancel := context.WithTimeout(browser, 60*time.Second)
	defer cancel()
	var adaLocation, adaNav, graceHome, graceNav string
	err := chromedp.Run(ctx,
		// Ada asks for the staff notes, signs in through Auth0 and gets them.
		chromedp.Navigate(p.srv.URL+"/portals/pets/docs/staff-notes/"),
		chromedp.WaitVisible(`//button[text()="Sign in with Auth0"]`, chromedp.BySearch),
		chromedp.Click(`//button[text()="Sign in with Auth0"]`, chromedp.BySearch),
		chromedp.WaitVisible(`.portal-account`, chromedp.ByQuery),
		chromedp.Location(&adaLocation),
		chromedp.Text(`.portal-nav`, &adaNav, chromedp.ByQuery),
		// She signs out from the navigation bar, and Grace signs in through
		// GitHub in the same browser.
		chromedp.Click(`//a[text()="Sign out"]`, chromedp.BySearch),
		chromedp.WaitVisible(`//h1[text()="You have signed out"]`, chromedp.BySearch),
		chromedp.Click(`//a[text()="Sign in again"]`, chromedp.BySearch),
		chromedp.WaitVisible(`//button[text()="Sign in with GitHub"]`, chromedp.BySearch),
		chromedp.Click(`//button[text()="Sign in with GitHub"]`, chromedp.BySearch),
		chromedp.WaitVisible(`//h1[text()="Portals"]`, chromedp.BySearch),
		chromedp.Text(`body`, &graceHome, chromedp.ByQuery),
		chromedp.Navigate(p.srv.URL+"/portals/pets/docs/guides/"),
		chromedp.WaitVisible(`.portal-account`, chromedp.ByQuery),
		chromedp.Text(`.portal-nav`, &graceNav, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("the readers' sign-ins: %v", err)
	}
	if adaLocation != p.srv.URL+"/portals/pets/docs/staff-notes/" {
		t.Errorf("Ada's sign-in ended at %s, want the staff notes", adaLocation)
	}
	for _, want := range []string{"API", "Guides", "Staff notes", "Ada Lovelace", "Sign out"} {
		if !strings.Contains(adaNav, want) {
			t.Errorf("Ada's navigation bar %q lacks %q", adaNav, want)
		}
	}
	if strings.Contains(adaNav, "Partners") {
		t.Errorf("Ada's navigation bar %q names the partners' portal", adaNav)
	}
	for _, want := range []string{"Pets", "Partners", "Grace Hopper"} {
		if !strings.Contains(graceHome, want) {
			t.Errorf("Grace's home page %q lacks %q", graceHome, want)
		}
	}
	if !strings.Contains(graceNav, "Guides") || strings.Contains(graceNav, "Staff notes") {
		t.Errorf("Grace's navigation bar %q, want the guides without the staff notes", graceNav)
	}
}
