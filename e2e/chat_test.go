//go:build e2e

package e2e

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

func TestChatAnswersAQuestion(t *testing.T) {
	cfg := portal.Config{Root: os.DirFS("../testdata"), Portals: petsPortal(sections("specs/petstore-3.1.yaml", "docs", ""))}
	lib, err := firstLibrary(portal.NewLibraries(cfg))
	if err != nil {
		t.Fatal(err)
	}
	m := fakemodel.New("opus", []fakemodel.Exchange{
		{Match: "How do I get one pet?", Call: &fakemodel.Call{Name: "list_operations", Args: map[string]any{}}},
		{Match: "showPetById", Reply: "Call [Info for a specific pet](/portals/pets/specs/api#/operations/showPetById)."},
	})
	c, err := chat.New(chat.Config{Model: m, Libraries: []*portal.Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Chat = c.Routes()
	h, err := portal.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	browser := newBrowser(t)
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	var href, target, label string
	var ok bool
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/portals/pets/chat"),
		chromedp.WaitVisible(`.phx-connected`, chromedp.ByQuery),
		chromedp.AttributeValue(`textarea[name=question]`, "aria-label", &label, &ok, chromedp.ByQuery),
		chromedp.SendKeys(`textarea[name=question]`, "How do I get one pet?", chromedp.ByQuery),
		chromedp.Click(`button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible(`//a[text()="Info for a specific pet"]`, chromedp.BySearch),
		chromedp.AttributeValue(`//a[text()="Info for a specific pet"]`, "href", &href, &ok, chromedp.BySearch),
		chromedp.AttributeValue(`//a[text()="Info for a specific pet"]`, "target", &target, &ok, chromedp.BySearch),
	)
	if err != nil {
		t.Fatalf("the chat did not answer: %v", err)
	}
	if href != "/portals/pets/specs/api#/operations/showPetById" || target != "_blank" {
		t.Errorf("the answer's link: href %q, target %q", href, target)
	}
	if label == "" {
		t.Error("the question's textarea has no accessible name")
	}
}
