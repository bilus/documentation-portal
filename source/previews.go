package source

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Folder returns the folder at the path name inside b, such as previews or
// previews/pr-123, as a bucket folder of its own, with b's size limit: its
// keys are b's keys under name, without name and its slash, and it leaves
// out the folders left out of b. It refuses a name other than a valid io/fs
// path, after one trailing slash, and the name ".".
func (b *Bucket) Folder(name string) (*Bucket, error) {
	dir, err := folderPath(name)
	if err != nil {
		return nil, err
	}
	f := *b
	f.prefix = b.prefix + dir + "/"
	return &f, nil
}

// Without returns b with a listing that leaves out the folder at the path
// name inside b, such as the previews location: every key under name and its
// slash. It refuses the names refused by Folder.
func (b *Bucket) Without(name string) (*Bucket, error) {
	dir, err := folderPath(name)
	if err != nil {
		return nil, err
	}
	w := *b
	w.skip = append(slices.Clone(b.skip), b.prefix+dir+"/")
	return &w, nil
}

// folderPath returns name without one trailing slash, or refuses a name that
// names no folder inside a bucket folder.
func folderPath(name string) (string, error) {
	dir := strings.TrimSuffix(name, "/")
	if dir == "." || !fs.ValidPath(dir) {
		return "", fmt.Errorf("the folder %q is not a valid path inside the bucket folder", name)
	}
	return dir, nil
}

// Previews holds the preview snapshots: the snapshot of each preview folder
// in memory, with its portal handler and a reloader of its own.
type Previews struct {
	ctx      context.Context // stops the checks of every folder
	folder   func(name string) (Source, error)
	build    func(fs.FS) (http.Handler, error)
	interval time.Duration
	idle     time.Duration // 0 keeps an unused snapshot
	max      int           // 0 keeps any number

	mu      sync.Mutex
	folders map[string]*preview // in memory or loading, by name
}

// preview is a preview folder in memory, or loading.
type preview struct {
	name   string
	loaded chan struct{} // closed when the load ends
	h      http.Handler  // the folder's reloader, after a load without an error
	err    error         // the load's error

	// Previews.mu guards these.
	used    time.Time          // the last request's time
	stop    context.CancelFunc // stops the reloader's checks; nil while loading
	timer   *time.Timer        // drops the snapshot after the idle time
	dropped bool
}

// NewPreviews returns the preview snapshots of the folders from folder, by
// name, such as the folders of the previews location, with portal handlers
// from build, refreshed every interval until ctx ends, like the snapshots of
// a reloader. A snapshot leaves memory after idle without a request, as the
// least recently used one beyond maxSnapshots, the preview limit, after a
// load that succeeds, and at a check that finds its folder empty. An idle
// time of zero or less keeps an unused snapshot, and a maxSnapshots of zero
// or less keeps any number.
func NewPreviews(ctx context.Context, folder func(name string) (Source, error), build func(fs.FS) (http.Handler, error), interval, idle time.Duration, maxSnapshots int) *Previews {
	return &Previews{ctx: ctx, folder: folder, build: build, interval: interval, idle: idle, max: maxSnapshots, folders: map[string]*preview{}}
}

// Handler returns the portal handler of the preview folder name: the
// reloader of the folder's snapshot, loaded at the folder's first request.
// It answers a name that is not one path element, and a folder without
// objects, with an error that wraps fs.ErrNotExist, and a failed load with
// its error; neither stays in memory.
func (p *Previews) Handler(ctx context.Context, name string) (http.Handler, error) {
	if name == "." || strings.Contains(name, "/") || !fs.ValidPath(name) {
		return nil, fmt.Errorf("preview %q: not a folder name: %w", name, fs.ErrNotExist)
	}
	p.mu.Lock()
	f, loading := p.folders[name]
	if !loading {
		f = &preview{name: name, loaded: make(chan struct{})}
		p.folders[name] = f
	}
	f.used = time.Now()
	p.mu.Unlock()
	if loading {
		select {
		case <-f.loaded:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return f.h, f.err
	}
	f.h, f.err = p.load(ctx, f)
	close(f.loaded)
	if f.err != nil {
		p.drop(f)
		return nil, f.err
	}
	return f.h, nil
}

// load reads the snapshot of the folder of f, builds its portal handler and
// wraps it in a reloader of its own, whose checks drop f at a listing
// without objects, and then drops the least recently used snapshots beyond
// the maximum.
func (p *Previews) load(ctx context.Context, f *preview) (http.Handler, error) {
	src, err := p.folder(f.name)
	if err != nil {
		return nil, fmt.Errorf("preview %q: %w", f.name, err)
	}
	snap, err := Load(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("preview %q: %w", f.name, err)
	}
	if len(snap.Listing) == 0 {
		return nil, fmt.Errorf("preview %q: no objects: %w", f.name, fs.ErrNotExist)
	}
	h, err := p.build(snap.Root)
	if err != nil {
		return nil, fmt.Errorf("preview %q: %w", f.name, err)
	}
	checks, stop := context.WithCancel(p.ctx)
	watched := &emptyWatch{Source: src, empty: func() {
		log.Printf("preview %s: the folder is empty, and its snapshot leaves memory", f.name)
		p.drop(f)
	}}
	r := newReloader(checks, "preview "+f.name, watched, snap.Listing, h, p.interval, p.build)
	p.mu.Lock()
	if f.dropped {
		p.mu.Unlock()
		stop()
		return r, nil
	}
	f.stop = stop
	if p.idle > 0 {
		f.timer = time.AfterFunc(p.idle, func() { p.expire(f) })
	}
	victims := p.beyondMax(f)
	p.mu.Unlock()
	for _, v := range victims {
		p.drop(v)
	}
	return r, nil
}

// beyondMax takes the least recently used snapshots other than f's out of
// the map while it holds more than the maximum, and returns their folders,
// for drop. A folder still loading holds no snapshot yet. p.mu is held.
func (p *Previews) beyondMax(f *preview) []*preview {
	var victims []*preview
	for p.max > 0 {
		var oldest *preview
		n := 0
		for _, g := range p.folders {
			if g.stop == nil {
				continue
			}
			n++
			if g != f && (oldest == nil || g.used.Before(oldest.used)) {
				oldest = g
			}
		}
		if n <= p.max || oldest == nil {
			return victims
		}
		delete(p.folders, oldest.name)
		oldest.dropped = true
		victims = append(victims, oldest)
	}
	return victims
}

// expire drops f when its last request lies the idle time back, or else
// waits for the rest of the idle time.
func (p *Previews) expire(f *preview) {
	p.mu.Lock()
	if left := p.idle - time.Since(f.used); left > 0 && !f.dropped {
		f.timer.Reset(left)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.drop(f)
}

// drop takes f out of memory, unless a later load replaced it, and stops its
// checks and its timer.
func (p *Previews) drop(f *preview) {
	p.mu.Lock()
	if p.folders[f.name] == f {
		delete(p.folders, f.name)
	}
	f.dropped = true
	stop, timer := f.stop, f.timer
	p.mu.Unlock()
	if stop != nil {
		stop()
	}
	if timer != nil {
		timer.Stop()
	}
}

// emptyWatch is the source of a preview folder in memory, which calls empty
// at a listing without objects: a deleted folder.
type emptyWatch struct {
	Source
	empty func()
}

// List lists the source, and calls empty when the listing holds no object.
func (w *emptyWatch) List(ctx context.Context) (Listing, error) {
	listing, err := w.Source.List(ctx)
	if err == nil && len(listing) == 0 {
		w.empty()
	}
	return listing, err
}
