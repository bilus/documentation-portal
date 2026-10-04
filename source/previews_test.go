package source

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// bucketWithPreviews is a bucket folder with a configuration file, a
// previews location with the preview folders pr-1 and pr-2, and a folder
// whose name starts with the location's.
var bucketWithPreviews = map[string]string{
	"environment.yaml":               "portals: []\n",
	"previews/":                      "",
	"previews/pr-1/":                 "",
	"previews/pr-1/environment.yaml": "portals: [pr-1]\n",
	"previews/pr-1/docs/a.md":        "# A\n",
	"previews/pr-2/environment.yaml": "portals: [pr-2]\n",
	"previews-old/x.md":              "# X\n",
}

// keysOf returns the keys of listing, separated by spaces.
func keysOf(listing Listing) string {
	var keys []string
	for _, o := range listing {
		keys = append(keys, o.Key)
	}
	return strings.Join(keys, " ")
}

func TestBucketFolderReadsItsOwnKeys(t *testing.T) {
	b := memBucket(t, bucketWithPreviews)
	pr1, err := NewBucket(b, 0).Folder("previews/pr-1")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Load(t.Context(), pr1)
	if err != nil {
		t.Fatal(err)
	}
	if keys := keysOf(snap.Listing); keys != "docs/a.md environment.yaml" {
		t.Errorf("the keys of previews/pr-1: %s", keys)
	}
	if data, err := fs.ReadFile(snap.Root, "environment.yaml"); err != nil || string(data) != "portals: [pr-1]\n" {
		t.Errorf("the configuration file of previews/pr-1: %q, %v", data, err)
	}
	// A folder of a folder, after a trailing slash.
	location, err := NewBucket(b, 0).Folder("previews/")
	if err != nil {
		t.Fatal(err)
	}
	pr2, err := location.Folder("pr-2")
	if err != nil {
		t.Fatal(err)
	}
	if listing, err := pr2.List(t.Context()); err != nil || keysOf(listing) != "environment.yaml" {
		t.Errorf("the keys of pr-2 in previews: %s, %v", keysOf(listing), err)
	}
	// The folder keeps the size limit.
	small, err := NewBucket(b, 5).Folder("previews/pr-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), small); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Errorf("a folder over the size limit: %v", err)
	}
	for _, name := range []string{"", ".", "..", "/previews", "previews//pr-1", "previews/../x", "previews/pr-1//"} {
		if _, err := NewBucket(b, 0).Folder(name); err == nil {
			t.Errorf("the folder %q was accepted", name)
		}
	}
}

func TestBucketWithoutLeavesTheFolderOut(t *testing.T) {
	b := memBucket(t, bucketWithPreviews)
	published, err := NewBucket(b, 0).Without("previews")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Load(t.Context(), published)
	if err != nil {
		t.Fatal(err)
	}
	if keys := keysOf(snap.Listing); keys != "environment.yaml previews-old/x.md" {
		t.Errorf("the keys without previews: %s", keys)
	}
	// The folder left out counts against no size limit.
	limited, err := NewBucket(b, int64(len("portals: []\n")+len("# X\n"))).Without("previews/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), limited); err != nil {
		t.Errorf("the folder left out counts against the size limit: %v", err)
	}
	// A folder inside the folder left out leaves out all of it.
	inside, err := published.Folder("previews/pr-1")
	if err != nil {
		t.Fatal(err)
	}
	if listing, err := inside.List(t.Context()); err != nil || len(listing) != 0 {
		t.Errorf("a folder inside the folder left out lists %s, %v", keysOf(listing), err)
	}
	for _, name := range []string{"", ".", "..", "/previews"} {
		if _, err := NewBucket(b, 0).Without(name); err == nil {
			t.Errorf("the folder %q was accepted", name)
		}
	}
}

// folders returns the folder function of NewPreviews over srcs, by name, and
// counts its calls in opened. A name of no source gets an error.
func folders(srcs map[string]*fakeSource, opened *atomic.Int64) func(string) (Source, error) {
	return func(name string) (Source, error) {
		opened.Add(1)
		if src, ok := srcs[name]; ok {
			return src, nil
		}
		return nil, fmt.Errorf("no source for %q", name)
	}
}

// folderOf returns a fake source whose index.md says text.
func folderOf(text string) *fakeSource {
	src := &fakeSource{}
	src.set("v1", map[string]string{"index.md": text})
	return src
}

func TestPreviewsLoadAFolderAtItsFirstRequest(t *testing.T) {
	one, two, three := folderOf("one"), folderOf("two"), folderOf("three")
	three.readErr = errors.New("bucket unreachable")
	var opened, builds atomic.Int64
	p := NewPreviews(t.Context(), folders(map[string]*fakeSource{"pr-1": one, "pr-2": two, "pr-3": three}, &opened), pageOf(&builds), 0, 0, 0)
	if builds.Load() != 0 || opened.Load() != 0 {
		t.Fatalf("NewPreviews opened %d folders and built %d handlers", opened.Load(), builds.Load())
	}
	h, err := p.Handler(t.Context(), "pr-1")
	if err != nil || body(h) != "one" {
		t.Fatalf("pr-1: %v", err)
	}
	// The next request finds the snapshot in memory.
	if h, err := p.Handler(t.Context(), "pr-1"); err != nil || body(h) != "one" || builds.Load() != 1 || one.lists.Load() != 1 {
		t.Errorf("pr-1 again: %v, %d builds, %d listings", err, builds.Load(), one.lists.Load())
	}
	// Two first requests of one folder share one load.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if h, err := p.Handler(t.Context(), "pr-2"); err != nil || body(h) != "two" {
				t.Errorf("pr-2: %v", err)
			}
		})
	}
	wg.Wait()
	if builds.Load() != 2 || two.lists.Load() != 1 {
		t.Errorf("eight first requests of pr-2 made %d builds and %d listings, want 1 of each", builds.Load()-1, two.lists.Load())
	}
	// A failed load stays out of memory, and the next request loads again.
	if _, err := p.Handler(t.Context(), "pr-3"); err == nil || !strings.Contains(err.Error(), "bucket unreachable") {
		t.Errorf("pr-3 unreachable: %v", err)
	}
	three.mu.Lock()
	three.readErr = nil
	three.mu.Unlock()
	if h, err := p.Handler(t.Context(), "pr-3"); err != nil || body(h) != "three" {
		t.Errorf("pr-3 after its failure: %v", err)
	}
}

func TestPreviewsRefreshAFolder(t *testing.T) {
	one := folderOf("one")
	var opened, builds atomic.Int64
	p := NewPreviews(t.Context(), folders(map[string]*fakeSource{"pr-1": one}, &opened), pageOf(&builds), time.Millisecond, 0, 0)
	h, err := p.Handler(t.Context(), "pr-1")
	if err != nil {
		t.Fatal(err)
	}
	one.set("v2", map[string]string{"index.md": "two"})
	waitFor(t, "the second snapshot of pr-1", func() bool { return body(h) == "two" })
}

func TestPreviewsDropAnIdleFolder(t *testing.T) {
	one := folderOf("one")
	var opened, builds atomic.Int64
	p := NewPreviews(t.Context(), folders(map[string]*fakeSource{"pr-1": one}, &opened), pageOf(&builds), 0, 100*time.Millisecond, 0)
	// Requests closer together than the idle time keep the snapshot.
	for range 20 {
		if _, err := p.Handler(t.Context(), "pr-1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := builds.Load(); n != 1 {
		t.Errorf("a snapshot in use was loaded %d times", n)
	}
	// Idle, it leaves memory, and the next request loads it again.
	time.Sleep(500 * time.Millisecond)
	if _, err := p.Handler(t.Context(), "pr-1"); err != nil || builds.Load() != 2 {
		t.Errorf("after the idle time: %v, %d builds, want 2", err, builds.Load())
	}
}

func TestPreviewsKeepAtMostTheMaximum(t *testing.T) {
	srcs := map[string]*fakeSource{"pr-1": folderOf("pr-1"), "pr-2": folderOf("pr-2"), "pr-3": folderOf("pr-3")}
	var opened, builds atomic.Int64
	p := NewPreviews(t.Context(), folders(srcs, &opened), pageOf(&builds), 0, 0, 2)
	open := func(name string, wantBuilds int64) {
		t.Helper()
		h, err := p.Handler(t.Context(), name)
		if err != nil || body(h) != name {
			t.Fatalf("%s: %v", name, err)
		}
		if n := builds.Load(); n != wantBuilds {
			t.Errorf("after %s: %d builds, want %d", name, n, wantBuilds)
		}
	}
	open("pr-1", 1)
	open("pr-2", 2)
	open("pr-1", 2) // pr-1 is now the most recently used
	open("pr-3", 3) // pr-2 leaves memory
	open("pr-1", 3)
	open("pr-3", 3)
	open("pr-2", 4)
}

func TestPreviewsDropADeletedFolderAtTheNextCheck(t *testing.T) {
	one := folderOf("one")
	var opened, builds atomic.Int64
	p := NewPreviews(t.Context(), folders(map[string]*fakeSource{"pr-1": one}, &opened), pageOf(&builds), time.Millisecond, 0, 0)
	if _, err := p.Handler(t.Context(), "pr-1"); err != nil {
		t.Fatal(err)
	}
	one.mu.Lock()
	one.listing, one.files = nil, nil
	lists := one.lists.Load()
	one.mu.Unlock()
	// The next check finds the folder empty, drops its snapshot and stops
	// the checks.
	waitFor(t, "a check of the deleted folder", func() bool { return one.lists.Load() > lists })
	time.Sleep(50 * time.Millisecond)
	if n := one.lists.Load() - lists; n != 1 {
		t.Errorf("%d checks of the deleted folder, want 1", n)
	}
	if _, err := p.Handler(t.Context(), "pr-1"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the deleted folder: %v, want an error of fs.ErrNotExist", err)
	}
}

func TestPreviewsRefuseANameOfNoFolder(t *testing.T) {
	var opened, builds atomic.Int64
	p := NewPreviews(t.Context(), folders(map[string]*fakeSource{"empty": {}}, &opened), pageOf(&builds), 0, 0, 0)
	for _, name := range []string{"", ".", "..", "a/b", "/a", "a/"} {
		if _, err := p.Handler(t.Context(), name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the name %q: %v, want an error of fs.ErrNotExist", name, err)
		}
	}
	if n := opened.Load(); n != 0 {
		t.Errorf("invalid names opened %d folders", n)
	}
	// A folder without objects, as the bucket lists an unknown one, is none.
	if _, err := p.Handler(t.Context(), "empty"); !errors.Is(err, fs.ErrNotExist) || builds.Load() != 0 {
		t.Errorf("the empty folder: %v, %d builds", err, builds.Load())
	}
	// A folder that does not open gives its error.
	if _, err := p.Handler(t.Context(), "unknown"); err == nil || !strings.Contains(err.Error(), `no source for "unknown"`) {
		t.Errorf("an unknown folder: %v", err)
	}
}

func TestPreviewsKeepTheirSnapshotsThroughFailedLoads(t *testing.T) {
	srcs := map[string]*fakeSource{"pr-1": folderOf("pr-1"), "pr-2": folderOf("pr-2"), "empty": {}}
	var opened, builds atomic.Int64
	p := NewPreviews(t.Context(), folders(srcs, &opened), pageOf(&builds), 0, 0, 2)
	for _, name := range []string{"pr-1", "pr-2"} {
		if _, err := p.Handler(t.Context(), name); err != nil {
			t.Fatal(err)
		}
	}
	// Folders that load no snapshot, such as names a stranger tries, take no
	// snapshot's place.
	for range 5 {
		for _, name := range []string{"empty", "unknown", "pr-3"} {
			if _, err := p.Handler(t.Context(), name); err == nil {
				t.Fatalf("%s opened", name)
			}
		}
	}
	for _, name := range []string{"pr-1", "pr-2"} {
		if h, err := p.Handler(t.Context(), name); err != nil || body(h) != name {
			t.Errorf("%s after the failed loads: %v", name, err)
		}
	}
	if n := builds.Load(); n != 2 {
		t.Errorf("%d builds, want the 2 of the first loads", n)
	}
}
