package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/bilus/documentation-portal/examples/portalapp/mocks"
	"github.com/bilus/documentation-portal/signin"
)

// stubToken runs the authorization code flow with PKCE at the stub GitHub
// at base, as the sign-in middleware does, and returns the access token.
func stubToken(t *testing.T, base string) *oauth2.Token {
	t.Helper()
	conf := oauth2.Config{
		ClientID:     mocks.ClientID,
		ClientSecret: mocks.ClientSecret,
		Endpoint:     oauth2.Endpoint{AuthURL: base + "/login/oauth/authorize", TokenURL: base + "/login/oauth/access_token"},
		RedirectURL:  "http://portal.test/auth/github/callback",
		Scopes:       []string{"read:org"},
	}
	verifier := oauth2.GenerateVerifier()
	noRedirects := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noRedirects.Get(conf.AuthCodeURL("the state", oauth2.S256ChallengeOption(verifier)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	back, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || back.Query().Get("state") != "the state" {
		t.Fatalf("the stub's authorization answered %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	token, err := conf.Exchange(t.Context(), back.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// The organizations of the tests at the stub GitHub.
var (
	acme    = mocks.GitHubOrg{Login: "Acme", ID: 1001}
	globex  = mocks.GitHubOrg{Login: "globex", ID: 1002}
	initech = mocks.GitHubOrg{Login: "Initech", ID: 1003}
)

func TestTheGitHubProviderGivesTheIDs(t *testing.T) {
	srv := httptest.NewServer(mocks.GitHub(mocks.Grace))
	defer srv.Close()
	id, err := newGitHub(githubSettings{webURL: srv.URL, apiURL: srv.URL}, nil).Identity(t.Context(), stubToken(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	for claim, want := range map[string][]string{"id": {"2"}, "org_ids": {"1001"}, "team_ids": {"2002"}} {
		if got := id.Claims.Strings(claim); !slices.Equal(got, want) {
			t.Errorf("the claim %s holds %q, want %q", claim, got, want)
		}
	}
}

func TestTheGitHubProviderKeepsTheNamedMemberships(t *testing.T) {
	srv := httptest.NewServer(mocks.GitHub(mocks.GitHubReader{ID: 583231, Login: "Octocat", Orgs: []mocks.GitHubOrg{acme, globex, initech},
		Teams: []mocks.GitHubTeam{{Org: acme, Slug: "staff", ID: 2001}, {Org: globex, Slug: "docs", ID: 3001}, {Org: initech, Slug: "partners", ID: 4001}}}))
	defer srv.Close()
	rules := accessRules{
		Labels:   map[string][]rule{"partner": {{Provider: "github", Claim: "team_ids", Values: []string{"4001"}}}},
		Previews: []rule{{Provider: "github", Claim: "orgs", Values: []string{"GLOBEX"}}, {Provider: "auth0", Claim: "orgs", Values: []string{"acme"}}},
	}
	keep := func(claim, value string) bool { return rules.names("github", claim, value) }
	id, err := newGitHub(githubSettings{webURL: srv.URL, apiURL: srv.URL}, keep).Identity(t.Context(), stubToken(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	for claim, want := range map[string][]string{
		"login": {"Octocat"}, "id": {"583231"},
		"orgs": {"globex"}, "org_ids": {"1002"},
		"teams": {"Initech/partners"}, "team_ids": {"4001"},
	} {
		if got := id.Claims.Strings(claim); !slices.Equal(got, want) {
			t.Errorf("the claim %s holds %q, want %q", claim, got, want)
		}
	}
}

func TestTheGitHubProviderFollowsNoRedirect(t *testing.T) {
	var tokens atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			tokens.Add(1)
		}
		w.Write([]byte(`{"id":2,"login":"grace"}`))
	}))
	defer elsewhere.Close()
	stub := mocks.GitHub(mocks.Grace)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			http.Redirect(w, r, elsewhere.URL+"/user", http.StatusTemporaryRedirect)
			return
		}
		stub.ServeHTTP(w, r)
	}))
	defer srv.Close()
	g := newGitHub(githubSettings{webURL: srv.URL, apiURL: srv.URL}, nil)
	if id, err := g.Identity(t.Context(), stubToken(t, srv.URL)); err == nil || tokens.Load() != 0 {
		t.Errorf("a redirect of GET /user: the identity %+v, the error %v, %d tokens elsewhere", id, err, tokens.Load())
	}
}

func TestTheGitHubEndpoint(t *testing.T) {
	if got := newGitHub(githubSettings{apiURL: "https://api.github.com"}, nil).Endpoint(); got != github.Endpoint {
		t.Errorf("without a web URL, the endpoint is %+v, want GitHub's", got)
	}
	for _, web := range []string{"http://127.0.0.1:9200", "http://127.0.0.1:9200/"} {
		got := newGitHub(githubSettings{webURL: web, apiURL: "http://127.0.0.1:9200"}, nil).Endpoint()
		if got.AuthURL != "http://127.0.0.1:9200/login/oauth/authorize" || got.TokenURL != "http://127.0.0.1:9200/login/oauth/access_token" {
			t.Errorf("with the web URL %s, the endpoint is %+v", web, got)
		}
	}
}

func TestTheGitHubProviderNamesTheReader(t *testing.T) {
	for name, tc := range map[string]struct {
		reader             mocks.GitHubReader
		want               signin.Identity
		login, orgs, teams []string
	}{
		"Grace": {
			reader: mocks.Grace,
			want:   signin.Identity{Subject: "github|2", Name: "Grace Hopper", Email: "grace@example.com"},
			login:  []string{"grace"}, orgs: []string{"Acme"}, teams: []string{"Acme/partners"},
		},
		"a reader of several pages, without a name": {
			reader: mocks.GitHubReader{ID: 583231, Login: "Octocat", Orgs: []mocks.GitHubOrg{acme, globex, initech},
				Teams: []mocks.GitHubTeam{{Org: acme, Slug: "staff", ID: 2001}, {Org: globex, Slug: "docs", ID: 3001}, {Org: initech, Slug: "partners", ID: 4001}}},
			want:  signin.Identity{Subject: "github|583231", Name: "Octocat"},
			login: []string{"Octocat"}, orgs: []string{"Acme", "globex", "Initech"}, teams: []string{"Acme/staff", "globex/docs", "Initech/partners"},
		},
		"a reader of no organization": {
			reader: mocks.GitHubReader{ID: 9, Login: "solo", Name: "Solo"},
			want:   signin.Identity{Subject: "github|9", Name: "Solo"},
			login:  []string{"solo"},
		},
	} {
		srv := httptest.NewServer(mocks.GitHub(tc.reader))
		g := newGitHub(githubSettings{webURL: srv.URL, apiURL: srv.URL}, nil)
		id, err := g.Identity(t.Context(), stubToken(t, srv.URL))
		srv.Close()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if id.Subject != tc.want.Subject || id.Name != tc.want.Name || id.Email != tc.want.Email {
			t.Errorf("%s: the identity is %+v, want %+v", name, id, tc.want)
		}
		for claim, want := range map[string][]string{"login": tc.login, "orgs": tc.orgs, "teams": tc.teams} {
			if got := id.Claims.Strings(claim); !slices.Equal(got, want) {
				t.Errorf("%s: the claim %s holds %q, want %q", name, claim, got, want)
			}
		}
	}
}

func TestTheGitHubProviderFailsOnAnAPIError(t *testing.T) {
	stub := mocks.GitHub(mocks.GitHubReader{ID: 2, Login: "grace", Name: "Grace Hopper", Orgs: []mocks.GitHubOrg{acme, globex},
		Teams: []mocks.GitHubTeam{{Org: acme, Slug: "partners", ID: 2002}, {Org: globex, Slug: "docs", ID: 3001}}})
	for name, change := range map[string]func(w http.ResponseWriter, r *http.Request) bool{
		"a refused token": func(w http.ResponseWriter, r *http.Request) bool {
			r.Header.Set("Authorization", "Bearer an unknown token")
			return false
		},
		"a failing user": func(w http.ResponseWriter, r *http.Request) bool {
			return fail(w, r.URL.Path == "/user", http.StatusBadGateway)
		},
		"failing teams": func(w http.ResponseWriter, r *http.Request) bool {
			return fail(w, r.URL.Path == "/user/teams", http.StatusInternalServerError)
		},
		"a second page of organizations that fails": func(w http.ResponseWriter, r *http.Request) bool {
			return fail(w, r.URL.Path == "/user/orgs" && r.URL.Query().Get("page") == "2", http.StatusForbidden)
		},
		"a user that is no JSON": func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path != "/user" {
				return false
			}
			w.Write([]byte("<html>"))
			return true
		},
		"a user without an ID": func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path != "/user" {
				return false
			}
			w.Write([]byte(`{"login":"grace","name":"Grace Hopper"}`))
			return true
		},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/login/") && change(w, r) {
				return
			}
			stub.ServeHTTP(w, r)
		}))
		g := newGitHub(githubSettings{webURL: srv.URL, apiURL: srv.URL}, nil)
		if id, err := g.Identity(t.Context(), stubToken(t, srv.URL)); err == nil {
			t.Errorf("%s: the provider named %+v", name, id)
		}
		srv.Close()
	}

	// A next page on another host gets neither a request nor the token.
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		w.Write([]byte("[]"))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/orgs" && stubAuthorized(w, r, stub) {
			w.Header().Set("Link", "<"+other.URL+`/user/orgs?page=2>; rel="next"`)
			w.Write([]byte(`[{"login":"acme"}]`))
			return
		}
		stub.ServeHTTP(w, r)
	}))
	defer srv.Close()
	g := newGitHub(githubSettings{webURL: srv.URL, apiURL: srv.URL}, nil)
	if id, err := g.Identity(t.Context(), stubToken(t, srv.URL)); err == nil || elsewhere.Load() != 0 {
		t.Errorf("a next page on another host: the identity %+v, the error %v, %d requests there", id, err, elsewhere.Load())
	}
}

// stubAuthorized reports whether the stub GitHub accepts the access token
// of r, by asking it for the user.
func stubAuthorized(w http.ResponseWriter, r *http.Request, stub http.Handler) bool {
	probe := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/user", nil)
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	stub.ServeHTTP(probe, req)
	return probe.Code == http.StatusOK
}

func TestNextPage(t *testing.T) {
	g := newGitHub(githubSettings{apiURL: "https://api.github.com"}, nil)
	for link, want := range map[string]string{
		"": "",
		`<https://api.github.com/user/teams?page=2>; rel="next", <https://api.github.com/user/teams?page=5>; rel="last"`: "https://api.github.com/user/teams?page=2",
		`<https://api.github.com/user/teams?page=1>; rel="prev", <https://api.github.com/user/teams?page=3>; rel="next"`: "https://api.github.com/user/teams?page=3",
		`<https://api.github.com/user/teams?page=1>; rel="first"`:                                                        "",
	} {
		if got, err := g.nextPage(link); err != nil || got != want {
			t.Errorf("the next page of %q: %q, %v, want %q", link, got, err, want)
		}
	}
	for _, link := range []string{
		`<http://api.github.com/user/teams?page=2>; rel="next"`,
		`<https://api.github.com.evil.example/user/teams?page=2>; rel="next"`,
		`<https://api.github.com:8443/user/teams?page=2>; rel="next"`,
	} {
		if got, err := g.nextPage(link); err == nil {
			t.Errorf("the next page of %q: %q, want an error", link, got)
		}
	}
}

// fail answers with status when when holds, and reports whether it did.
func fail(w http.ResponseWriter, when bool, status int) bool {
	if when {
		http.Error(w, http.StatusText(status), status)
	}
	return when
}
