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

func TestRawSVGRunsNoScript(t *testing.T) {
	h, err := portal.New(portal.Config{Root: os.DirFS("../testdata"), Portals: petsPortal(sections("specs/petstore-3.1.yaml", "docs", ""))})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(newBrowser(t), 30*time.Second)
	defer cancel()
	var root string
	var ran bool
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/portals/pets/raw/documents/evil.svg"),
		chromedp.Evaluate(`document.documentElement.localName`, &root),
		chromedp.Evaluate(`document.documentElement.hasAttribute("data-ran")`, &ran),
	)
	if err != nil {
		t.Fatal(err)
	}
	if root != "svg" {
		t.Fatalf("the raw URL did not show the SVG: root element %q", root)
	}
	if ran {
		t.Error("the SVG's script ran")
	}
}

// sidebarStyle is the computed style of the sidebar, a group heading and an
// item, the open one included, as Elements lays them out.
const sidebarStyle = `(() => {
  const pick = (e, keys) => { const s = getComputedStyle(e); return Object.fromEntries(keys.map(k => [k, s[k]])); };
  const side = document.querySelector(".sl-elements-api > .sl-flex > div");
  const heading = side.querySelector(".sl-uppercase");
  const item = side.querySelector(".ElementsTableOfContentsItem > div:not(.sl-bg-primary-tint)");
  const open = side.querySelector(".ElementsTableOfContentsItem > .sl-bg-primary-tint");
  return JSON.stringify({
    side: pick(side, ["width", "backgroundColor", "borderRightWidth", "borderRightColor", "paddingTop", "fontFamily"]),
    heading: pick(heading, ["fontSize", "fontWeight", "letterSpacing", "textTransform", "paddingLeft", "color"]),
    item: pick(item, ["height", "paddingLeft", "paddingRight", "backgroundColor", "fontSize", "color"]),
    open: pick(open, ["height", "backgroundColor", "fontSize", "color"]),
  });
})()`

func TestDocSidebarMatchesElements(t *testing.T) {
	h, err := portal.New(portal.Config{Root: os.DirFS("../testdata"), Portals: petsPortal(sections("specs/petstore-3.1.yaml", "docs", ""))})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	browser := newBrowser(t)
	pageText(t, browser, srv.URL+"/portals/pets/specs/api#/operations/showPetById", "microchipId")
	var elements, docs string
	if err := chromedp.Run(browser, chromedp.Evaluate(sidebarStyle, &elements)); err != nil {
		t.Fatal(err)
	}
	pageText(t, browser, srv.URL+"/portals/pets/docs/documents/guide/intro.md", "Introduction")
	if err := chromedp.Run(browser, chromedp.Evaluate(sidebarStyle, &docs)); err != nil {
		t.Fatal(err)
	}
	if docs != elements {
		t.Errorf("the document sidebar differs from the Elements sidebar:\ndocs:     %s\nelements: %s", docs, elements)
	}
}
