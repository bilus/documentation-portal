//go:build e2e

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/bilus/documentation-portal/examples/portalapp/mocks"
)

// newBrowser starts a headless Chrome for the test, as the library's
// browser tests do: CHROME_BIN names it, else chromedp finds one.
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
	t.Skip("HOLE(4): build each snapshot's portal handler with the access hook and the account hook")
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

func TestOneQuestionLimitForAReaderInTheBrowser(t *testing.T) {
	t.Skip("HOLE(4): add the chat with the reader hook of the sign-in, with an API key")
	model := httptest.NewServer(mocks.Model())
	defer model.Close()
	p := newPortalTest(t)
	p.env["ANTHROPIC_API_KEY"], p.env["ANTHROPIC_BASE_URL"] = "a test key", model.URL
	_, h, err := startup(t.Context(), func(name string) string { return p.env[name] })
	if err != nil {
		t.Fatal(err)
	}
	// Each request comes from a client of its own, as from a phone on the
	// move, so that only the reader's ID can tie the questions together.
	var requests atomic.Int32
	p.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:443", requests.Add(1)%250+1)
		h.ServeHTTP(w, r)
	})
	p.srv.Start()

	browser := newBrowser(t)
	ctx, cancel := context.WithTimeout(browser, 90*time.Second)
	defer cancel()
	ask := func(question, answer string) chromedp.Tasks {
		return chromedp.Tasks{
			chromedp.SendKeys(`textarea[name=question]`, question, chromedp.ByQuery),
			chromedp.Click(`button[type=submit]`, chromedp.ByQuery),
			chromedp.WaitVisible(answer, chromedp.BySearch),
		}
	}
	tasks := chromedp.Tasks{
		chromedp.Navigate(p.srv.URL + "/portals/pets/chat"),
		chromedp.WaitVisible(`//button[text()="Sign in with Auth0"]`, chromedp.BySearch),
		chromedp.Click(`//button[text()="Sign in with Auth0"]`, chromedp.BySearch),
		chromedp.WaitVisible(`.phx-connected`, chromedp.ByQuery),
	}
	// The chat's limit: 20 questions an hour for each reader.
	for i := 1; i <= 20; i++ {
		tasks = append(tasks, ask(fmt.Sprintf("Question %d?", i), fmt.Sprintf(`//p[text()="Answer %d."]`, i)))
	}
	tasks = append(tasks,
		chromedp.Navigate(p.srv.URL+"/portals/pets/chat"),
		chromedp.WaitVisible(`.phx-connected`, chromedp.ByQuery),
		ask("Question 21?", `//p[starts-with(text(), "You have asked many questions")]`),
	)
	if err := chromedp.Run(ctx, tasks); err != nil {
		t.Fatalf("Ada's 21st question, from a page loaded from another address, did not meet her question limit: %v", err)
	}
}
