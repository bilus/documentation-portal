// Package source reads the documentation root from a documentation source: a
// local directory, read live, or a bucket folder on GCS, S3 or the local file
// system, read into snapshots that a reloader swaps as the folder changes.
//
// A program opens a source with OpenDirectory or OpenBucket, after a blank
// import of the Go CDK drivers it needs, loads its first snapshot with Load,
// builds the portal handler from the snapshot's root, and wraps the handler
// in NewReloader, which builds it again for each settled change.
package source

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"testing/fstest"
	"time"

	"gocloud.dev/blob"
)

// Source is a documentation source: List lists it, and Read reads a
// listing's files into a documentation root handle.
type Source interface {
	// List returns the source's listing: its objects in key order.
	List(ctx context.Context) (Listing, error)
	// Read reads the files of listing, as List returned it, into a
	// documentation root handle.
	Read(ctx context.Context, listing Listing) (fs.FS, error)
}

// Listing is the objects of a documentation source at one check, in key
// order. A local directory's listing is empty.
type Listing []Object

// Object is one object of a listing, as the bucket reports it.
type Object struct {
	Key     string // a valid io/fs path
	Size    int64
	ModTime time.Time
	MD5     []byte // empty when the bucket reports none
}

// Equal reports whether l and m hold the same objects, item for item.
func (l Listing) Equal(m Listing) bool {
	if len(l) != len(m) {
		return false
	}
	for i := range l {
		if l[i].Key != m[i].Key || l[i].Size != m[i].Size || !l[i].ModTime.Equal(m[i].ModTime) || string(l[i].MD5) != string(m[i].MD5) {
			return false
		}
	}
	return true
}

// Size returns the total size of l's objects.
func (l Listing) Size() int64 {
	var n int64
	for _, o := range l {
		n += o.Size
	}
	return n
}

// Snapshot is a documentation source read at one time: a documentation root
// handle, and the listing it was read from.
type Snapshot struct {
	Root    fs.FS
	Listing Listing
}

// Load loads the snapshot of src: it lists src and reads the listing's files.
func Load(ctx context.Context, src Source) (Snapshot, error) {
	listing, err := src.List(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	root, err := src.Read(ctx, listing)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Root: root, Listing: listing}, nil
}

// Directory is a local directory as a documentation source: its listing is
// empty, and its snapshot is the directory itself, read live through
// os.OpenRoot, which keeps every read inside it, symlinks included.
type Directory struct {
	root *os.Root
}

// OpenDirectory opens the directory at name as a documentation source. The
// directory stays open for as long as the source is in use.
func OpenDirectory(name string) (*Directory, error) {
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return &Directory{root: root}, nil
}

// List returns the empty listing: a directory is read live, so the reloader
// never finds a change in it.
func (d *Directory) List(ctx context.Context) (Listing, error) {
	return nil, nil
}

// Read returns the directory as a documentation root handle, whatever the
// listing.
func (d *Directory) Read(ctx context.Context, listing Listing) (fs.FS, error) {
	return d.root.FS(), nil
}

// Bucket is a bucket folder as a documentation source: the objects of a Go
// CDK bucket, whose keys the bucket's prefix, if any, has already stripped.
type Bucket struct {
	bucket *blob.Bucket
	prefix string        // the keys' prefix of the folder in bucket, ending in a slash, or empty
	skip   []string      // the keys' prefixes in bucket of the folders that the listing leaves out
	limit  int64         // the size limit in bytes; 0 means none
	read   func(n int64) // called with the bytes read of each object, for tests
}

// NewBucket makes the bucket folder of bucket a documentation source, whose
// snapshots hold at most limit bytes of objects; a limit of 0 means no limit.
func NewBucket(bucket *blob.Bucket, limit int64) *Bucket {
	return &Bucket{bucket: bucket, limit: limit}
}

// OpenBucket opens the bucket folder at the Go CDK URL rawURL, such as
// gs://docs?prefix=portal/, with the drivers the program has registered, as a
// documentation source whose snapshots hold at most limit bytes.
func OpenBucket(ctx context.Context, rawURL string, limit int64) (*Bucket, error) {
	bucket, err := blob.OpenBucket(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return NewBucket(bucket, limit), nil
}

// List lists the folder's objects in key order, with each one's size,
// modification time and MD5 sum as the bucket reports them. It leaves out
// the zero-byte objects whose keys end in a slash or are empty, which the
// console writes for a folder and for the folder itself, and refuses a key
// that is not a valid io/fs path or that is also a directory of another key.
func (b *Bucket) List(ctx context.Context) (Listing, error) {
	var listing Listing
	dirs := map[string]bool{} // every directory of a key
	it := b.bucket.List(&blob.ListOptions{Prefix: b.prefix})
	for {
		o, err := it.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list the bucket folder: %w", err)
		}
		if slices.ContainsFunc(b.skip, func(skip string) bool { return strings.HasPrefix(o.Key, skip) }) {
			continue
		}
		key := strings.TrimPrefix(o.Key, b.prefix)
		if o.Size == 0 && (key == "" || strings.HasSuffix(key, "/")) {
			continue
		}
		if !fs.ValidPath(key) {
			return nil, fmt.Errorf("list the bucket folder: the key %q is not a valid path", key)
		}
		for dir := path.Dir(key); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = true
		}
		listing = append(listing, Object{Key: key, Size: o.Size, ModTime: o.ModTime, MD5: o.MD5})
	}
	for _, o := range listing {
		if dirs[o.Key] {
			return nil, fmt.Errorf("list the bucket folder: the key %q names both an object and a folder", o.Key)
		}
	}
	// The drivers list in key order; sorting keeps Equal exact without them.
	slices.SortFunc(listing, func(a, b Object) int { return strings.Compare(a.Key, b.Key) })
	return listing, nil
}

// Read reads the objects of listing into a documentation root handle held in
// memory. It refuses a listing over the size limit, reads no object past
// the limit, and refuses an object whose size or MD5 sum differs from the
// listing's, which changed since the listing, so that the snapshot never
// mixes two versions of the folder.
func (b *Bucket) Read(ctx context.Context, listing Listing) (fs.FS, error) {
	if b.limit > 0 && listing.Size() > b.limit {
		return nil, fmt.Errorf("the bucket folder holds %d bytes, over the size limit of %d", listing.Size(), b.limit)
	}
	root := make(fstest.MapFS, len(listing))
	var read int64
	for _, o := range listing {
		// One byte past the allowance tells an object that grew from one at the limit.
		allowance := int64(1 << 62)
		if b.limit > 0 {
			allowance = b.limit - read + 1
		}
		data, err := b.readObject(ctx, o.Key, allowance)
		if err != nil {
			return nil, fmt.Errorf("read %s from the bucket folder: %w", o.Key, err)
		}
		if read += int64(len(data)); b.limit > 0 && read > b.limit {
			return nil, fmt.Errorf("the bucket folder holds over %d bytes, the size limit", b.limit)
		}
		if int64(len(data)) != o.Size || (len(o.MD5) > 0 && !bytes.Equal(sumOf(data), o.MD5)) {
			return nil, fmt.Errorf("read %s from the bucket folder: the object changed since the listing", o.Key)
		}
		root[o.Key] = &fstest.MapFile{Data: data, Mode: 0o444, ModTime: o.ModTime}
	}
	return root, nil
}

// readObject reads at most max bytes of the object at key.
func (b *Bucket) readObject(ctx context.Context, key string, max int64) ([]byte, error) {
	r, err := b.bucket.NewReader(ctx, b.prefix+key, nil)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, max))
	if b.read != nil {
		b.read(int64(len(data)))
	}
	return data, err
}

// sumOf returns the MD5 sum of data, as a bucket reports it.
func sumOf(data []byte) []byte {
	sum := md5.Sum(data)
	return sum[:]
}

// NewReloader wraps first, the portal handler of src's snapshot with
// listing, in the reloader: a handler that answers each request with the
// handler in service, checks src every interval, and for each settled change
// builds the new snapshot's handler with build and swaps it in. A failed
// check or refresh keeps the handler in service and goes to the log, and the
// next check tries again. An interval of zero or less turns the checks off,
// and ctx stops them.
func NewReloader(ctx context.Context, src Source, listing Listing, first http.Handler, interval time.Duration, build func(fs.FS) (http.Handler, error)) http.Handler {
	return newReloader(ctx, "documentation source", src, listing, first, interval, build)
}

// newReloader is NewReloader with the name of the source in the log.
func newReloader(ctx context.Context, name string, src Source, listing Listing, first http.Handler, interval time.Duration, build func(fs.FS) (http.Handler, error)) *reloader {
	r := &reloader{name: name, src: src, build: build}
	r.swap(listing, first)
	if interval > 0 {
		go r.run(ctx, interval)
	}
	return r
}

// reloader answers each request with the portal handler of the snapshot in
// service, and swaps in the handler of each settled change of its source.
type reloader struct {
	name  string // the source's name in the log
	src   Source
	build func(fs.FS) (http.Handler, error)

	inService atomic.Pointer[snapshotInService]
	// run alone touches these, after the first swap.
	last     Listing // the last check's listing, when it differed from the snapshot in service
	lastSeen bool    // whether last holds a listing, which may be empty
	problem  string  // the last problem logged, so that a check logs it once
}

// snapshotInService is the listing of the snapshot in service and its portal
// handler.
type snapshotInService struct {
	listing Listing
	handler http.Handler
}

// run checks the source every interval until ctx ends, and for each settled
// change reads the snapshot, builds its handler and swaps it in.
func (r *reloader) run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		settled, ok := r.check(ctx)
		if !ok {
			continue
		}
		h, err := r.rebuild(ctx, settled)
		if err != nil {
			continue
		}
		r.swap(settled, h)
	}
}

// swap puts the snapshot with listing and its portal handler h in service,
// in one step, so that every request from now on reads that snapshot, and
// logs the swap, unless the listing is empty, as a directory's is. It forgets
// the last problem, so that the log names it again if it comes back.
func (r *reloader) swap(listing Listing, h http.Handler) {
	r.inService.Store(&snapshotInService{listing: listing, handler: h})
	if len(listing) > 0 {
		log.Printf("%s: %d objects in service", r.name, len(listing))
	}
	r.problem = ""
}

// note logs problem, unless the log already named the same problem last.
func (r *reloader) note(problem string) {
	if problem != r.problem {
		log.Printf("%s: %s", r.name, problem)
	}
	r.problem = problem
}

// check lists the source and compares the listing with the snapshot in
// service and with the last check's: it returns the listing and true for a
// settled change, a listing that differs from the snapshot in service and
// that the last check reported too. A failed listing goes to the log, once
// until the problem changes, and counts as no change.
func (r *reloader) check(ctx context.Context) (Listing, bool) {
	listing, err := r.src.List(ctx)
	if err != nil {
		r.note("check: " + err.Error())
		return nil, false
	}
	if listing.Equal(r.inService.Load().listing) {
		r.last, r.lastSeen = nil, false
		return nil, false
	}
	// A listing seen for the first time may be an upload in progress.
	if !r.lastSeen || !listing.Equal(r.last) {
		r.last, r.lastSeen = listing, true
		return nil, false
	}
	return listing, true
}

// rebuild reads the snapshot of the settled listing and builds its portal
// handler with the builder, or logs the failure, so that the snapshot in
// service stays until the next check.
func (r *reloader) rebuild(ctx context.Context, listing Listing) (http.Handler, error) {
	root, err := r.src.Read(ctx, listing)
	if err != nil {
		r.note("refresh: " + err.Error())
		return nil, err
	}
	h, err := r.build(root)
	if err != nil {
		r.note("refresh: " + err.Error())
		return nil, err
	}
	return h, nil
}

// ServeHTTP answers the request with the portal handler of the snapshot in
// service, which the request keeps to its end.
func (r *reloader) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.inService.Load().handler.ServeHTTP(w, req)
}
