package source

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"time"
)

// Folder returns the folder at the path name inside b, such as previews or
// previews/pr-123, as a bucket folder of its own, with b's size limit: its
// keys are b's keys under name, without name and its slash, and it leaves
// out the folders left out of b. It refuses a name other than a valid io/fs
// path, after one trailing slash, and the name ".".
func (b *Bucket) Folder(name string) (*Bucket, error) {
	// HOLE(2): a key prefix of the folder's own
	return b, nil
}

// Without returns b with a listing that leaves out the folder at the path
// name inside b, such as the previews location: every key under name and its
// slash. It refuses the names refused by Folder.
func (b *Bucket) Without(name string) (*Bucket, error) {
	// HOLE(2): a folder for the listing to leave out
	return b, nil
}

// Previews holds the preview snapshots: the snapshot of each preview folder
// in memory, with its portal handler and a reloader of its own.
type Previews struct{}

// NewPreviews returns the preview snapshots of the folders from folder, by
// name, such as the folders of the previews location, with portal handlers
// from build, refreshed every interval until ctx ends, like the snapshots of
// a reloader. A snapshot leaves memory after idle without a request, as the
// least recently used one beyond maxSnapshots, the preview limit, after a
// load that succeeds, and at a check that finds its folder empty. An idle
// time of zero or less keeps an unused snapshot, and a maxSnapshots of zero
// or less keeps any number.
func NewPreviews(ctx context.Context, folder func(name string) (Source, error), build func(fs.FS) (http.Handler, error), interval, idle time.Duration, maxSnapshots int) *Previews {
	// HOLE(2): keep the arguments for Handler
	return &Previews{}
}

// Handler returns the portal handler of the preview folder name: the
// reloader of the folder's snapshot, loaded at the folder's first request.
// It answers a name that is not one path element, and a folder without
// objects, with an error that wraps fs.ErrNotExist, and a failed load with
// its error; neither stays in memory.
func (p *Previews) Handler(ctx context.Context, name string) (http.Handler, error) {
	// HOLE(2): find the folder's snapshot in memory, or load it
	return nil, fmt.Errorf("preview %q: %w", name, fs.ErrNotExist)
}
