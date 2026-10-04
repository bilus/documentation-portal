package main

import (
	"maps"
	"strings"
	"testing"
	"time"
)

// someSettings are the environment of complete settings with Auth0 alone.
var someSettings = map[string]string{
	"PORTAL_URL":          "https://docs.example.com",
	"PORTAL_BUCKET":       "gs://docs-bucket?prefix=portal/",
	"PORTAL_SESSION_KEY":  testKey,
	"AUTH0_ISSUER":        "https://tenant.auth0.com/",
	"AUTH0_CLIENT_ID":     "the Auth0 client",
	"AUTH0_CLIENT_SECRET": "the Auth0 secret",
}

// lookup returns a lookup of the environment env.
func lookup(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

func TestReadSettings(t *testing.T) {
	s, err := readSettings(lookup(someSettings))
	if err != nil {
		t.Fatal(err)
	}
	want := settings{
		addr:       ":8080",
		bucketURL:  "gs://docs-bucket?prefix=portal/",
		configPath: "environment.yaml",
		previews:   "previews",
		refresh:    time.Minute,
		accessFile: "access.yaml",
		providers: providerSettings{
			appURL:     "https://docs.example.com",
			sessionKey: testKey,
			auth0:      auth0Settings{issuer: "https://tenant.auth0.com/", clientID: "the Auth0 client", clientSecret: "the Auth0 secret"},
			github:     githubSettings{apiURL: "https://api.github.com"},
		},
		chat: chatSettings{model: "claude-opus-5-5"},
	}
	if s != want {
		t.Errorf("the defaults:\n got %+v\nwant %+v", s, want)
	}

	every := maps.Clone(someSettings)
	maps.Copy(every, map[string]string{
		"PORTAL_ADDR":          "127.0.0.1:9000",
		"PORTAL_URL":           "http://localhost:9000/",
		"PORTAL_CONFIG":        "portals.yaml",
		"PORTAL_PREVIEWS":      "pulls/",
		"PORTAL_REFRESH":       "30s",
		"PORTAL_ACCESS_FILE":   "/etc/portal/access.yaml",
		"PORTAL_CHAT_MODEL":    "claude-sonnet-5",
		"AUTH0_LOGOUT_URL":     "https://tenant.auth0.com/v2/logout?client_id=the+Auth0+client",
		"GITHUB_CLIENT_ID":     "the GitHub client",
		"GITHUB_CLIENT_SECRET": "the GitHub secret",
		"GITHUB_URL":           "http://127.0.0.1:9200",
		"GITHUB_API_URL":       "http://127.0.0.1:9200/api/",
		"ANTHROPIC_API_KEY":    "the API key",
		"ANTHROPIC_BASE_URL":   "http://127.0.0.1:9300",
	})
	s, err = readSettings(lookup(every))
	if err != nil {
		t.Fatal(err)
	}
	want = settings{
		addr:       "127.0.0.1:9000",
		bucketURL:  "gs://docs-bucket?prefix=portal/",
		configPath: "portals.yaml",
		previews:   "pulls/",
		refresh:    30 * time.Second,
		accessFile: "/etc/portal/access.yaml",
		providers: providerSettings{
			appURL:     "http://localhost:9000",
			sessionKey: testKey,
			auth0: auth0Settings{issuer: "https://tenant.auth0.com/", clientID: "the Auth0 client", clientSecret: "the Auth0 secret",
				logoutURL: "https://tenant.auth0.com/v2/logout?client_id=the+Auth0+client"},
			github: githubSettings{clientID: "the GitHub client", clientSecret: "the GitHub secret",
				webURL: "http://127.0.0.1:9200", apiURL: "http://127.0.0.1:9200/api"},
		},
		chat: chatSettings{apiKey: "the API key", model: "claude-sonnet-5", baseURL: "http://127.0.0.1:9300"},
	}
	if s != want {
		t.Errorf("every setting:\n got %+v\nwant %+v", s, want)
	}

	// GitHub alone, and a refresh interval of 0, which turns the checks off.
	github := map[string]string{
		"PORTAL_URL": "https://docs.example.com", "PORTAL_BUCKET": "file:///srv/docs", "PORTAL_SESSION_KEY": testKey,
		"PORTAL_REFRESH": "0", "GITHUB_CLIENT_ID": "the GitHub client", "GITHUB_CLIENT_SECRET": "the GitHub secret",
	}
	s, err = readSettings(lookup(github))
	if err != nil {
		t.Fatal(err)
	}
	if s.providers.auth0 != (auth0Settings{}) || s.providers.github.clientID != "the GitHub client" || s.refresh != 0 {
		t.Errorf("GitHub alone: %+v", s)
	}
}

func TestReadSettingsRefusesIncompleteSettings(t *testing.T) {
	for name, tc := range map[string]struct {
		change map[string]string // "" deletes the variable
		want   string            // in the error
	}{
		"no portal URL":                       {map[string]string{"PORTAL_URL": ""}, "PORTAL_URL"},
		"a portal URL without a scheme":       {map[string]string{"PORTAL_URL": "docs.example.com"}, "PORTAL_URL"},
		"a portal URL of another scheme":      {map[string]string{"PORTAL_URL": "ftp://docs.example.com"}, "PORTAL_URL"},
		"a portal URL with a path":            {map[string]string{"PORTAL_URL": "https://example.com/docs"}, "PORTAL_URL"},
		"a portal URL with a query":           {map[string]string{"PORTAL_URL": "https://docs.example.com/?a=b"}, "PORTAL_URL"},
		"a portal URL with a fragment":        {map[string]string{"PORTAL_URL": "https://docs.example.com/#top"}, "PORTAL_URL"},
		"a portal URL with a user":            {map[string]string{"PORTAL_URL": "https://ada@docs.example.com"}, "PORTAL_URL"},
		"no bucket folder":                    {map[string]string{"PORTAL_BUCKET": ""}, "PORTAL_BUCKET"},
		"no session key":                      {map[string]string{"PORTAL_SESSION_KEY": ""}, "PORTAL_SESSION_KEY"},
		"a short session key":                 {map[string]string{"PORTAL_SESSION_KEY": "31 bytes of session key, no more"[:31]}, "PORTAL_SESSION_KEY"},
		"a refresh interval of no duration":   {map[string]string{"PORTAL_REFRESH": "soon"}, "PORTAL_REFRESH"},
		"a negative refresh interval":         {map[string]string{"PORTAL_REFRESH": "-1m"}, "PORTAL_REFRESH"},
		"no identity provider":                {map[string]string{"AUTH0_ISSUER": "", "AUTH0_CLIENT_ID": "", "AUTH0_CLIENT_SECRET": ""}, "AUTH0_CLIENT_ID"},
		"Auth0 without an issuer":             {map[string]string{"AUTH0_ISSUER": ""}, "AUTH0_ISSUER"},
		"Auth0 without a secret":              {map[string]string{"AUTH0_CLIENT_SECRET": ""}, "AUTH0_CLIENT_SECRET"},
		"an Auth0 setting without its client": {map[string]string{"AUTH0_CLIENT_ID": "", "GITHUB_CLIENT_ID": "c", "GITHUB_CLIENT_SECRET": "s"}, "AUTH0_CLIENT_ID"},
		"a logout URL without Auth0":          {map[string]string{"AUTH0_ISSUER": "", "AUTH0_CLIENT_ID": "", "AUTH0_CLIENT_SECRET": "", "AUTH0_LOGOUT_URL": "/", "GITHUB_CLIENT_ID": "c", "GITHUB_CLIENT_SECRET": "s"}, "AUTH0_CLIENT_ID"},
		"GitHub without a secret":             {map[string]string{"GITHUB_CLIENT_ID": "c"}, "GITHUB_CLIENT_SECRET"},
		"a GitHub secret without its client":  {map[string]string{"GITHUB_CLIENT_SECRET": "s"}, "GITHUB_CLIENT_ID"},
		"a GitHub URL without GitHub":         {map[string]string{"GITHUB_URL": "http://127.0.0.1:9200"}, "GITHUB_CLIENT_ID"},
		"a GitHub URL that is no URL":         {map[string]string{"GITHUB_CLIENT_ID": "c", "GITHUB_CLIENT_SECRET": "s", "GITHUB_URL": "github"}, "GITHUB_URL"},
		"a GitHub API URL that is no URL":     {map[string]string{"GITHUB_CLIENT_ID": "c", "GITHUB_CLIENT_SECRET": "s", "GITHUB_API_URL": "api"}, "GITHUB_API_URL"},
	} {
		env := maps.Clone(someSettings)
		for k, v := range tc.change {
			if v == "" {
				delete(env, k)
			} else {
				env[k] = v
			}
		}
		_, err := readSettings(lookup(env))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: the error %v does not name %s", name, err, tc.want)
			continue
		}
		for _, secret := range []string{testKey, "the Auth0 secret"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the error %q shows a secret", name, err)
			}
		}
	}
}
