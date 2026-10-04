package main

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/source"
)

// openBucket opens the bucket folder of the bucket folder URL bucketURL, and
// returns the published documentation, which is the bucket folder without
// its previews location, and the previews location, the folder at the path
// previews inside it.
func openBucket(ctx context.Context, bucketURL, previews string) (source.Source, *source.Bucket, error) {
	// HOLE(4): open the bucket folder with the size limit, and split the previews location off it
	dir, err := source.OpenDirectory("demo/docs")
	return dir, nil, err
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
// snapshot. A build that fails leaves the chat as it was.
func (b *builder) build(root fs.FS) (http.Handler, error) {
	// HOLE(4): read the portal configuration, add the hooks and the chat, and build the portal handler
	return http.NotFoundHandler(), nil
}

// buildPreview builds the portal handler of a preview folder's snapshot at
// root like build, without the chat: it reads the portal configuration from
// the configuration file, and adds the access hook and the account hook.
func (b *builder) buildPreview(root fs.FS) (http.Handler, error) {
	// HOLE(4): read the portal configuration, add the hooks, and build the portal handler
	return http.NotFoundHandler(), nil
}

// withPreviews wraps reloader in the previews handler, which opens the
// preview folders of location, each loaded at its first request with build
// and checked every refresh interval, to the readers whom rules allow.
func withPreviews(ctx context.Context, reloader http.Handler, location *source.Bucket, refresh time.Duration, rules accessRules, build func(fs.FS) (http.Handler, error)) http.Handler {
	// HOLE(4): keep the snapshots of the preview folders, and wrap reloader in the previews handler
	return reloader
}
