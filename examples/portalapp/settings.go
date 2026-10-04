package main

import "time"

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
	// HOLE(1): read each variable, else its default, and refuse incomplete settings
	return settings{addr: ":8080"}, nil
}
