// Package source reads the documentation root from a documentation source: a
// local directory, read live, or a bucket folder on GCS, S3 or the local file
// system, read into snapshots that a reloader swaps as the folder changes.
package source

import (
	"context"
	"io/fs"
	"net/http"
	"os"
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
	// HOLE(1): list the source and read the listing's files into the snapshot
	root, err := src.Read(ctx, nil)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Root: root}, nil
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
	// HOLE(1): return the empty listing
	return nil, nil
}

// Read returns the directory as a documentation root handle, whatever the
// listing.
func (d *Directory) Read(ctx context.Context, listing Listing) (fs.FS, error) {
	// HOLE(1): return the directory's root as an fs.FS
	return d.root.FS(), nil
}

// Bucket is a bucket folder as a documentation source: the objects of a Go
// CDK bucket, whose keys the bucket's prefix, if any, has already stripped.
type Bucket struct {
	bucket *blob.Bucket
	limit  int64 // the size limit in bytes; 0 means none
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
// the zero-byte objects whose keys end in a slash, and refuses a key that is
// not a valid io/fs path.
func (b *Bucket) List(ctx context.Context) (Listing, error) {
	// HOLE(1): list the bucket's objects into a listing
	return nil, nil
}

// Read reads the objects of listing into a documentation root handle held in
// memory, or refuses a listing over the size limit.
func (b *Bucket) Read(ctx context.Context, listing Listing) (fs.FS, error) {
	// HOLE(1): read each object of the listing into an in-memory file system
	return nil, nil
}

// NewReloader wraps first, the portal handler of src's snapshot with
// listing, in the reloader: a handler that answers each request with the
// handler in service, checks src every interval, and for each settled change
// builds the new snapshot's handler with build and swaps it in. A failed
// check or refresh keeps the handler in service and goes to the log, and the
// next check tries again. An interval of zero or less turns the checks off,
// and ctx stops them.
func NewReloader(ctx context.Context, src Source, listing Listing, first http.Handler, interval time.Duration, build func(fs.FS) (http.Handler, error)) http.Handler {
	// HOLE(2): build the reloader from its steps: swap the first handler in, and check, rebuild and swap on a timer
	return first
}
