package main

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// settings are the example application's settings, from the environment
// alone. The README lists each variable with its default.
type settings struct {
	addr       string        // PORTAL_ADDR
	bucketURL  string        // PORTAL_BUCKET, the bucket folder's Go CDK URL
	configPath string        // PORTAL_CONFIG, the configuration file's path inside the bucket folder
	previews   string        // PORTAL_PREVIEWS, the previews location's path inside the bucket folder
	refresh    time.Duration // PORTAL_REFRESH, between checks of the bucket folder; 0 never
	accessFile string        // PORTAL_ACCESS_FILE, the access file's path
	providers  providerSettings
	chat       chatSettings
}

// providerSettings are the settings of the sign-in: the application URL,
// the session key, and the identity providers.
type providerSettings struct {
	appURL     string // PORTAL_URL, the application's origin, such as https://docs.example.com
	sessionKey string // PORTAL_SESSION_KEY, 32 bytes or more
	auth0      auth0Settings
	github     githubSettings
}

// auth0Settings are Auth0's settings, all empty when Auth0 is off.
type auth0Settings struct {
	issuer       string // AUTH0_ISSUER, such as https://TENANT.auth0.com/
	clientID     string // AUTH0_CLIENT_ID
	clientSecret string // AUTH0_CLIENT_SECRET
	logoutURL    string // AUTH0_LOGOUT_URL, or empty for the signed-out page
}

// githubSettings are GitHub's settings, with an empty client ID when GitHub
// is off.
type githubSettings struct {
	clientID     string // GITHUB_CLIENT_ID
	clientSecret string // GITHUB_CLIENT_SECRET
	webURL       string // GITHUB_URL, the host of the OAuth endpoints; empty for github.com
	apiURL       string // GITHUB_API_URL
}

// chatSettings are the chat's settings, with an empty API key when the chat
// is off.
type chatSettings struct {
	apiKey  string // ANTHROPIC_API_KEY
	model   string // PORTAL_CHAT_MODEL
	baseURL string // ANTHROPIC_BASE_URL, or empty for Anthropic's own
}

// readSettings reads the settings from the environment: the address, the
// bucket folder, the previews location, the refresh interval, the access
// file, the provider settings and the chat, or refuses incomplete settings.
// It refuses a missing application URL or one other than an http or https
// origin, a missing bucket folder URL, a session key under 32 bytes, a
// refresh interval that is no duration or a negative one, settings without
// an identity provider, and a provider with some of its settings missing. A
// provider is on when its client ID is set, and the chat when the API key
// is.
func readSettings(getenv func(string) string) (settings, error) {
	or := func(name, fallback string) string { return cmp.Or(getenv(name), fallback) }
	s := settings{
		addr:       or("PORTAL_ADDR", ":8080"),
		bucketURL:  getenv("PORTAL_BUCKET"),
		configPath: or("PORTAL_CONFIG", "environment.yaml"),
		previews:   or("PORTAL_PREVIEWS", "previews"),
		refresh:    time.Minute,
		accessFile: or("PORTAL_ACCESS_FILE", "access.yaml"),
		providers: providerSettings{
			appURL:     strings.TrimSuffix(getenv("PORTAL_URL"), "/"),
			sessionKey: getenv("PORTAL_SESSION_KEY"),
			auth0: auth0Settings{
				issuer:       getenv("AUTH0_ISSUER"),
				clientID:     getenv("AUTH0_CLIENT_ID"),
				clientSecret: getenv("AUTH0_CLIENT_SECRET"),
				logoutURL:    getenv("AUTH0_LOGOUT_URL"),
			},
			github: githubSettings{
				clientID:     getenv("GITHUB_CLIENT_ID"),
				clientSecret: getenv("GITHUB_CLIENT_SECRET"),
				webURL:       strings.TrimSuffix(getenv("GITHUB_URL"), "/"),
				apiURL:       strings.TrimSuffix(or("GITHUB_API_URL", gitHubAPI), "/"),
			},
		},
		chat: chatSettings{
			apiKey:  getenv("ANTHROPIC_API_KEY"),
			model:   or("PORTAL_CHAT_MODEL", "claude-opus-5-5"),
			baseURL: getenv("ANTHROPIC_BASE_URL"),
		},
	}
	if v := getenv("PORTAL_REFRESH"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return settings{}, fmt.Errorf("PORTAL_REFRESH: %q is not a duration of 0 or more", v)
		}
		s.refresh = d
	}
	switch {
	case !webURL(s.providers.appURL, false):
		return settings{}, fmt.Errorf("PORTAL_URL: %q is not the application's origin, an http or https URL such as https://docs.example.com", getenv("PORTAL_URL"))
	case s.bucketURL == "":
		return settings{}, errors.New("PORTAL_BUCKET is empty: name the bucket folder, such as gs://docs-bucket?prefix=portal/")
	case len(s.providers.sessionKey) < 32:
		return settings{}, errors.New("PORTAL_SESSION_KEY holds fewer than 32 bytes")
	}
	if err := s.providers.check(); err != nil {
		return settings{}, err
	}
	return s, nil
}

// gitHubAPI is the URL of GitHub's API.
const gitHubAPI = "https://api.github.com"

// check refuses provider settings without an identity provider, and a
// provider with some of its settings missing, by the first missing one's
// variable. A provider is on with any of its settings, GitHub's API URL
// other than GitHub's own included.
func (s providerSettings) check() error {
	auth0 := s.auth0 != auth0Settings{}
	github := s.github != githubSettings{apiURL: gitHubAPI}
	switch {
	case !auth0 && !github:
		return errors.New("no identity provider: set AUTH0_CLIENT_ID or GITHUB_CLIENT_ID with the provider's other settings")
	case auth0 && s.auth0.clientID == "":
		return errors.New("AUTH0_CLIENT_ID is empty, beside other Auth0 settings")
	case auth0 && s.auth0.issuer == "":
		return errors.New("AUTH0_ISSUER is empty, beside AUTH0_CLIENT_ID")
	case auth0 && s.auth0.clientSecret == "":
		return errors.New("AUTH0_CLIENT_SECRET is empty, beside AUTH0_CLIENT_ID")
	case github && s.github.clientID == "":
		return errors.New("GITHUB_CLIENT_ID is empty, beside other GitHub settings")
	case github && s.github.clientSecret == "":
		return errors.New("GITHUB_CLIENT_SECRET is empty, beside GITHUB_CLIENT_ID")
	case github && s.github.webURL != "" && !webURL(s.github.webURL, true):
		return fmt.Errorf("GITHUB_URL: %q is not an http or https URL", s.github.webURL)
	case github && !webURL(s.github.apiURL, true):
		return fmt.Errorf("GITHUB_API_URL: %q is not an http or https URL", s.github.apiURL)
	}
	return nil
}

// webURL reports whether raw is an absolute http or https URL with a host,
// and without a user, a query or a fragment, and with a path only for
// withPath.
func webURL(raw string, withPath bool) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil &&
		u.RawQuery == "" && !u.ForceQuery && !strings.Contains(raw, "#") && (withPath || u.Path == "")
}
