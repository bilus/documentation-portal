// Command docportal serves the documentation of API specs and markdown files
// from a local directory or a bucket folder, as the portals of a
// configuration file, each a set of sections, and with an issuer signs each
// reader in through an OpenID Connect provider.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	// The drivers of the bucket folder URLs that -root accepts.
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/s3blob"

	"github.com/bilus/documentation-portal/anthropicmodel"
	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/signin"
	"github.com/bilus/documentation-portal/source"
)

type config struct {
	Addr        string
	ConfigName  string        // the configuration file: a local path, or with Root a path inside the bucket folder
	Root        string        // the bucket folder's URL, or empty for the directory of ConfigName
	Archive     string        // the key of the archive inside the bucket folder that holds the documentation root, or empty for the folder's objects
	Refresh     time.Duration // between checks of the bucket folder; 0 turns them off
	MaxSize     int64         // of a bucket folder's objects, in bytes
	Previews    string        // the previews location inside the bucket folder, or empty without previews
	PreviewIdle time.Duration // after which an unused preview's snapshot leaves memory; 0 never
	MaxPreviews int           // preview snapshots in memory; 0 means no limit
	HideTryIt   bool
	ChatModel   string       // empty without a chat
	SignIn      signInConfig // the sign-in settings; without an issuer, readers do not sign in
}

// signInConfig holds the sign-in settings: the OpenID Connect provider of
// the readers' sign-in, and their sessions.
type signInConfig struct {
	Issuer       string // the provider's issuer URL; empty for no sign-in
	ClientID     string
	ClientSecret string // from the environment alone
	CallbackURL  string
	LogoutURL    string        // empty for the signed-out page
	Scopes       string        // separated by commas or spaces; empty for openid, profile and email
	Audience     string        // the authorization request's audience parameter, or empty
	Lifetime     time.Duration // of a session; 0 means 8 hours
	Key          string        // seals the session cookie; from the environment alone, and empty for a random key
}

func main() {
	addr, h, err := startup(context.Background(), os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docportal:", err)
		os.Exit(1)
	}
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

// startup reads the configuration, opens the documentation source, loads its
// snapshot, builds the portal handler from it, wraps it in the reloader,
// which rebuilds it for each settled change of the source until the end of
// ctx, opens the previews location, wraps the reloader in the previews
// handler, makes the sign-in configuration of the sign-in settings, and with
// an issuer wraps the previews handler in the sign-in middleware. It returns
// the address to listen on and the handler: the previews handler, behind the
// sign-in middleware with an issuer.
func startup(ctx context.Context, args []string, getenv func(string) string) (string, http.Handler, error) {
	cfg, err := parseConfig(args, getenv)
	if err != nil {
		return "", nil, err
	}
	src, configPath, err := openSource(ctx, cfg)
	if err != nil {
		return "", nil, err
	}
	snap, err := source.Load(ctx, src)
	if err != nil {
		return "", nil, err
	}
	b := &builder{cfg: cfg, configPath: configPath}
	h, err := b.build(snap.Root)
	if err != nil {
		return "", nil, err
	}
	reloader := source.NewReloader(ctx, src, snap.Listing, h, cfg.Refresh, b.build)
	previews, err := openPreviews(ctx, cfg, b.buildPreview)
	if err != nil {
		return "", nil, err
	}
	h, err = signIn(ctx, signInConfigOf(cfg.SignIn), portal.WithPreviews(reloader, previews))
	if err != nil {
		return "", nil, err
	}
	return cfg.Addr, h, nil
}

// builder builds the portal handler of each snapshot of the documentation
// source, with one chat across the snapshots.
type builder struct {
	cfg        config
	configPath string     // the configuration file's path inside the snapshot
	chat       *chat.Chat // nil until the first build with a chat model
}

// build builds the portal handler of the snapshot at root: it reads the
// portal configuration from the configuration file, adds the Try It setting,
// the sign-in's account hook and the chat, and builds the handler, as
// startup's boxes do for the first snapshot. A build that fails leaves the
// chat as it was.
func (b *builder) build(root fs.FS) (http.Handler, error) {
	if b.cfg.Archive != "" {
		// Before the first upload the archive is absent and its root empty:
		// the portal starts and says so, and a check brings the upload.
		if _, err := fs.Stat(root, b.configPath); errors.Is(err, fs.ErrNotExist) {
			return http.HandlerFunc(unpublished), nil
		}
	}
	pcfg, err := portal.ReadConfig(root, b.configPath)
	if err != nil {
		return nil, err
	}
	pcfg = setTryIt(pcfg, b.cfg.HideTryIt)
	pcfg = addAccount(pcfg, b.cfg.SignIn.Issuer)
	pcfg, b.chat, err = addChat(pcfg, b.cfg.ChatModel, b.chat, b.cfg.SignIn.Issuer)
	if err != nil {
		return nil, err
	}
	return portal.New(pcfg)
}

// unpublished answers every request of an archive root that holds no
// documentation yet: a page saying so, sent uncacheable, since the next
// check may bring the documentation.
func unpublished(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("<!doctype html><title>Documentation</title><h1>No documentation yet</h1><p>The documentation has not been published. Try again in a few minutes.</p>"))
}

// buildPreview builds the portal handler of a preview folder's snapshot at
// root like build, without the chat: it reads the portal configuration from
// the configuration file, adds the Try It setting and the sign-in's account
// hook, and builds the handler.
func (b *builder) buildPreview(root fs.FS) (http.Handler, error) {
	pcfg, err := portal.ReadConfig(root, b.configPath)
	if err != nil {
		return nil, err
	}
	pcfg = setTryIt(pcfg, b.cfg.HideTryIt)
	return portal.New(addAccount(pcfg, b.cfg.SignIn.Issuer))
}

// parseConfig reads the configuration from the flags and the environment.
// Flags win over the environment, which wins over the defaults.
func parseConfig(args []string, getenv func(string) string) (config, error) {
	cfg := config{Addr: ":8080", ConfigName: "environment.yaml", Refresh: time.Minute, MaxSize: 256 << 20, PreviewIdle: time.Hour, MaxPreviews: 10}
	if v := getenv("DOCPORTAL_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("DOCPORTAL_CONFIG"); v != "" {
		cfg.ConfigName = v
	}
	if v := getenv("DOCPORTAL_ROOT"); v != "" {
		cfg.Root = v
	}
	if v := getenv("DOCPORTAL_ARCHIVE"); v != "" {
		cfg.Archive = v
	}
	if v := getenv("DOCPORTAL_REFRESH"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("DOCPORTAL_REFRESH: %q is not a duration", v)
		}
		cfg.Refresh = d
	}
	maxSizeMiB := cfg.MaxSize >> 20
	if v := getenv("DOCPORTAL_MAX_SIZE"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return config{}, fmt.Errorf("DOCPORTAL_MAX_SIZE: %q is not a number of MiB", v)
		}
		maxSizeMiB = n
	}
	if v := getenv("DOCPORTAL_CHAT_MODEL"); v != "" {
		cfg.ChatModel = v
	}
	if v := getenv("DOCPORTAL_HIDE_TRY_IT"); v != "" {
		hide, err := strconv.ParseBool(v)
		if err != nil {
			return config{}, fmt.Errorf("DOCPORTAL_HIDE_TRY_IT: %q is not a boolean", v)
		}
		cfg.HideTryIt = hide
	}
	if v := getenv("DOCPORTAL_PREVIEWS"); v != "" {
		cfg.Previews = v
	}
	if v := getenv("DOCPORTAL_PREVIEW_IDLE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("DOCPORTAL_PREVIEW_IDLE: %q is not a duration", v)
		}
		cfg.PreviewIdle = d
	}
	if v := getenv("DOCPORTAL_MAX_PREVIEWS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return config{}, fmt.Errorf("DOCPORTAL_MAX_PREVIEWS: %q is not a number", v)
		}
		cfg.MaxPreviews = n
	}
	// The client secret and the session key come from the environment alone.
	for name, setting := range map[string]*string{
		"DOCPORTAL_OIDC_ISSUER":        &cfg.SignIn.Issuer,
		"DOCPORTAL_OIDC_CLIENT_ID":     &cfg.SignIn.ClientID,
		"DOCPORTAL_OIDC_CLIENT_SECRET": &cfg.SignIn.ClientSecret,
		"DOCPORTAL_OIDC_CALLBACK_URL":  &cfg.SignIn.CallbackURL,
		"DOCPORTAL_OIDC_LOGOUT_URL":    &cfg.SignIn.LogoutURL,
		"DOCPORTAL_OIDC_SCOPES":        &cfg.SignIn.Scopes,
		"DOCPORTAL_OIDC_AUDIENCE":      &cfg.SignIn.Audience,
		"DOCPORTAL_SESSION_KEY":        &cfg.SignIn.Key,
	} {
		*setting = getenv(name)
	}
	if v := getenv("DOCPORTAL_SESSION_LIFETIME"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("DOCPORTAL_SESSION_LIFETIME: %q is not a duration", v)
		}
		cfg.SignIn.Lifetime = d
	}

	// The flag package's message and usage go into the error, which main prints.
	var out strings.Builder
	flags := flag.NewFlagSet("docportal", flag.ContinueOnError)
	flags.SetOutput(&out)
	flags.StringVar(&cfg.Addr, "addr", cfg.Addr, "address to listen on")
	flags.StringVar(&cfg.ConfigName, "config", cfg.ConfigName, "configuration file, which lists the portals and their sections; its directory is the documentation root, or with -root its path inside the bucket folder")
	flags.StringVar(&cfg.Root, "root", cfg.Root, "bucket folder that holds the documentation, as a Go CDK URL such as gs://bucket?prefix=docs/; without it, the directory of -config")
	flags.StringVar(&cfg.Archive, "archive", cfg.Archive, "gzip-compressed tar archive inside the bucket folder that holds the documentation root, such as published.tgz; without it, the folder's objects are the root")
	flags.DurationVar(&cfg.Refresh, "refresh", cfg.Refresh, "how often to check the bucket folder for changes; 0 never")
	flags.Int64Var(&maxSizeMiB, "max-size", maxSizeMiB, "largest total size of the bucket folder's objects, in MiB; 0 means no limit")
	flags.StringVar(&cfg.Previews, "previews", cfg.Previews, "previews location: a folder of the bucket folder, such as previews/, with a preview folder for each preview at /previews/{folder}, or with -archive an archive {folder}.tgz; none turns previews off")
	flags.DurationVar(&cfg.PreviewIdle, "preview-idle", cfg.PreviewIdle, "how long an unused preview's snapshot stays in memory; 0 means no limit")
	flags.IntVar(&cfg.MaxPreviews, "max-previews", cfg.MaxPreviews, "most preview snapshots in memory; 0 means no limit")
	flags.BoolVar(&cfg.HideTryIt, "hide-try-it", cfg.HideTryIt, "hide the Try It console of the viewer page")
	flags.StringVar(&cfg.ChatModel, "chat-model", cfg.ChatModel, "Anthropic model of the chat page, such as claude-opus-5-5; none disables the chat")
	flags.StringVar(&cfg.SignIn.Issuer, "oidc-issuer", cfg.SignIn.Issuer, "issuer URL of the OpenID Connect provider for the readers' sign-in, such as https://TENANT.auth0.com/; none serves every reader without a sign-in")
	flags.StringVar(&cfg.SignIn.ClientID, "oidc-client-id", cfg.SignIn.ClientID, "the portal's client ID at the provider; DOCPORTAL_OIDC_CLIENT_SECRET holds its secret")
	flags.StringVar(&cfg.SignIn.CallbackURL, "oidc-callback-url", cfg.SignIn.CallbackURL, "the portal's URL for the provider's answer, such as https://docs.example.com/auth/callback")
	flags.StringVar(&cfg.SignIn.LogoutURL, "oidc-logout-url", cfg.SignIn.LogoutURL, "the reader's destination after the sign-out, such as the provider's logout endpoint; none for the signed-out page")
	flags.StringVar(&cfg.SignIn.Scopes, "oidc-scopes", cfg.SignIn.Scopes, "scopes of the sign-in, separated by commas; none for openid, profile and email")
	flags.StringVar(&cfg.SignIn.Audience, "oidc-audience", cfg.SignIn.Audience, "audience parameter of the sign-in, such as the identifier of an Auth0 API")
	flags.DurationVar(&cfg.SignIn.Lifetime, "session-lifetime", cfg.SignIn.Lifetime, "lifetime of a signed-in reader's session; 0 means 8 hours")
	if err := flags.Parse(args); err != nil {
		return config{}, errors.New(strings.TrimSpace(out.String()))
	}
	if flags.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if maxSizeMiB < 0 || maxSizeMiB > math.MaxInt64>>20 {
		return config{}, fmt.Errorf("-max-size: %d MiB is not a size between 0 and %d", maxSizeMiB, math.MaxInt64>>20)
	}
	switch {
	case cfg.Archive != "" && cfg.Root == "":
		return config{}, errors.New("-archive needs -root: the archive is an object of the bucket folder")
	case cfg.Archive != "" && !fs.ValidPath(cfg.Archive):
		return config{}, fmt.Errorf("-archive: %q is not a valid path inside the bucket folder", cfg.Archive)
	case cfg.Previews != "" && cfg.Root == "":
		return config{}, errors.New("-previews needs -root: previews are folders of the bucket folder")
	case cfg.PreviewIdle < 0:
		return config{}, fmt.Errorf("-preview-idle: %v is negative", cfg.PreviewIdle)
	case cfg.MaxPreviews < 0:
		return config{}, fmt.Errorf("-max-previews: %d is negative", cfg.MaxPreviews)
	}
	cfg.MaxSize = maxSizeMiB << 20
	if cfg.SignIn.Issuer == "" && cfg.SignIn != (signInConfig{}) {
		return config{}, errors.New("sign-in settings without -oidc-issuer (DOCPORTAL_OIDC_ISSUER): name the issuer, or drop the settings")
	}
	if cfg.SignIn.Lifetime < 0 {
		return config{}, fmt.Errorf("-session-lifetime: %v is negative", cfg.SignIn.Lifetime)
	}
	return cfg, nil
}

// openSource opens the documentation source of cfg: the directory of the
// configuration file, so that the portal cannot read outside it, or the
// bucket folder that cfg.Root names, with cfg's size limit, without its
// previews location. It returns the source with the configuration file's
// path inside it. A file:// bucket folder, unlike the directory, reads
// through a symlink to a file outside it, as the file driver does.
func openSource(ctx context.Context, cfg config) (source.Source, string, error) {
	if cfg.Archive != "" {
		archive, err := source.OpenArchive(ctx, cfg.Root, cfg.Archive, cfg.MaxSize)
		if err != nil {
			return nil, "", fmt.Errorf("open bucket folder %s: %w", cfg.Root, err)
		}
		return archive, cfg.ConfigName, nil
	}
	if cfg.Root != "" {
		bucket, err := source.OpenBucket(ctx, cfg.Root, cfg.MaxSize)
		if err != nil {
			return nil, "", fmt.Errorf("open bucket folder %s: %w", cfg.Root, err)
		}
		if cfg.Previews == "" {
			return bucket, cfg.ConfigName, nil
		}
		published, err := bucket.Without(cfg.Previews)
		if err != nil {
			return nil, "", fmt.Errorf("-previews: %w", err)
		}
		return published, cfg.ConfigName, nil
	}
	// The root stays open for as long as docportal runs.
	dir, err := source.OpenDirectory(filepath.Dir(cfg.ConfigName))
	if err != nil {
		return nil, "", fmt.Errorf("open documentation root: %w", err)
	}
	return dir, filepath.Base(cfg.ConfigName), nil
}

// openPreviews opens the previews location named by cfg.Previews, if any,
// into the previews configuration, so that the previews handler opens the
// portal handler of each preview folder, loaded at the folder's first
// request with build. Without a previews location, previews are off.
func openPreviews(ctx context.Context, cfg config, build func(fs.FS) (http.Handler, error)) (portal.PreviewsConfig, error) {
	if cfg.Previews == "" {
		return portal.PreviewsConfig{}, nil
	}
	if cfg.Archive != "" {
		return openArchivePreviews(ctx, cfg, build)
	}
	bucket, err := source.OpenBucket(ctx, cfg.Root, cfg.MaxSize)
	if err != nil {
		return portal.PreviewsConfig{}, fmt.Errorf("open bucket folder %s: %w", cfg.Root, err)
	}
	location, err := bucket.Folder(cfg.Previews)
	if err != nil {
		return portal.PreviewsConfig{}, fmt.Errorf("-previews: %w", err)
	}
	folder := func(name string) (source.Source, error) {
		f, err := location.Folder(name)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	previews := source.NewPreviews(ctx, folder, build, cfg.Refresh, cfg.PreviewIdle, cfg.MaxPreviews)
	return portal.PreviewsConfig{Open: previews.Handler}, nil
}

// openArchivePreviews is openPreviews for archives: the preview named name
// is the archive at {previews}/{name}.tgz inside the bucket folder.
func openArchivePreviews(ctx context.Context, cfg config, build func(fs.FS) (http.Handler, error)) (portal.PreviewsConfig, error) {
	location := strings.TrimSuffix(cfg.Previews, "/")
	if location == "." || !fs.ValidPath(location) {
		return portal.PreviewsConfig{}, fmt.Errorf("-previews: the folder %q is not a valid path inside the bucket folder", cfg.Previews)
	}
	archive, err := source.OpenArchive(ctx, cfg.Root, cfg.Archive, cfg.MaxSize)
	if err != nil {
		return portal.PreviewsConfig{}, fmt.Errorf("open bucket folder %s: %w", cfg.Root, err)
	}
	folder := func(name string) (source.Source, error) {
		return archive.Object(location + "/" + name + ".tgz"), nil
	}
	previews := source.NewPreviews(ctx, folder, build, cfg.Refresh, cfg.PreviewIdle, cfg.MaxPreviews)
	return portal.PreviewsConfig{Open: previews.Handler}, nil
}

// setTryIt adds the Try It setting to the portal configuration.
func setTryIt(pcfg portal.Config, hide bool) portal.Config {
	pcfg.HideTryIt = hide
	return pcfg
}

// addAccount adds the account hook of the sign-in to the portal
// configuration when issuer is not empty, so that every page ends with the
// reader's name and a sign-out link.
func addAccount(pcfg portal.Config, issuer string) portal.Config {
	if issuer != "" {
		pcfg.Account = signin.AccountLinks
	}
	return pcfg
}

// addChat adds the chat's routes to the portal configuration when modelID
// names a model, so that the portal handler serves a chat page in every
// portal. It builds the chat for the first snapshot, when c is nil, with the
// reader hook of the sign-in when issuer is not empty, so that a signed-in
// reader's questions count against one limit, and gives an existing chat the
// new snapshot's libraries, so that the chat keeps its conversations across
// snapshots. It returns the chat for the next snapshot: the one it built or
// was given, and on a failure the one it was given, so that a snapshot that
// fails never costs the chat its conversations.
func addChat(pcfg portal.Config, modelID string, c *chat.Chat, issuer string) (portal.Config, *chat.Chat, error) {
	if modelID == "" {
		return pcfg, nil, nil
	}
	libs, err := portal.NewLibraries(pcfg)
	if err != nil {
		return portal.Config{}, c, err
	}
	if c == nil {
		m, err := anthropicmodel.New(modelID, anthropicmodel.Config{})
		if err != nil {
			return portal.Config{}, nil, err
		}
		var reader func(*http.Request) string
		if issuer != "" {
			reader = signin.ReaderID
		}
		if c, err = chat.New(chat.Config{Model: m, Libraries: libs, Reader: reader}); err != nil {
			return portal.Config{}, nil, err
		}
	} else if err := c.Reload(libs); err != nil {
		return portal.Config{}, c, err
	}
	pcfg.Chat = c.Routes()
	return pcfg, c, nil
}

// signInConfigOf makes the sign-in configuration of the sign-in settings s:
// none without an issuer, and with one a random session key, with a notice
// in the log, when s holds none, so that the sessions last until
// docportal's exit.
func signInConfigOf(s signInConfig) signin.Config {
	if s.Issuer == "" {
		return signin.Config{}
	}
	key := []byte(s.Key)
	if len(key) == 0 {
		key = make([]byte, 32)
		rand.Read(key)
		log.Print("DOCPORTAL_SESSION_KEY is empty: the sessions last until docportal's exit")
	}
	return signin.Config{
		Issuer:       s.Issuer,
		ClientID:     s.ClientID,
		ClientSecret: s.ClientSecret,
		CallbackURL:  s.CallbackURL,
		LogoutURL:    s.LogoutURL,
		Scopes:       strings.FieldsFunc(s.Scopes, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }),
		Audience:     s.Audience,
		Lifetime:     s.Lifetime,
		Key:          key,
	}
}

// signIn wraps h, the previews handler, in the sign-in middleware of cfg
// when cfg names an issuer: the middleware signs each reader in through the
// identity provider and puts the reader's identity into each request's
// context, so that the request hooks read it. Without an issuer, it returns
// h unchanged.
func signIn(ctx context.Context, cfg signin.Config, h http.Handler) (http.Handler, error) {
	if cfg.Issuer == "" {
		return h, nil
	}
	return signin.New(ctx, cfg, h)
}
