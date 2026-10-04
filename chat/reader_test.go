package chat

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bilus/live-templ/interpreter"
	"github.com/bilus/live-templ/live"

	"github.com/bilus/documentation-portal/internal/fakemodel"
	"github.com/bilus/documentation-portal/portal"
)

// readerHeader names the reader of a request to headerReader.
const readerHeader = "Reader"

// headerReader is a reader hook that names the reader in the request's
// Reader header, and no reader without the header.
func headerReader(r *http.Request) string { return r.Header.Get(readerHeader) }

// newReaderChat returns the chat of the Pets library with the reader hook
// reader, or none for nil, a question limit of questions an hour, and a model
// that answers "Yes." answers times.
func newReaderChat(t *testing.T, reader func(*http.Request) string, questions, answers int) *Chat {
	t.Helper()
	script := make([]fakemodel.Exchange, answers)
	for i := range script {
		script[i] = fakemodel.Exchange{Reply: "Yes."}
	}
	c, err := New(Config{Model: fakemodel.New("opus", script), Libraries: []*portal.Library{library(t)}, Limits: Limits{Questions: questions, Window: time.Hour}, Reader: reader})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// openPage is a chat page in a browser tab, mounted from its page session as
// at the join.
type openPage struct {
	*page
	lv live.Ctx
}

// loadPage loads the chat page of c's portal through a portal handler from
// the remote address addr, with the Reader header reader unless it is empty,
// and mounts it again from its page session alone, as the join does.
func loadPage(t *testing.T, c *Chat, reader, addr string) openPage {
	t.Helper()
	var session map[string]string
	keep := portal.Route{Pattern: "GET /session", Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var err error
		if session, err = c.sessionOf(r); err != nil {
			t.Error(err)
		}
	})}
	r := httptest.NewRequest(http.MethodGet, "/session", nil)
	r.RemoteAddr = addr
	if reader != "" {
		r.Header.Set(readerHeader, reader)
	}
	servedByPortal(t, c, keep).ServeHTTP(httptest.NewRecorder(), r)
	u := &url.URL{Path: "/portals/pets/chat"}
	lv := interpreter.NewCtx(t.Context(), u.Path, u, session, true)
	p, err := c.mount(lv, c.currentAgents()[0])
	if err != nil {
		t.Fatal(err)
	}
	return openPage{p, lv}
}

// limited asks a question on the page, and reports whether the page refuses
// it over the question limit. It fails the test on any other error.
func (o openPage) limited(t *testing.T) bool {
	t.Helper()
	o.Form.Question = "Is it there?"
	o.Ask(o.lv)
	last := o.Messages[len(o.Messages)-1]
	if strings.Contains(last.HTML, "You have asked many questions") {
		return true
	}
	if last.Error || !strings.Contains(last.HTML, "Yes.") {
		t.Fatalf("the page's answer: %+v", last)
	}
	return false
}

func TestAReaderAndAClientNeverShareAnAsker(t *testing.T) {
	for _, text := range []string{"192.0.2.7", "", "alice"} {
		reader, client := ReaderAsker(text), ClientAsker(text)
		if reader == client {
			t.Errorf("%q: ReaderAsker and ClientAsker give one asker", text)
		}
		// No text of the other kind names the same asker either.
		if ClientAsker(reader.key) == reader || ReaderAsker(client.key) == client {
			t.Errorf("%q: the key of one kind of asker, as the other kind's text, names the same asker", text)
		}
	}
	if ReaderAsker("alice") != ReaderAsker("alice") || ClientAsker("192.0.2.7") != ClientAsker("192.0.2.7") {
		t.Error("one reader ID or one client gives two askers")
	}
}

func TestThePageSessionHoldsTheReaderID(t *testing.T) {
	for name, tc := range map[string]struct {
		hook         func(*http.Request) string
		reader, want string
	}{
		"a signed-in reader":            {headerReader, "alice", "alice"},
		"a reader who is not signed in": {headerReader, "", ""},
		"no reader hook":                {nil, "alice", ""},
	} {
		c := newReaderChat(t, tc.hook, 1, 0)
		var session map[string]string
		keep := portal.Route{Pattern: "GET /session", Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			var err error
			if session, err = c.sessionOf(r); err != nil {
				t.Error(err)
			}
		})}
		r := httptest.NewRequest(http.MethodGet, "/session", nil)
		r.Header.Set(readerHeader, tc.reader)
		servedByPortal(t, c, keep).ServeHTTP(httptest.NewRecorder(), r)
		if session["readerID"] != tc.want || session["client"] != "192.0.2.1" {
			t.Errorf("%s: the page session %v, want the reader ID %q", name, session, tc.want)
		}
	}
}

func TestOneLimitForAReaderFromAnyClient(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	c := newReaderChat(t, headerReader, 2, 4)
	c.now = func() time.Time { return now }
	desk := loadPage(t, c, "alice", "192.0.2.1:5555")
	phone := loadPage(t, c, "alice", "[2001:db8:1:2::9]:443")
	if desk.limited(t) || phone.limited(t) {
		t.Fatal("alice's first two questions are over the limit")
	}
	if !desk.limited(t) {
		t.Error("alice's third question in the hour, from her first address again, is not over the limit")
	}
	// Bob reads behind alice's first address, as behind one proxy.
	if loadPage(t, c, "bob", "192.0.2.1:5555").limited(t) {
		t.Error("bob's first question counts against alice's limit")
	}
	now = now.Add(time.Hour)
	if phone.limited(t) {
		t.Error("alice's question after the window is over the limit")
	}
}

func TestPagesWithoutAReaderIDCountByClient(t *testing.T) {
	for name, tc := range map[string]struct {
		hook   func(*http.Request) string
		reader string // the Reader header of every page
	}{
		"a reader who is not signed in": {headerReader, ""},
		"no reader hook":                {nil, "alice"},
	} {
		c := newReaderChat(t, tc.hook, 1, 2)
		if loadPage(t, c, tc.reader, "192.0.2.1:5555").limited(t) {
			t.Fatalf("%s: the first question is over the limit", name)
		}
		if !loadPage(t, c, tc.reader, "192.0.2.1:6666").limited(t) {
			t.Errorf("%s: a second page from the same client is not over the limit", name)
		}
		if loadPage(t, c, tc.reader, "198.51.100.2:5555").limited(t) {
			t.Errorf("%s: a page from another client is over the first client's limit", name)
		}
	}
}

func TestAReaderIDNeverSharesAClientsLimit(t *testing.T) {
	c := newReaderChat(t, headerReader, 1, 2)
	if loadPage(t, c, "192.0.2.7", "192.0.2.7:5555").limited(t) {
		t.Fatal("the first question of the reader 192.0.2.7 is over the limit")
	}
	if loadPage(t, c, "", "192.0.2.7:5555").limited(t) {
		t.Error("a reader who is not signed in, from the client 192.0.2.7, shares the limit of the reader ID 192.0.2.7")
	}
}

func TestAskCountsAReaderApartFromAClient(t *testing.T) {
	c := newReaderChat(t, headerReader, 1, 2)
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, ReaderAsker("192.0.2.7"), "a", "Is it there?"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, ClientAsker("192.0.2.7"), "b", "Is it there?"); err != nil {
		t.Errorf("the client 192.0.2.7 shares the limit of the reader ID 192.0.2.7: %v", err)
	}
	// A program's own question for a reader counts against the limit of the
	// reader's chat pages.
	if !loadPage(t, c, "192.0.2.7", "198.51.100.2:5555").limited(t) {
		t.Error("the chat page of the reader 192.0.2.7 has a limit apart from ReaderAsker(\"192.0.2.7\")")
	}
}

func TestTheChatPageIsPrivateWithAReaderHook(t *testing.T) {
	for name, tc := range map[string]struct {
		hook func(*http.Request) string
		want string
	}{
		"a reader hook":  {headerReader, "private"},
		"no reader hook": {nil, ""},
	} {
		c := newReaderChat(t, tc.hook, 1, 0)
		r := httptest.NewRequest(http.MethodGet, "/portals/pets/chat", nil)
		r.Header.Set(readerHeader, "alice")
		rec := httptest.NewRecorder()
		servedByPortal(t, c).ServeHTTP(rec, r)
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != tc.want {
			t.Errorf("%s: the chat page answers %d with Cache-Control %q, want %q", name, rec.Code, rec.Header().Get("Cache-Control"), tc.want)
		}
	}
}
