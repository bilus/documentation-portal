//go:build e2e && chat

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bilus/documentation-portal/examples/portalapp/mocks"
	"github.com/chromedp/chromedp"
)

func TestOneQuestionLimitForAReaderInTheBrowser(t *testing.T) {
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
