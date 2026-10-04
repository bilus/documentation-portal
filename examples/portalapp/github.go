package main

import (
	"context"
	"net/http"

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
	// HOLE(2): pick the endpoints of the web URL and the API URL, with an HTTP client with a timeout
	return githubProvider{endpoint: github.Endpoint}
}

// Endpoint returns the provider's authorization and token URLs.
func (g githubProvider) Endpoint() oauth2.Endpoint {
	// HOLE(2): return the provider's endpoint
	return github.Endpoint
}

// Identity identifies the reader to whom GitHub gave token: the subject,
// the name and the email from GET /user, and the claims login, orgs and
// teams from GET /user/orgs and GET /user/teams, or fails the sign-in with
// an error for any answer of the API other than 200. The subject is
// github| and the user's numeric ID, and the name is the user's name, else
// the login. The claims hold the names as GitHub spells them, from every
// page of each list, and the token goes to no host other than the API's.
func (g githubProvider) Identity(ctx context.Context, token *oauth2.Token) (signin.Identity, error) {
	// HOLE(2): read the user, the organizations and the teams from the API with token
	return signin.Identity{}, nil
}
