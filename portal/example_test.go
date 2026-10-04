package portal_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

// readerKey keys the name of a signed-in reader in a request's context.
type readerKey struct{}

// signIn stands in for the program's sign-in middleware: it trusts the
// X-Reader header, where a real one checks a session cookie.
func signIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name := r.Header.Get("X-Reader"); name != "" {
			r = r.WithContext(context.WithValue(r.Context(), readerKey{}, name))
		}
		next.ServeHTTP(w, r)
	})
}

// signedOut is the access of a reader who is not signed in: no portal.
type signedOut struct{}

func (signedOut) Portal(portal.Portal) bool                  { return false }
func (signedOut) Section(portal.Portal, portal.Section) bool { return false }

// A program mounts the portal handler behind its own sign-in, beside routes
// of its own, and its request hooks read the reader from the request.
func ExampleNew() {
	h, err := portal.New(portal.Config{
		Root:    fstest.MapFS{"pets.yaml": {Data: []byte("openapi: 3.1.0\ninfo:\n  title: Pets\n  version: 1.0.0\npaths: {}\n")}},
		Portals: []portal.Portal{{Name: "Pets", Sections: []portal.Section{{Title: "API", Type: portal.SpecSection, Input: "pets.yaml"}}}},
		Access: func(r *http.Request) (portal.Access, error) {
			if _, ok := r.Context().Value(readerKey{}).(string); ok {
				return portal.Everything, nil
			}
			return signedOut{}, nil
		},
		Account: func(r *http.Request) []portal.AccountLink {
			if name, ok := r.Context().Value(readerKey{}).(string); ok {
				return []portal.AccountLink{{Label: name}, {Label: "Sign out", URL: "/auth/sign-out"}}
			}
			return []portal.AccountLink{{Label: "Sign in", URL: "/auth/sign-in"}}
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/sign-out", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.Handle("/", signIn(h))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// get prints the status and the account links of a page for a reader.
	get := func(reader, path string) {
		r, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if reader != "" {
			r.Header.Set("X-Reader", reader)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		page := string(body)
		links := "no account links"
		if start, end := strings.Index(page, `<span class="portal-account">`), strings.Index(page, "</nav>"); start >= 0 && end > start {
			links = page[start:end]
		}
		fmt.Println(resp.StatusCode, resp.Header.Get("Cache-Control"), links)
	}
	get("Ada", "/portals/pets/specs/api")
	get("", "/")
	// Output:
	// 200 private <span class="portal-account"><span>Ada</span><span><a href="/auth/sign-out">Sign out</a></span></span>
	// 200 private <span class="portal-account"><span><a href="/auth/sign-in">Sign in</a></span></span>
}
