package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bilus/documentation-portal/signin"
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
// issuer and GitHub through the GitHub provider, which keeps the
// memberships named by a GitHub rule of rules, each with its provider's
// name in the context of each request, and the sign-in router before them.
func signIn(ctx context.Context, s providerSettings, rules accessRules, h http.Handler) (http.Handler, error) {
	app, err := url.Parse(s.appURL)
	rt := &router{secure: err == nil && app.Scheme == "https"}
	add := func(p provider, cfg signin.Config) error {
		cfg.CallbackURL, cfg.SignOutPath, cfg.Key = s.appURL+p.callbackPath, p.signOutPath, []byte(s.sessionKey)
		mw, err := signin.New(ctx, cfg, withProvider(p.name, h))
		if err != nil {
			return fmt.Errorf("%s: %w", p.title, err)
		}
		p.middleware = mw
		rt.providers = append(rt.providers, p)
		return nil
	}
	if s.auth0.clientID != "" {
		if err := add(newProvider("auth0", "Auth0"), signin.Config{
			Issuer:       s.auth0.issuer,
			ClientID:     s.auth0.clientID,
			ClientSecret: s.auth0.clientSecret,
			LogoutURL:    s.auth0.logoutURL,
		}); err != nil {
			return nil, err
		}
	}
	if s.github.clientID != "" {
		if err := add(newProvider("github", "GitHub"), signin.Config{
			Provider:     newGitHub(s.github, func(claim, value string) bool { return rules.names("github", claim, value) }),
			ClientID:     s.github.clientID,
			ClientSecret: s.github.clientSecret,
			Scopes:       []string{"read:org"},
		}); err != nil {
			return nil, err
		}
	}
	if len(rt.providers) == 0 {
		return nil, errors.New("no identity provider")
	}
	return rt, nil
}

// newProvider returns the provider of name, titled title, with its routes
// under /auth/{name}/.
func newProvider(name, title string) provider {
	return provider{name: name, title: title, callbackPath: "/auth/" + name + "/callback", signOutPath: "/auth/" + name + "/sign-out"}
}

// withProvider returns h with the provider name in the context of each
// request, for the access hook.
func withProvider(name string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), providerKey{}, name)))
	})
}

// The sign-in page's path, and the choice cookie's name and lifetime.
const (
	signInPath     = "/sign-in"
	choiceName     = "signin_provider"
	choiceLifetime = 30 * 24 * time.Hour
)

// ServeHTTP answers each request: it shows the sign-in page, keeps the
// reader's choice of identity provider in the choice cookie, sends a
// callback or a sign-out to the middleware of its path's provider, with the
// choice cleared at a sign-out, and any other request to the middleware of
// the reader's provider, or without one to the sign-in page.
func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == signInPath:
		rt.signInPage(w, r)
	case strings.HasPrefix(r.URL.Path, signInPath+"/"):
		rt.choose(w, r, strings.TrimPrefix(r.URL.Path, signInPath+"/"))
	default:
		p, ok := rt.providerOf(r)
		if !ok {
			rt.toSignIn(w, r)
			return
		}
		// A cross-site sign-out keeps the session until the reader confirms.
		if r.URL.Path == p.signOutPath && r.Header.Get("Sec-Fetch-Site") != "cross-site" {
			http.SetCookie(w, rt.choiceCookie("", -1))
		}
		p.middleware.ServeHTTP(w, r)
	}
}

// providerOf returns the provider of r: the one with r's path as its
// callback path or sign-out path, else the one named by the choice cookie,
// or false for none.
func (rt *router) providerOf(r *http.Request) (provider, bool) {
	for _, p := range rt.providers {
		if r.URL.Path == p.callbackPath || r.URL.Path == p.signOutPath {
			return p, true
		}
	}
	if c, err := r.Cookie(rt.choiceCookie("", 0).Name); err == nil {
		for _, p := range rt.providers {
			if p.name == c.Value {
				return p, true
			}
		}
	}
	return provider{}, false
}

// toSignIn sends a reader without a provider to the sign-in page, with the
// request as the return target, or refuses a request that cannot follow the
// redirect: one other than GET and HEAD, or one that upgrades its
// connection.
func (rt *router) toSignIn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.Header.Get("Upgrade") != "" {
		http.Error(w, "Sign in first.", http.StatusForbidden)
		return
	}
	http.Redirect(w, r, signInPath+"?return="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
}

// signInPage shows the sign-in page: a form for each provider, which posts
// the choice with the return target of the request's query.
func (rt *router) signInPage(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Return    string
		Providers []struct{ Name, Title string }
	}{Return: returnTarget(r.URL.Query().Get("return"))}
	for _, p := range rt.providers {
		data.Providers = append(data.Providers, struct{ Name, Title string }{p.name, p.title})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	signInTemplate.Execute(w, data)
}

// signInTemplate lays out the sign-in page.
var signInTemplate = template.Must(template.New("sign-in").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Sign in</title></head>
<body><main><h1>Sign in</h1><p>Sign in to read the documentation.</p>
{{range .Providers}}<form method="post" action="/sign-in/{{.Name}}"><input type="hidden" name="return" value="{{$.Return}}"><button type="submit">Sign in with {{.Title}}</button></form>
{{end}}</main></body>
</html>
`))

// choose keeps the reader's choice of the provider of name in the choice
// cookie, and sends the reader to the form's return target, through that
// provider's sign-in. It refuses a choice other than a POST, one from
// another site, and one of no configured provider.
func (rt *router) choose(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Choose a provider on the sign-in page.", http.StatusMethodNotAllowed)
		return
	}
	if err := http.NewCrossOriginProtection().Check(r); err != nil {
		http.Error(w, "Choose a provider on the sign-in page.", http.StatusForbidden)
		return
	}
	if !slices.ContainsFunc(rt.providers, func(p provider) bool { return p.name == name }) {
		http.NotFound(w, r)
		return
	}
	http.SetCookie(w, rt.choiceCookie(name, int(choiceLifetime/time.Second)))
	http.Redirect(w, r, returnTarget(r.PostFormValue("return")), http.StatusSeeOther)
}

// choiceCookie returns the choice cookie with value, which lasts maxAge
// seconds, or which a negative maxAge deletes: HttpOnly, SameSite=Lax, for
// /, and with an https application URL Secure, under the prefix __Host-.
func (rt *router) choiceCookie(value string, maxAge int) *http.Cookie {
	name := choiceName
	if rt.secure {
		name = "__Host-" + name
	}
	return &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: rt.secure, SameSite: http.SameSiteLaxMode}
}

// returnTarget returns target when it is a relative path on the
// application's own origin, such as /portals/pets/?q=1, and / for any
// other: an absolute or protocol-relative URL, a backslash, a control
// character or an unparsable target.
func returnTarget(target string) string {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") ||
		strings.ContainsFunc(target, func(r rune) bool { return r == '\\' || r < ' ' || r == 0x7f }) {
		return "/"
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}
	return target
}
