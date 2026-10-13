package portal_test

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

// guide is a markdown file with an embedded video, attempts at active content
// around it, a fenced JSON block and a fenced block with no language.
const guide = "# Guide\n\n" +
	`<video style="border:1px solid black" width="960" height="540" controls>` + "\n" +
	`  <source src="https://s.example.com/other/guide.mp4" type="video/mp4">Your browser does not support HTML5 video.</video>` + "\n\n" +
	`<video src="https://s.example.com/x.mp4" onerror="alert(1)" poster="javascript:alert(2)"></video>` + "\n\n" +
	`<video><source src="javascript:alert(3)"><source src="http://plain.example.com/y.mp4"></video>` + "\n\n" +
	"<script>alert(4)</script>\n\n" +
	`<iframe src="https://evil.example.com/"></iframe>` + "\n\n" +
	"```json\n{\"hasLiveData\": true, \"mqKey\": \"#.b.84246\"}\n```\n\n" +
	"```\nplain text\n```\n"

// guidePage returns the document page of the guide, from a portal built with
// opts.
func guidePage(t *testing.T, opts ...portal.Option) string {
	t.Helper()
	root := fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}, "docs/guide.md": {Data: []byte(guide)}}
	h, err := portal.New(portal.Config{Root: root, Portals: petsPortal(sections("apis/pets.yaml", "docs", ""))}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(h, "/portals/pets/docs/documents/guide.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("the guide: %d %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// guideBody returns the rendered markdown of a guide page, without the
// page's own markup around it.
func guideBody(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, `<h1 id="guide">`)
	end := strings.LastIndex(page, "</pre>")
	if start < 0 || end < start {
		t.Fatalf("the page has no rendered guide: %s", page)
	}
	return page[start : end+len("</pre>")]
}

func TestWithoutOptionsRawHTMLIsDroppedAndCodeIsPlain(t *testing.T) {
	page := guidePage(t)

	for _, absent := range []string{"<video", "<source", "<script", "<iframe", "portal-code", "portal-chroma"} {
		if strings.Contains(page, absent) {
			t.Errorf("without options the page has %q, but raw HTML is dropped and code is not highlighted: %s", absent, page)
		}
	}
	if !strings.Contains(page, "<pre><code") {
		t.Errorf("without options a fenced block is a plain pre and code: %s", page)
	}
}

func TestWithEmbeddedVideoKeepsThePlayerAndNothingActive(t *testing.T) {
	page := guidePage(t, portal.WithEmbeddedVideo())

	for _, want := range []string{
		`<video width="960" height="540" controls`,
		`<source src="https://s.example.com/other/guide.mp4" type="video/mp4">`,
		`<video src="https://s.example.com/x.mp4">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s: %s", want, page)
		}
	}
	body := guideBody(t, page)
	for _, absent := range []string{"style=", "onerror", "javascript:", "http://plain.example.com", "<script", "alert(4)", "<iframe", "evil.example.com"} {
		if strings.Contains(body, absent) {
			t.Errorf("the guide has %q, which the sanitiser must remove: %s", absent, body)
		}
	}
}

func TestWithCodeHighlightingPutsCodeOnADarkBackground(t *testing.T) {
	page := guidePage(t, portal.WithCodeHighlighting(""))

	if !strings.Contains(page, `class="sl-prose sl-markdown-viewer portal-code"`) {
		t.Errorf("the document body is not marked for the code stylesheet: %s", page)
	}
	if !strings.Contains(page, "<style>") || !strings.Contains(page, ".portal-code pre{") {
		t.Errorf("the page lacks the code stylesheet, which gives every code block its background: %s", page)
	}
	if !strings.Contains(page, `class="portal-chroma"`) || !strings.Contains(page, `class="portal-nt"`) || !strings.Contains(page, "hasLiveData") {
		t.Errorf("the JSON block is not highlighted with prefixed classes: %s", page)
	}
	if !strings.Contains(page, "<pre><code>plain text") {
		t.Errorf("a block without a language is a plain pre, which the stylesheet still darkens: %s", page)
	}
	if strings.Contains(page, "<video") {
		t.Errorf("highlighting alone does not render raw HTML: %s", page)
	}
}

func TestWithCodeHighlightingRefusesAnUnknownStyle(t *testing.T) {
	root := fstest.MapFS{"apis/pets.yaml": {Data: []byte(pets)}}
	_, err := portal.New(portal.Config{Root: root, Portals: petsPortal(sections("apis/pets.yaml", "", ""))}, portal.WithCodeHighlighting("no-such-style"))
	if err == nil || !strings.Contains(err.Error(), "no-such-style") {
		t.Errorf("err = %v, want one naming the unknown style", err)
	}
}

func TestBothOptionsTogether(t *testing.T) {
	page := guidePage(t, portal.WithEmbeddedVideo(), portal.WithCodeHighlighting("monokai"))

	if !strings.Contains(page, `<source src="https://s.example.com/other/guide.mp4"`) || !strings.Contains(page, `class="portal-nt"`) {
		t.Errorf("the page lacks the video or the highlighted JSON: %s", page)
	}
	if strings.Contains(page, "<script") || strings.Contains(page, "onerror") {
		t.Errorf("the sanitiser let active content through: %s", page)
	}
}
