package chat

import (
	"context"
	"encoding/json"
	"errors"
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

// readerKey keys the reader ID that signIn puts into a request's context.
type readerKey struct{}

// signIn stands in for a program's sign-in middleware: it puts the request's
// Reader header into the request's context.
func signIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), readerKey{}, r.Header.Get(readerHeader))))
	})
}

// contextReader is a reader hook that names the reader whom signIn put into
// the request's context, as a program's hook reads its middleware's claims.
func contextReader(r *http.Request) string {
	id, _ := r.Context().Value(readerKey{}).(string)
	return id
}

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

// loadPage loads the chat page of c's portal through signIn and a portal
// handler from the remote address addr, with the Reader header reader unless
// it is empty, and mounts it again from its page session alone, as the join
// does, after the session's round trip through JSON, as live-templ's signer
// encodes it.
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
	signIn(servedByPortal(t, c, keep)).ServeHTTP(httptest.NewRecorder(), r)
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	var signed map[string]string
	if err := json.Unmarshal(data, &signed); err != nil {
		t.Fatal(err)
	}
	u := &url.URL{Path: "/portals/pets/chat"}
	lv := interpreter.NewCtx(t.Context(), u.Path, u, signed, true)
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
	for _, text := range []string{"192.0.2.7", "alice"} {
		reader, client := AskerOf(text, "198.51.100.9"), AskerOf("", text)
		if reader == client {
			t.Errorf("%q: the reader ID and the client give one asker", text)
		}
		// No text of the other kind names the same asker either.
		if AskerOf("", reader.key) == reader || AskerOf(client.key, "198.51.100.9") == client {
			t.Errorf("%q: the key of one kind of asker, as the other kind's text, names the same asker", text)
		}
		for _, prefix := range []string{"reader ", "client "} {
			if AskerOf(prefix+text, "198.51.100.9") == client || AskerOf("", prefix+text) == reader {
				t.Errorf("%q: a text with the prefix %q names the other kind's asker", text, prefix)
			}
		}
	}
	if AskerOf("alice", "192.0.2.1") != AskerOf("alice", "198.51.100.9") || AskerOf("", "192.0.2.7") != AskerOf("", "192.0.2.7") {
		t.Error("one reader ID from two clients, or one client, gives two askers")
	}
}

func TestReadersWithoutAnIDAskAsTheirClients(t *testing.T) {
	if AskerOf("", "192.0.2.1") == AskerOf("", "198.51.100.2") {
		t.Error("two readers without a reader ID, from two clients, share an asker")
	}
}

func TestAskRefusesTheZeroAsker(t *testing.T) {
	c := newReaderChat(t, nil, 1, 0)
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, Asker{}, "c", "Is it there?"); !errors.Is(err, ErrNoAsker) {
		t.Errorf("a question without an asker: %v, want ErrNoAsker", err)
	}
	if AskerOf("", "") == (Asker{}) {
		t.Error("the asker of a reader without a reader ID and without a client names nobody")
	}
}

func TestThePageSessionHoldsTheReaderID(t *testing.T) {
	for name, tc := range map[string]struct {
		hook         func(*http.Request) string
		reader, want string
	}{
		"a signed-in reader":              {headerReader, "alice", "alice"},
		"a reader who is not signed in":   {headerReader, "", ""},
		"no reader hook":                  {nil, "alice", ""},
		"a hook of the request's context": {contextReader, "alice", "alice"},
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
		signIn(servedByPortal(t, c, keep)).ServeHTTP(httptest.NewRecorder(), r)
		if readerIDIn(session["readerID"]) != tc.want || session["client"] != "192.0.2.1" {
			t.Errorf("%s: the page session %v, want the reader ID %q", name, session, tc.want)
		}
	}
}

func TestOneLimitForAReaderFromAnyClient(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	c := newReaderChat(t, contextReader, 2, 5)
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
	// A reader ID is the hook's text, letter case included.
	if loadPage(t, c, "Alice", "192.0.2.1:5555").limited(t) {
		t.Error("the first question of Alice counts against alice's limit")
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

func TestReaderIDsKeepEveryByte(t *testing.T) {
	c := newReaderChat(t, headerReader, 1, 2)
	if loadPage(t, c, "a\xffb", "192.0.2.1:5555").limited(t) {
		t.Fatal("the first question of the reader a\\xffb is over the limit")
	}
	if loadPage(t, c, "a\xfeb", "192.0.2.1:5555").limited(t) {
		t.Error("the readers a\\xffb and a\\xfeb share a question limit")
	}
	// A program's own question for the first reader counts against the limit
	// of the reader's tabs.
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("a\xffb", "198.51.100.9"), "x", "Is it there?"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("a program's question for the reader a\\xffb: %v, want ErrRateLimited", err)
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
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("192.0.2.7", "198.51.100.9"), "a", "Is it there?"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ask(t.Context(), "pets", portal.Everything, AskerOf("", "192.0.2.7"), "b", "Is it there?"); err != nil {
		t.Errorf("the client 192.0.2.7 shares the limit of the reader ID 192.0.2.7: %v", err)
	}
	// A program's own question for a reader counts against the limit of the
	// reader's chat pages.
	if !loadPage(t, c, "192.0.2.7", "198.51.100.2:5555").limited(t) {
		t.Error("the chat page of the reader 192.0.2.7 has a limit apart from AskerOf(\"192.0.2.7\", \"198.51.100.9\")")
	}
}

func TestTheChatPageIsPrivateWithAReaderHook(t *testing.T) {
	for name, tc := range map[string]struct {
		hook         func(*http.Request) string
		reader, want string
	}{
		"a signed-in reader":            {headerReader, "alice", "private"},
		"a reader who is not signed in": {headerReader, "", "private"},
		"no reader hook":                {nil, "alice", ""},
	} {
		c := newReaderChat(t, tc.hook, 1, 0)
		r := httptest.NewRequest(http.MethodGet, "/portals/pets/chat", nil)
		r.Header.Set(readerHeader, tc.reader)
		rec := httptest.NewRecorder()
		servedByPortal(t, c).ServeHTTP(rec, r)
		// The headers that went out with the status, not the recorder's map.
		sent := rec.Result().Header.Get("Cache-Control")
		if rec.Code != http.StatusOK || sent != tc.want {
			t.Errorf("%s: the chat page answers %d with Cache-Control %q, want %q", name, rec.Code, sent, tc.want)
		}
	}
}

func TestAPrivatePageStaysPrivateWhenItsHandlerDeletesTheHeader(t *testing.T) {
	c := newReaderChat(t, headerReader, 1, 0)
	h := c.privatePage(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Del("Cache-Control")
		http.Error(w, "gone", http.StatusNotFound)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portals/pets/chat", nil))
	if got := rec.Result().Header.Get("Cache-Control"); got != "private" {
		t.Errorf("a chat page whose handler deletes Cache-Control goes out with %q, want private", got)
	}
}
