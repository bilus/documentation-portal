package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/bilus/documentation-portal/signin"
)

// githubProvider is GitHub as a signin.Provider, for an OAuth app: its
// OAuth endpoints, and the identity of the reader behind an access token,
// from GitHub's API.
type githubProvider struct {
	endpoint oauth2.Endpoint
	api      string                         // the API's URL, without a trailing slash
	client   *http.Client                   // for the API, with a timeout of its own
	keep     func(claim, value string) bool // whether a rule names a membership; nil keeps every one
}

// newGitHub returns the GitHub provider of s: GitHub's OAuth endpoints, or
// with a web URL the same paths on its host, the API at the API URL, and
// keep, which selects the memberships kept in the claims: those of the
// claims orgs and org_ids, and teams and team_ids, accepted by keep, and
// every one for a nil keep.
func newGitHub(s githubSettings, keep func(claim, value string) bool) githubProvider {
	g := githubProvider{
		endpoint: github.Endpoint,
		api:      strings.TrimSuffix(cmp.Or(s.apiURL, gitHubAPI), "/"),
		client:   &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect},
		keep:     keep,
	}
	if web := strings.TrimSuffix(s.webURL, "/"); web != "" {
		g.endpoint = oauth2.Endpoint{AuthURL: web + "/login/oauth/authorize", TokenURL: web + "/login/oauth/access_token"}
	}
	return g
}

// Endpoint returns the provider's authorization and token URLs.
func (g githubProvider) Endpoint() oauth2.Endpoint {
	return g.endpoint
}

// Identity identifies the reader to whom GitHub gave token: the subject,
// the name and the email from GET /user, the claims login and id, and the
// claims orgs, org_ids, teams and team_ids of the memberships named by a
// GitHub rule, from GET /user/orgs and GET /user/teams, or fails the sign-in
// with an error for any answer of the API other than 200. The subject is
// github| and the user's numeric ID, and the name is the user's name, else
// the login. The claims hold the names as GitHub spells them and the IDs
// in decimal, of the memberships allowed by the provider's keep, from every
// page of each list, and the token goes to no host other than the API's.
func (g githubProvider) Identity(ctx context.Context, token *oauth2.Token) (signin.Identity, error) {
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if _, err := g.get(ctx, token, g.api+"/user", &user); err != nil {
		return signin.Identity{}, err
	}
	if user.ID == 0 || user.Login == "" {
		return signin.Identity{}, errors.New("github: the user has no ID or login")
	}
	orgs, err := pages[struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}](ctx, g, token, "/user/orgs")
	if err != nil {
		return signin.Identity{}, err
	}
	teams, err := pages[struct {
		ID           int64  `json:"id"`
		Slug         string `json:"slug"`
		Organization struct {
			Login string `json:"login"`
		} `json:"organization"`
	}](ctx, g, token, "/user/teams")
	if err != nil {
		return signin.Identity{}, err
	}
	var orgNames, orgIDs, teamNames, teamIDs []string
	for _, o := range orgs {
		name, id := o.Login, strconv.FormatInt(o.ID, 10)
		if g.keeps("orgs", name) || g.keeps("org_ids", id) {
			orgNames, orgIDs = append(orgNames, name), append(orgIDs, id)
		}
	}
	for _, t := range teams {
		name, id := t.Organization.Login+"/"+t.Slug, strconv.FormatInt(t.ID, 10)
		if g.keeps("teams", name) || g.keeps("team_ids", id) {
			teamNames, teamIDs = append(teamNames, name), append(teamIDs, id)
		}
	}
	id := strconv.FormatInt(user.ID, 10)
	return signin.Identity{
		Subject: "github|" + id,
		Name:    cmp.Or(user.Name, user.Login),
		Email:   user.Email,
		Claims: signin.Claims{"login": user.Login, "id": id, "orgs": orgNames, "org_ids": orgIDs,
			"teams": teamNames, "team_ids": teamIDs},
	}, nil
}

// noRedirect stops the API's client at a redirect, whose answer then fails
// the identity, so that the token reaches the API's host alone.
func noRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// keeps reports whether the claims keep the membership value of claim:
// every one without a filter.
func (g githubProvider) keeps(claim, value string) bool {
	return g.keep == nil || g.keep(claim, value)
}

// maxPages bounds the pages of one list, of 100 entries each.
const maxPages = 10

// pages returns the entries of every page of the list at path of g's API,
// following each page's link to the next on the API's host alone.
func pages[T any](ctx context.Context, g githubProvider, token *oauth2.Token, path string) ([]T, error) {
	var all []T
	next := g.api + path + "?per_page=100"
	for n := 0; next != ""; n++ {
		if n == maxPages {
			return nil, fmt.Errorf("github: %s has more than %d pages", path, maxPages)
		}
		var page []T
		link, err := g.get(ctx, token, next, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if next, err = g.nextPage(link); err != nil {
			return nil, err
		}
	}
	return all, nil
}

// get reads the API's answer at u into v with token, and returns the
// answer's Link header, or an error for an answer other than 200.
func (g githubProvider) get(ctx context.Context, token *oauth2.Token, u string, v any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("github: %w", err)
	}
	token.SetAuthHeader(req)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := g.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github: GET %s answered %s", req.URL.Path, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v); err != nil {
		return "", fmt.Errorf("github: read %s: %w", req.URL.Path, err)
	}
	return resp.Header.Get("Link"), nil
}

// nextPage returns the URL of the next page named by the Link header
// link, or "" for none, and an error for a next page on another host than
// the API's, so that the token never leaves the API.
func (g githubProvider) nextPage(link string) (string, error) {
	for _, part := range strings.Split(link, ",") {
		target, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.Contains(params, `rel="next"`) {
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(target), "<"), ">")
		next, err := url.Parse(raw)
		api, apiErr := url.Parse(g.api)
		if err != nil || apiErr != nil || next.Scheme != api.Scheme || next.Host != api.Host {
			return "", fmt.Errorf("github: the next page %q is not on the API's host", raw)
		}
		return raw, nil
	}
	return "", nil
}
