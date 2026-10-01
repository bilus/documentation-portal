package chat

import (
	"net/http/httptest"
	"strings"
	"testing"
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
	for _, src := range []string{"[a](https://example.com/a)", "See https://example.com/a", "<https://example.com/a>"} {
		got := answerHTML(src)
		for _, want := range []string{`href="https://example.com/a"`, `target="_blank"`, `rel="nofollow noopener"`} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %s has no %s", src, got, want)
			}
		}
	}
}

func TestAnswerHTMLHasNoActiveContent(t *testing.T) {
	got := answerHTML("<script>alert(1)</script>\n\n[x](javascript:alert(1)) <img src=x onerror=alert(1)>")
	for _, bad := range []string{"<script", "javascript:", "onerror", "<!--"} {
		if strings.Contains(got, bad) {
			t.Errorf("%s holds %s", got, bad)
		}
	}
}
