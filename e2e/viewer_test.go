//go:build e2e

// Package e2e drives the portal in a headless browser. Run these tests with
// `devbox run make test-e2e`.
package e2e

import (
	"context"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/bilus/documentation-portal/portal"
)

func TestViewerRendersSampleBundles(t *testing.T) {
	t.Skip("HOLE(4): the viewer page, the raw spec and the Elements assets are served for real")

	browser := newBrowser(t)
	for _, tc := range []struct{ file, version, description string }{
		{"petstore-3.0.yaml", "v1.0.0", "written for OpenAPI 3.0"},
		{"petstore-3.1.yaml", "v2.0.0", "written for OpenAPI 3.1"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			h, err := portal.New(portal.Config{Specs: os.DirFS("../testdata/specs"), SpecPath: tc.file})
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(h)
			defer srv.Close()
			page := srv.URL + "/specs/" + tc.file

			overview := pageText(t, browser, page, "Show a pet")
			for _, want := range []string{"Petstore", tc.version, tc.description} {
				if !strings.Contains(overview, want) {
					t.Errorf("overview does not show %q", want)
				}
			}
			lines := strings.Split(overview, "\n")
			for _, want := range []string{"List pets", "Create a pet", "Show a pet", "NewPet", "Pet", "Pets", "Error"} {
				if !slices.Contains(lines, want) {
					t.Errorf("navigation does not list %q", want)
				}
			}

			// Pet reaches the operation only through a $ref, and microchipId is one of its fields.
			operation := pageText(t, browser, page+"#/operations/showPetById", "microchipId")
			for _, want := range []string{"GET", "/pets/{petId}", "petId", "200"} {
				if !strings.Contains(operation, want) {
					t.Errorf("operation does not show %q", want)
				}
			}
			model := pageText(t, browser, page+"#/schemas/Pet", "microchipId")

			for name, text := range map[string]string{"overview": overview, "operation": operation, "model": model} {
				if strings.Contains(text, "$ref") {
					t.Errorf("%s shows a raw $ref", name)
				}
			}
		})
	}
}

func newBrowser(t *testing.T) context.Context {
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

// pageText opens url, waits for an element whose text is wait, and returns
// the text of the page.
func pageText(t *testing.T, browser context.Context, url, wait string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	var text string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`//*[text()="`+wait+`"]`, chromedp.BySearch),
		chromedp.Text("body", &text, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("%s: %v", url, err)
	}
	return text
}
