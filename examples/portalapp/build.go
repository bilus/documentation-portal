package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/bilus/documentation-portal/anthropicmodel"
	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/signin"
	"github.com/bilus/documentation-portal/source"
)

// The bounds of the snapshots: docportal's defaults.
const (
	maxSize     = 256 << 20 // bytes of a snapshot's objects
	previewIdle = time.Hour // after which an unused preview's snapshot leaves memory
	maxPreviews = 10        // preview snapshots in memory
)

// openBucket opens the bucket folder of the bucket folder URL bucketURL, and
// returns the published documentation, which is the bucket folder without
// its previews location, and the previews location, the folder at the path
// previews inside it.
func openBucket(ctx context.Context, bucketURL, previews string) (source.Source, *source.Bucket, error) {
	bucket, err := source.OpenBucket(ctx, bucketURL, maxSize)
	if err != nil {
		return nil, nil, fmt.Errorf("open the bucket folder %s: %w", bucketURL, err)
	}
	published, err := bucket.Without(previews)
	if err != nil {
		return nil, nil, fmt.Errorf("PORTAL_PREVIEWS: %w", err)
	}
	location, err := bucket.Folder(previews)
	if err != nil {
		return nil, nil, fmt.Errorf("PORTAL_PREVIEWS: %w", err)
	}
	return published, location, nil
}

// builder builds the portal handler of each snapshot of the published
// documentation and of each preview folder, with one chat across the
// snapshots of the published documentation.
type builder struct {
	configPath string       // the configuration file's path inside each snapshot
	rules      accessRules  // whose access method is the access hook
	model      chatSettings // with no chat without an API key
	chat       *chat.Chat   // nil until the first build with an API key
}

// build builds the portal handler of a snapshot at root: it reads the
// portal configuration from the configuration file, and adds the access
// hook of the access rules, the account hook of the sign-in and, with an API
// key, the chat with the reader hook of the sign-in, kept from the last
// snapshot and given its libraries before its routes. A build whose portal
// configuration, libraries or chat fail leaves the chat as it was; after
// them, portal.New refuses nothing that NewLibraries took.
func (b *builder) build(root fs.FS) (http.Handler, error) {
	cfg, err := b.portalConfig(root)
	if err != nil {
		return nil, err
	}
	if b.model.apiKey == "" {
		return portal.New(cfg)
	}
	libs, err := portal.NewLibraries(cfg)
	if err != nil {
		return nil, err
	}
	if b.chat == nil {
		m, err := anthropicmodel.New(b.model.model, anthropicmodel.Config{APIKey: b.model.apiKey, BaseURL: b.model.baseURL})
		if err != nil {
			return nil, err
		}
		c, err := chat.New(chat.Config{Model: m, Libraries: libs, Reader: signin.ReaderID})
		if err != nil {
			return nil, err
		}
		b.chat = c
	} else if err := b.chat.Reload(libs); err != nil {
		return nil, err
	}
	// Routes binds each chat page to the libraries of this snapshot.
	cfg.Chat = b.chat.Routes()
	return portal.New(cfg)
}

// portalConfig reads the portal configuration of the snapshot at root, with
// the access hook of the access rules and the account hook of the sign-in.
func (b *builder) portalConfig(root fs.FS) (portal.Config, error) {
	cfg, err := portal.ReadConfig(root, b.configPath)
	if err != nil {
		return portal.Config{}, err
	}
	cfg.Access, cfg.Account = b.rules.access, signin.AccountLinks
	return cfg, nil
}

// buildPreview builds the portal handler of a preview folder's snapshot at
// root like build, without the chat: it reads the portal configuration from
// the configuration file, and adds the access hook and the account hook.
func (b *builder) buildPreview(root fs.FS) (http.Handler, error) {
	cfg, err := b.portalConfig(root)
	if err != nil {
		return nil, err
	}
	return portal.New(cfg)
}

// withPreviews wraps reloader in the previews handler, which opens the
// preview folders of location, each loaded at its first request with build
// and checked every refresh interval, to the readers whom rules allow.
func withPreviews(ctx context.Context, reloader http.Handler, location *source.Bucket, refresh time.Duration, rules accessRules, build func(fs.FS) (http.Handler, error)) http.Handler {
	if location == nil {
		return reloader
	}
	folder := func(name string) (source.Source, error) {
		f, err := location.Folder(name)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	previews := source.NewPreviews(ctx, folder, build, refresh, previewIdle, maxPreviews)
	return portal.WithPreviews(reloader, portal.PreviewsConfig{Open: previews.Handler, Access: rules.access})
}
