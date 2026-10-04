package main

import (
	"context"
	"io"
	"net/http"
)

// provider is an identity provider of the example application, as the
// sign-in router serves it.
type provider struct {
	name         string       // the provider name: auth0 or github
	title        string       // its name on the sign-in page
	middleware   http.Handler // its sign-in middleware
	callbackPath string       // the path of its callback URL
	signOutPath  string       // its middleware's sign-out path
}

// router is the sign-in router: the example application's handler in front
// of the sign-in middleware of each configured identity provider.
type router struct {
	providers []provider // in the order of the sign-in page
	secure    bool       // whether the choice cookie is Secure: with an https application URL
}

// signIn puts the sign-in in front of h, the previews handler: a sign-in
// middleware for each configured identity provider of s, Auth0 by its
// issuer and GitHub through the GitHub provider, each with its provider's
// name in the context of each request, and the sign-in router before them.
func signIn(ctx context.Context, s providerSettings, h http.Handler) (http.Handler, error) {
	// HOLE(3): build the middleware of each configured provider around h, and the router before them
	return &router{}, nil
}

// ServeHTTP answers each request: it shows the sign-in page, keeps the
// reader's choice of identity provider in the choice cookie, sends a
// callback or a sign-out to the middleware of its path's provider, with the
// choice cleared at a sign-out, and any other request to the middleware of
// the reader's provider, or without one to the sign-in page.
func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// HOLE(3): route the request by its path and the choice cookie
	io.WriteString(w, mockSignInPage)
}

// mockSignInPage is the sign-in page of the router's hole.
const mockSignInPage = `<!doctype html><title>Sign in</title><h1>Sign in</h1>
<form method="post" action="/sign-in/auth0"><button>Sign in with Auth0</button></form>
<form method="post" action="/sign-in/github"><button>Sign in with GitHub</button></form>
`
