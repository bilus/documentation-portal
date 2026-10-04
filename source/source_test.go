package source

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"gocloud.dev/blob"
	"gocloud.dev/blob/memblob"
)

// memBucket returns a bucket folder in memory holding files, with the
// placeholder object that the GCS console writes for a folder.
func memBucket(t *testing.T, files map[string]string) *blob.Bucket {
	t.Helper()
	b := memblob.OpenBucket(nil)
	t.Cleanup(func() { b.Close() })
	for key, body := range files {
		if err := b.WriteAll(t.Context(), key, []byte(body), nil); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

var folder = map[string]string{
	"environment.yaml": "portals: []\n",
	"docs/a.md":        "# A\n",
	"docs/guide/":      "",
	"docs/guide/b.md":  "# B\n",
	"specs/pets.yaml":  "openapi: 3.1.0\n",
}

func TestBucketListing(t *testing.T) {
	src := NewBucket(memBucket(t, folder), 0)
	listing, err := src.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, o := range listing {
		keys = append(keys, o.Key)
		if o.Size != int64(len(folder[o.Key])) || o.ModTime.IsZero() || len(o.MD5) == 0 {
			t.Errorf("object %+v lacks its size, time or MD5", o)
		}
	}
	if want := []string{"docs/a.md", "docs/guide/b.md", "environment.yaml", "specs/pets.yaml"}; strings.Join(keys, " ") != strings.Join(want, " ") {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	if n := listing.Size(); n != int64(len(folder["environment.yaml"])+len(folder["docs/a.md"])+len(folder["docs/guide/b.md"])+len(folder["specs/pets.yaml"])) {
		t.Errorf("size = %d", n)
	}
}

func TestBucketRefusesAnInvalidKey(t *testing.T) {
	for _, key := range []string{"docs//a.md", "../a.md", "/a.md", "docs/./a.md"} {
		src := NewBucket(memBucket(t, map[string]string{"a.md": "# A\n", key: "x"}), 0)
		if _, err := src.List(t.Context()); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("key %q: err = %v, want one naming the key", key, err)
		}
	}
}

func TestBucketSnapshot(t *testing.T) {
	src := NewBucket(memBucket(t, folder), 0)
	snap, err := Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(snap.Root, "environment.yaml", "docs/a.md", "docs/guide/b.md", "specs/pets.yaml"); err != nil {
		t.Error(err)
	}
	if b, err := fs.ReadFile(snap.Root, "docs/guide/b.md"); err != nil || string(b) != "# B\n" {
		t.Errorf("docs/guide/b.md = %q, %v", b, err)
	}
	if len(snap.Listing) != 4 {
		t.Errorf("the snapshot's listing has %d objects, want 4", len(snap.Listing))
	}
	// The snapshot is a copy: a later write to the bucket leaves it as it is.
	if err := src.bucket.WriteAll(t.Context(), "docs/a.md", []byte("# Changed\n"), nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := fs.ReadFile(snap.Root, "docs/a.md"); string(b) != "# A\n" {
		t.Errorf("docs/a.md = %q after a write to the bucket", b)
	}
}

func TestBucketRefusesAFolderOverTheSizeLimit(t *testing.T) {
	b := memBucket(t, folder)
	listing, err := NewBucket(b, 0).List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBucket(b, listing.Size()-1).Read(t.Context(), listing); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Errorf("err = %v, want one naming the size limit", err)
	}
	if _, err := NewBucket(b, listing.Size()).Read(t.Context(), listing); err != nil {
		t.Errorf("a folder at the limit: %v", err)
	}
}

func TestDirectoryIsReadLive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := OpenDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Listing) != 0 {
		t.Errorf("listing = %v, want none", snap.Listing)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := fs.ReadFile(snap.Root, "a.md"); err != nil || string(b) != "# Changed\n" {
		t.Errorf("a.md = %q, %v: the directory is not read live", b, err)
	}
}

func TestListingEqual(t *testing.T) {
	now := time.Now()
	a := Listing{{Key: "a", Size: 1, ModTime: now, MD5: []byte{1}}, {Key: "b", Size: 2, ModTime: now}}
	same := Listing{{Key: "a", Size: 1, ModTime: now.In(time.UTC), MD5: []byte{1}}, {Key: "b", Size: 2, ModTime: now}}
	if !a.Equal(same) || !Listing(nil).Equal(Listing{}) {
		t.Error("equal listings compare unequal")
	}
	for name, other := range map[string]Listing{
		"a key":    {{Key: "a", Size: 1, ModTime: now, MD5: []byte{1}}, {Key: "c", Size: 2, ModTime: now}},
		"a size":   {{Key: "a", Size: 1, ModTime: now, MD5: []byte{1}}, {Key: "b", Size: 3, ModTime: now}},
		"a time":   {{Key: "a", Size: 1, ModTime: now, MD5: []byte{1}}, {Key: "b", Size: 2, ModTime: now.Add(time.Second)}},
		"an MD5":   {{Key: "a", Size: 1, ModTime: now, MD5: []byte{2}}, {Key: "b", Size: 2, ModTime: now}},
		"a length": {{Key: "a", Size: 1, ModTime: now, MD5: []byte{1}}},
	} {
		if a.Equal(other) {
			t.Errorf("listings that differ in %s compare equal", name)
		}
	}
}

// fakeSource is a documentation source whose listing a test sets, with a
// count of the checks made on it.
type fakeSource struct {
	mu       sync.Mutex
	listing  Listing
	files    map[string]string
	readErr  error // returned by Read when set
	volatile bool  // when set, every List returns a listing of its own
	lists    atomic.Int64
}

func (f *fakeSource) set(version string, files map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files = files
	f.listing = Listing{{Key: "version", Size: int64(len(version)), MD5: []byte(version)}}
}

func (f *fakeSource) List(context.Context) (Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.lists.Add(1)
	if f.volatile {
		return Listing{{Key: "version", Size: n, MD5: []byte("upload in progress")}}, nil
	}
	return append(Listing(nil), f.listing...), nil
}

func (f *fakeSource) Read(context.Context, Listing) (fs.FS, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	m := fstest.MapFS{}
	for name, body := range f.files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m, nil
}

// pageOf returns a handler that answers every request with the index.md of
// root, and counts the builds.
func pageOf(builds *atomic.Int64) func(fs.FS) (http.Handler, error) {
	return func(root fs.FS) (http.Handler, error) {
		builds.Add(1)
		body, err := fs.ReadFile(root, "index.md")
		if err != nil {
			return nil, err
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }), nil
	}
}

func body(h http.Handler) string {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	return rec.Body.String()
}

// waitFor polls until cond holds, for at most two seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for start := time.Now(); !cond(); time.Sleep(time.Millisecond) {
		if time.Since(start) > 2*time.Second {
			t.Fatalf("waited two seconds for %s", what)
		}
	}
}

func TestReloaderSwapsASettledChange(t *testing.T) {
	src := &fakeSource{}
	src.set("v1", map[string]string{"index.md": "one"})
	var builds atomic.Int64
	build := pageOf(&builds)
	snap, err := Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	first, err := build(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	h := NewReloader(t.Context(), src, snap.Listing, first, time.Millisecond, build)
	if got := body(h); got != "one" {
		t.Fatalf("the first snapshot answers %q", got)
	}

	// A change becomes the snapshot in service once two checks report it alike.
	src.set("v2", map[string]string{"index.md": "two"})
	waitFor(t, "the second snapshot", func() bool { return body(h) == "two" })
	if n := builds.Load(); n != 2 {
		t.Errorf("%d builds, want 2", n)
	}

	// A listing that changes at every check never settles.
	src.mu.Lock()
	src.files = map[string]string{"index.md": "unsettled"}
	src.volatile = true
	src.mu.Unlock()
	lists := src.lists.Load()
	waitFor(t, "twenty more checks", func() bool { return src.lists.Load() >= lists+20 })
	if got := body(h); got != "two" {
		t.Errorf("an unsettled change was loaded: %q", got)
	}
	src.mu.Lock()
	src.volatile = false
	src.mu.Unlock()

	// A failed read keeps the snapshot in service, and the next check tries again.
	src.mu.Lock()
	src.readErr = errors.New("bucket unreachable")
	src.mu.Unlock()
	src.set("v3", map[string]string{"index.md": "three"})
	lists = src.lists.Load()
	waitFor(t, "three more checks", func() bool { return src.lists.Load() >= lists+3 })
	if got := body(h); got != "two" {
		t.Errorf("after a failed read, %q", got)
	}
	src.mu.Lock()
	src.readErr = nil
	src.mu.Unlock()
	waitFor(t, "the third snapshot", func() bool { return body(h) == "three" })

	// A build that fails keeps the snapshot in service too.
	src.set("v4", map[string]string{"readme.md": "no index"})
	lists = src.lists.Load()
	waitFor(t, "three more checks", func() bool { return src.lists.Load() >= lists+3 })
	if got := body(h); got != "three" {
		t.Errorf("after a failed build, %q", got)
	}
	// The next check tries the build again.
	if n := builds.Load(); n < 4 {
		t.Errorf("%d builds, want the failing one tried", n)
	}
}

func TestReloaderWithoutChecks(t *testing.T) {
	src := &fakeSource{}
	src.set("v1", map[string]string{"index.md": "one"})
	var builds atomic.Int64
	snap, _ := Load(t.Context(), src)
	first, _ := pageOf(&builds)(snap.Root)
	h := NewReloader(t.Context(), src, snap.Listing, first, 0, pageOf(&builds))
	src.set("v2", map[string]string{"index.md": "two"})
	time.Sleep(20 * time.Millisecond)
	if got := body(h); got != "one" || src.lists.Load() != 1 {
		t.Errorf("answers %q after %d checks, want one and 1", got, src.lists.Load())
	}
}

// safeBuffer is a bytes.Buffer that the reloader's goroutine and a test may
// share.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestReloaderLogsAFailureOnce(t *testing.T) {
	var logged safeBuffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	src := &fakeSource{}
	src.set("v1", map[string]string{"index.md": "one"})
	var builds atomic.Int64
	snap, _ := Load(t.Context(), src)
	first, _ := pageOf(&builds)(snap.Root)
	h := NewReloader(t.Context(), src, snap.Listing, first, time.Millisecond, pageOf(&builds))
	src.mu.Lock()
	src.readErr = errors.New("bucket unreachable")
	src.mu.Unlock()
	src.set("v2", map[string]string{"index.md": "two"})
	lists := src.lists.Load()
	waitFor(t, "ten more checks", func() bool { return src.lists.Load() >= lists+10 })
	if n := strings.Count(logged.String(), "bucket unreachable"); n != 1 {
		t.Errorf("the failure was logged %d times:\n%s", n, logged.String())
	}
	src.mu.Lock()
	src.readErr = nil
	src.mu.Unlock()
	waitFor(t, "the second snapshot", func() bool { return body(h) == "two" })
	if !strings.Contains(logged.String(), "in service") {
		t.Errorf("the swap was not logged:\n%s", logged.String())
	}
}

// TestReloaderSmoke runs the reloader's steps on one change of the source:
// the first snapshot goes in service, a check finds the change, the rebuild
// makes its handler, the swap puts it in service, and a request reads it.
func TestReloaderSmoke(t *testing.T) {
	src := &fakeSource{}
	src.set("v1", map[string]string{"index.md": "one"})
	var builds atomic.Int64
	build := pageOf(&builds)
	snap, err := Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	first, err := build(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	h := NewReloader(t.Context(), src, snap.Listing, first, time.Millisecond, build)
	if got := body(h); got != "one" {
		t.Fatalf("the first snapshot answers %q", got)
	}
	src.set("v2", map[string]string{"index.md": "two"})
	waitFor(t, "the second snapshot", func() bool { return body(h) == "two" })
	if n := builds.Load(); n != 2 {
		t.Errorf("%d builds, want 2", n)
	}
}

func TestBucketListingLeavesOutTheFolderItself(t *testing.T) {
	// The console's "Create folder" writes a zero-byte object at the prefix,
	// which the prefix strips to an empty key.
	b := memBucket(t, map[string]string{"portal/": "", "portal/environment.yaml": "portals: []\n", "portal/docs/": "", "portal/docs/a.md": "# A\n"})
	listing, err := NewBucket(blob.PrefixedBucket(b, "portal/"), 0).List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, o := range listing {
		keys = append(keys, o.Key)
	}
	if want := "docs/a.md environment.yaml"; strings.Join(keys, " ") != want {
		t.Errorf("keys = %v, want %s", keys, want)
	}
}

func TestBucketListingRefusesAnObjectNamedLikeAFolder(t *testing.T) {
	src := NewBucket(memBucket(t, map[string]string{"docs": "a file", "docs/a.md": "# A\n"}), 0)
	_, err := src.List(t.Context())
	if err == nil || !strings.Contains(err.Error(), `"docs"`) {
		t.Errorf("err = %v, want one naming docs", err)
	}
	// A non-empty object whose key ends in a slash is no placeholder.
	src = NewBucket(memBucket(t, map[string]string{"a.md": "# A\n", "docs/": "x"}), 0)
	if _, err := src.List(t.Context()); err == nil || !strings.Contains(err.Error(), `"docs/"`) {
		t.Errorf("err = %v, want one naming docs/", err)
	}
}

func TestBucketReadCountsAnObjectThatGrew(t *testing.T) {
	b := memBucket(t, folder)
	src := NewBucket(b, 0)
	listing, err := src.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.WriteAll(t.Context(), "docs/a.md", []byte(strings.Repeat("# A\n", 100)), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBucket(b, listing.Size()).Read(t.Context(), listing); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Errorf("err = %v, want one naming the size limit", err)
	}
}

// drive returns the reloader of src's first snapshot without a timer, so
// that a test calls its steps by hand.
func drive(t *testing.T, src *fakeSource, builds *atomic.Int64) *reloader {
	t.Helper()
	snap, err := Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	first, err := pageOf(builds)(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	return NewReloader(t.Context(), src, snap.Listing, first, 0, pageOf(builds)).(*reloader)
}

func TestCheckSettlesAnEmptiedFolder(t *testing.T) {
	var logged safeBuffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	src := &fakeSource{}
	src.set("v1", map[string]string{"index.md": "one"})
	var builds atomic.Int64
	r := drive(t, src, &builds)
	src.mu.Lock()
	src.listing, src.files = nil, nil
	src.mu.Unlock()
	if _, ok := r.check(t.Context()); ok {
		t.Error("an emptied folder settled at first sight")
	}
	settled, ok := r.check(t.Context())
	if !ok || len(settled) != 0 {
		t.Fatalf("the emptied folder did not settle at the second check: %v, %v", settled, ok)
	}
	if _, err := r.rebuild(t.Context(), settled); err == nil || !strings.Contains(logged.String(), "index.md") {
		t.Errorf("the failed build of the emptied folder: err %v, log %q", err, logged.String())
	}
}

func TestCheckForgetsAChangeThatReverts(t *testing.T) {
	src := &fakeSource{}
	src.set("v1", map[string]string{"index.md": "one"})
	var builds atomic.Int64
	r := drive(t, src, &builds)
	src.set("v2", map[string]string{"index.md": "two"})
	if _, ok := r.check(t.Context()); ok {
		t.Fatal("a change settled at first sight")
	}
	src.set("v1", map[string]string{"index.md": "one"})
	if _, ok := r.check(t.Context()); ok {
		t.Fatal("the snapshot in service counted as a change")
	}
	// The change comes back: it is a first sight again.
	src.set("v2", map[string]string{"index.md": "two"})
	if _, ok := r.check(t.Context()); ok {
		t.Error("a change that came back after a revert was loaded at first sight")
	}
	if _, ok := r.check(t.Context()); !ok {
		t.Error("the change did not settle at its second check")
	}
}

func TestReloaderLogsAProblemThatComesBack(t *testing.T) {
	var logged safeBuffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	src := &fakeSource{}
	src.set("v1", map[string]string{"index.md": "one"})
	var builds atomic.Int64
	r := drive(t, src, &builds)
	fail := func(version string) {
		t.Helper()
		src.set(version, map[string]string{"index.md": version})
		src.mu.Lock()
		src.readErr = errors.New("bucket unreachable")
		src.mu.Unlock()
		r.check(t.Context())
		settled, ok := r.check(t.Context())
		if !ok {
			t.Fatalf("%s did not settle", version)
		}
		if _, err := r.rebuild(t.Context(), settled); err == nil {
			t.Fatalf("%s: the read did not fail", version)
		}
		src.mu.Lock()
		src.readErr = nil
		src.mu.Unlock()
		h, err := r.rebuild(t.Context(), settled)
		if err != nil {
			t.Fatal(err)
		}
		r.swap(settled, h)
	}
	fail("v2")
	fail("v3")
	if n := strings.Count(logged.String(), "bucket unreachable"); n != 2 {
		t.Errorf("a problem that came back after a swap was logged %d times, want 2:\n%s", n, logged.String())
	}
}

func TestRebuildLogsAFailedBuild(t *testing.T) {
	var logged safeBuffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	src := &fakeSource{}
	src.set("v1", map[string]string{"index.md": "one"})
	var builds atomic.Int64
	r := drive(t, src, &builds)
	src.set("v2", map[string]string{"readme.md": "no index"})
	r.check(t.Context())
	settled, ok := r.check(t.Context())
	if !ok {
		t.Fatal("v2 did not settle")
	}
	if _, err := r.rebuild(t.Context(), settled); err == nil || !strings.Contains(logged.String(), "index.md") {
		t.Errorf("a failed build: err %v, log %q", err, logged.String())
	}
}
