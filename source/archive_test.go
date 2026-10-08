package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"gocloud.dev/blob"
)

// entry is one entry of a test archive.
type entry struct {
	name string
	body string
	typ  byte // 0 for a file
}

// tgz packs entries into a gzip-compressed tar archive.
func tgz(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o644, Size: int64(len(e.body)), ModTime: time.Unix(1_700_000_000, 0)}
		if typ == tar.TypeSymlink {
			hdr.Linkname, hdr.Size = e.body, 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// archiveBucket returns a bucket in memory holding the archive at key.
func archiveBucket(t *testing.T, key string, archive []byte) *blob.Bucket {
	t.Helper()
	b := memBucket(t, nil)
	if err := b.WriteAll(t.Context(), key, archive, nil); err != nil {
		t.Fatal(err)
	}
	return b
}

var docs = []entry{
	{name: "environment.yaml", body: "portals: []\n"},
	{name: "docs/a.md", body: "# A\n"},
	{name: "docs/guide/b.md", body: "# B\n"},
}

func TestArchiveListingIsTheObject(t *testing.T) {
	archive := tgz(t, docs...)
	src := NewArchive(archiveBucket(t, "published.tgz", archive), "published.tgz", 0)

	listing, err := src.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 1 || listing[0].Key != "published.tgz" || listing[0].Size != int64(len(archive)) || listing[0].ModTime.IsZero() || len(listing[0].MD5) == 0 {
		t.Errorf("listing = %+v, want the one object with its size, time and MD5", listing)
	}
}

func TestArchiveListsNothingWithoutTheObject(t *testing.T) {
	src := NewArchive(memBucket(t, nil), "published.tgz", 0)

	listing, err := src.List(t.Context())
	if err != nil {
		t.Fatalf("a missing archive is an empty source, as a folder without objects is, not an error: %v", err)
	}
	if len(listing) != 0 {
		t.Errorf("listing = %+v, want none", listing)
	}
}

func TestArchiveReadsAnEmptyRootBeforeTheFirstUpload(t *testing.T) {
	src := NewArchive(memBucket(t, nil), "published.tgz", 0)

	snap, err := Load(t.Context(), src)
	if err != nil {
		t.Fatalf("before the first upload the source is empty, not broken: %v", err)
	}
	if entries, err := fs.ReadDir(snap.Root, "."); err != nil || len(entries) != 0 {
		t.Errorf("the root holds %v, %v; want nothing", entries, err)
	}
}

func TestArchiveSnapshot(t *testing.T) {
	src := NewArchive(archiveBucket(t, "published.tgz", tgz(t, docs...)), "published.tgz", 0)

	snap, err := Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range docs {
		got, err := fs.ReadFile(snap.Root, e.name)
		if err != nil || string(got) != e.body {
			t.Errorf("%s = %q, %v; want %q", e.name, got, err, e.body)
		}
	}
	if err := fstest.TestFS(snap.Root, "environment.yaml", "docs/a.md", "docs/guide/b.md"); err != nil {
		t.Error(err)
	}
}

func TestArchivePackedFromDotReadsTheSame(t *testing.T) {
	// tar -czf docs.tgz . writes "./", "./docs/" and "./docs/a.md".
	src := NewArchive(archiveBucket(t, "published.tgz", tgz(t,
		entry{name: "./", typ: tar.TypeDir},
		entry{name: "./docs/", typ: tar.TypeDir},
		entry{name: "./docs/a.md", body: "# A\n"},
		entry{name: "./environment.yaml", body: "portals: []\n"},
	)), "published.tgz", 0)

	snap, err := Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := fs.ReadFile(snap.Root, "docs/a.md"); err != nil || string(got) != "# A\n" {
		t.Errorf("docs/a.md = %q, %v", got, err)
	}
	if entries, err := fs.ReadDir(snap.Root, "."); err != nil || len(entries) != 2 {
		t.Errorf("the root holds %v, %v; want docs and environment.yaml alone", entries, err)
	}
}

func TestArchiveRefusesBadEntries(t *testing.T) {
	for name, entries := range map[string][]entry{
		"a path outside the root":      {{name: "../a.md", body: "x"}},
		"an absolute path":             {{name: "/a.md", body: "x"}},
		"a symlink":                    {{name: "a.md", body: "/etc/passwd", typ: tar.TypeSymlink}},
		"a name used twice":            {{name: "a.md", body: "x"}, {name: "a.md", body: "y"}},
		"a file that is also a folder": {{name: "docs", body: "x"}, {name: "docs/a.md", body: "y"}},
	} {
		t.Run(name, func(t *testing.T) {
			src := NewArchive(archiveBucket(t, "published.tgz", tgz(t, entries...)), "published.tgz", 0)
			if _, err := Load(t.Context(), src); err == nil {
				t.Error("the archive was accepted; it must be refused, since the portal would serve it")
			}
		})
	}
}

func TestArchiveRefusesWhatIsNotAnArchive(t *testing.T) {
	src := NewArchive(archiveBucket(t, "published.tgz", []byte("portals: []\n")), "published.tgz", 0)
	if _, err := Load(t.Context(), src); err == nil {
		t.Error("plain text was accepted as an archive")
	}
}

func TestArchiveLimitAppliesToTheUnpackedContent(t *testing.T) {
	// A megabyte of zeros packs into about a kilobyte.
	archive := tgz(t, entry{name: "big.bin", body: strings.Repeat("\x00", 1<<20)})
	if len(archive) > 1<<14 {
		t.Fatalf("the test archive is %d bytes; it was meant to be far smaller than its content", len(archive))
	}
	src := NewArchive(archiveBucket(t, "published.tgz", archive), "published.tgz", 1<<16)

	_, err := Load(t.Context(), src)

	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Errorf("err = %v; the limit must count the unpacked bytes, or a small archive could fill memory", err)
	}
	if _, err := Load(t.Context(), NewArchive(src.bucket, "published.tgz", 2<<20)); err != nil {
		t.Errorf("under the limit: %v", err)
	}
}

func TestArchiveReadRefusesAChangedObject(t *testing.T) {
	b := archiveBucket(t, "published.tgz", tgz(t, docs...))
	src := NewArchive(b, "published.tgz", 0)
	listing, err := src.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.WriteAll(t.Context(), "published.tgz", tgz(t, entry{name: "environment.yaml", body: "portals: [new]\n"}), nil); err != nil {
		t.Fatal(err)
	}

	_, err = src.Read(t.Context(), listing)

	if err == nil {
		t.Error("an object rewritten since the listing was read; the snapshot must come from one version")
	}
}

func TestArchiveReloaderSwapsANewUpload(t *testing.T) {
	b := archiveBucket(t, "published.tgz", tgz(t, entry{name: "index.md", body: "one"}))
	src := NewArchive(b, "published.tgz", 0)
	snap, err := Load(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	var builds atomic.Int64
	build := pageOf(&builds)
	first, err := build(snap.Root)
	if err != nil {
		t.Fatal(err)
	}
	h := NewReloader(t.Context(), src, snap.Listing, first, time.Millisecond, build)

	if err := b.WriteAll(t.Context(), "published.tgz", tgz(t, entry{name: "index.md", body: "two"}), nil); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the new upload", func() bool { return body(h) == "two" })
}

func TestArchivePreviews(t *testing.T) {
	b := archiveBucket(t, "previews/pr-1.tgz", tgz(t, entry{name: "index.md", body: "pr-1"}))
	published := NewArchive(b, "published.tgz", 0)
	folder := func(name string) (Source, error) { return published.Object("previews/" + name + ".tgz"), nil }
	var builds atomic.Int64
	p := NewPreviews(t.Context(), folder, pageOf(&builds), 0, 0, 0)

	h, err := p.Handler(t.Context(), "pr-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := body(h); got != "pr-1" {
		t.Errorf("the preview answers %q", got)
	}

	_, err = p.Handler(t.Context(), "pr-2")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a preview without an archive: err = %v, want one wrapping fs.ErrNotExist, as a folder without objects gives", err)
	}
}
