package chat

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bilus/live-templ/interpreter"

	"github.com/bilus/documentation-portal/internal/fakemodel"
)

func TestClientOf(t *testing.T) {
	for _, tc := range []struct{ addr, want string }{
		{"192.0.2.7:5555", "192.0.2.7"},
		{"[::ffff:192.0.2.7]:5555", "192.0.2.7"},
		// One IPv6 host often holds a whole /64, so the /64 is the client.
		{"[2001:db8:1:2:3:4:5:6]:443", "2001:db8:1:2::/64"},
		{"[2001:db8:1:2::9]:80", "2001:db8:1:2::/64"},
	} {
		r := httptest.NewRequest("GET", "/chat", nil)
		r.RemoteAddr = tc.addr
		if got, err := clientOf(r); err != nil || got["client"] != tc.want {
			t.Errorf("%s: %v, %v, want %s", tc.addr, got, err, tc.want)
		}
	}
}

func TestAnswerHTMLOpensLinksInANewTab(t *testing.T) {
	got := answerHTML("See [the guide](/docs/a.md#setup).")
	for _, want := range []string{`href="/docs/a.md#setup"`, `target="_blank"`, `rel="nofollow noopener"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s has no %s", got, want)
		}
	}
}

func TestAnswerHTMLLinksOnlyThePortal(t *testing.T) {
	for src, want := range map[string]string{
		"[docs](https://evil.example/a)":       "docs (https://evil.example/a)",
		"[docs](//evil.example/a)":             "docs (//evil.example/a)",
		"[docs](///evil.example/a)":            "docs (///evil.example/a)",
		"[docs](/&#47;evil.example/a)":         "docs (//evil.example/a)",
		`[docs](/\evil.example/a)`:             `docs (/\evil.example/a)`,
		"See https://evil.example/a":           "See https://evil.example/a",
		"See <https://evil.example/a>":         "See https://evil.example/a",
		"![a cat](https://evil.example/c.png)": "a cat",
	} {
		got := answerHTML(src)
		if strings.Contains(got, "<a ") || strings.Contains(got, "<img") || !strings.Contains(got, want) {
			t.Errorf("%s: %s, want the text %s", src, got, want)
		}
	}
	if got := answerHTML("![a pet](/raw/a.png)"); !strings.Contains(got, `<img src="/raw/a.png" alt="a pet"`) {
		t.Errorf("an image of the portal: %s", got)
	}
}

func TestAnswerHTMLHasNoActiveContent(t *testing.T) {
	got := answerHTML("<script>alert(1)</script>\n\n[x](javascript:alert(1)) <img src=x onerror=alert(1)>")
	for _, bad := range []string{"<script", `href="javascript:`, "onerror", "<!--"} {
		if strings.Contains(got, bad) {
			t.Errorf("%s holds %s", got, bad)
		}
	}
}

func TestChatPageAsksItsPortal(t *testing.T) {
	m := fakemodel.New("opus", []fakemodel.Exchange{{Match: "questions from customers about the Store API.", Reply: "From the store."}})
	c, err := New(Config{Model: m, Libraries: twoPortals(t)})
	if err != nil {
		t.Fatal(err)
	}
	u := &url.URL{Path: "/portals/store/chat"}
	lv := interpreter.NewCtx(t.Context(), u.Path, u, map[string]string{"client": "client"}, false)
	p, err := c.mount(lv, c.agents[1])
	if err != nil {
		t.Fatal(err)
	}
	p.Form.Question = "What is in the store?"
	p.Ask(lv)
	if last := p.Messages[len(p.Messages)-1]; last.Error || !strings.Contains(last.HTML, "From the store.") {
		t.Errorf("the Store page's answer: %+v", last)
	}
}
