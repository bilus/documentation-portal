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
	api      string       // the API's URL, without a trailing slash
	client   *http.Client // for the API, with a timeout of its own
}

// newGitHub returns the GitHub provider of s: GitHub's OAuth endpoints, or
// with a web URL the same paths on its host, and the API at the API URL.
func newGitHub(s githubSettings) githubProvider {
	g := githubProvider{
		endpoint: github.Endpoint,
		api:      strings.TrimSuffix(cmp.Or(s.apiURL, gitHubAPI), "/"),
		client:   &http.Client{Timeout: 30 * time.Second},
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
// the name and the email from GET /user, and the claims login, orgs and
// teams from GET /user/orgs and GET /user/teams, or fails the sign-in with
// an error for any answer of the API other than 200. The subject is
// github| and the user's numeric ID, and the name is the user's name, else
// the login. The claims hold the names as GitHub spells them, from every
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
		Login string `json:"login"`
	}](ctx, g, token, "/user/orgs")
	if err != nil {
		return signin.Identity{}, err
	}
	teams, err := pages[struct {
		Slug         string `json:"slug"`
		Organization struct {
			Login string `json:"login"`
		} `json:"organization"`
	}](ctx, g, token, "/user/teams")
	if err != nil {
		return signin.Identity{}, err
	}
	var orgNames, teamNames []string
	for _, o := range orgs {
		orgNames = append(orgNames, o.Login)
	}
	for _, t := range teams {
		teamNames = append(teamNames, t.Organization.Login+"/"+t.Slug)
	}
	return signin.Identity{
		Subject: "github|" + strconv.FormatInt(user.ID, 10),
		Name:    cmp.Or(user.Name, user.Login),
		Email:   user.Email,
		Claims:  signin.Claims{"login": user.Login, "orgs": orgNames, "teams": teamNames},
	}, nil
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
		client = http.DefaultClient
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

// nextPage returns the URL of the next page that the Link header link
// names, or "" for none, and an error for a next page on another host than
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
