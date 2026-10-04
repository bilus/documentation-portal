//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

func TestOneQuestionLimitForAReaderInTheBrowser(t *testing.T) {
	cfg := portal.Config{Root: os.DirFS("../testdata"), Portals: petsPortal(sections("specs/petstore-3.1.yaml", "docs", ""))}
	lib, err := firstLibrary(portal.NewLibraries(cfg))
	if err != nil {
		t.Fatal(err)
	}
	m := fakemodel.New("opus", []fakemodel.Exchange{{Match: "How many pets are there?", Reply: "Three pets."}})
	c, err := chat.New(chat.Config{Model: m, Libraries: []*portal.Library{lib}, Limits: chat.Limits{Questions: 1},
		Reader: func(r *http.Request) string { return r.Header.Get("Reader") }})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chat = c.Routes()
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Every request comes from the signed-in reader alice, each from a
	// client of its own, as from a phone on the move.
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Reader", "alice")
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:443", requests.Add(1)%250+1)
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	browser := newBrowser(t)
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/portals/pets/chat"),
		chromedp.WaitVisible(`.phx-connected`, chromedp.ByQuery),
		chromedp.SendKeys(`textarea[name=question]`, "How many pets are there?", chromedp.ByQuery),
		chromedp.Click(`button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible(`//p[text()="Three pets."]`, chromedp.BySearch),
		chromedp.Navigate(srv.URL+"/portals/pets/chat"),
		chromedp.WaitVisible(`.phx-connected`, chromedp.ByQuery),
		chromedp.SendKeys(`textarea[name=question]`, "And how many cats?", chromedp.ByQuery),
		chromedp.Click(`button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible(`//p[starts-with(text(), "You have asked many questions")]`, chromedp.BySearch),
	)
	if err != nil {
		t.Fatalf("alice's second question, from a page loaded from another address, did not meet her question limit: %v", err)
	}
}
