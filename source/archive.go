package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"testing/fstest"

	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
)

// Archive is one object of a bucket as a documentation source: a
// gzip-compressed tar archive that holds the documentation root, which one
// upload replaces whole. Its listing is that object alone, so a snapshot is
// one version of the documentation by construction, and a check reads the
// object's attributes rather than listing a folder.
type Archive struct {
	bucket *blob.Bucket
	key    string
	limit  int64 // the size limit in bytes of the unpacked content; 0 means none
}

// NewArchive makes the object at key in bucket a documentation source,
// whose unpacked content holds at most limit bytes; a limit of 0 means no
// limit.
func NewArchive(bucket *blob.Bucket, key string, limit int64) *Archive {
	return &Archive{bucket: bucket, key: key, limit: limit}
}

// OpenArchive opens the bucket at the Go CDK URL rawURL, such as
// gs://docs?prefix=portal/, with the drivers the program has registered,
// and makes its object at key, such as published.tgz, a documentation
// source whose unpacked content holds at most limit bytes.
func OpenArchive(ctx context.Context, rawURL, key string, limit int64) (*Archive, error) {
	bucket, err := blob.OpenBucket(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return NewArchive(bucket, key, limit), nil
}

// Object returns the archive at another key of the same bucket, with the
// same size limit, such as a preview's archive beside the published one.
func (a *Archive) Object(key string) *Archive {
	return &Archive{bucket: a.bucket, key: key, limit: a.limit}
}

// List returns the archive's object, with its size, modification time and
// MD5 sum as the bucket reports them, or an empty listing when the bucket
// holds no such object, as a folder without objects lists.
func (a *Archive) List(ctx context.Context) (Listing, error) {
	attrs, err := a.bucket.Attributes(ctx, a.key)
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("check the archive %s: %w", a.key, err)
	}
	return Listing{{Key: a.key, Size: attrs.Size, ModTime: attrs.ModTime, MD5: attrs.MD5}}, nil
}

// Read reads the archive of listing, as List returned it, and unpacks it
// into a documentation root handle held in memory; an empty listing, from a
// bucket without the object, reads as an empty root. It refuses an object
// whose size or MD5 sum differs from the listing's, which changed since the
// listing; unpacked content over the size limit; and an entry that is not a
// file, that is not a valid path, or that names both a file and a
// directory. A leading "./" comes off each name, and a directory entry is
// skipped, so an archive packed from "." reads like one packed from a file
// list.
func (a *Archive) Read(ctx context.Context, listing Listing) (fs.FS, error) {
	if len(listing) == 0 {
		// No object yet, as before the first upload: an empty root, as a
		// bucket folder without objects reads, for the program to decide on.
		return fstest.MapFS{}, nil
	}
	if len(listing) != 1 || listing[0].Key != a.key {
		return nil, fmt.Errorf("read the archive %s: the listing names %d objects", a.key, len(listing))
	}
	o := listing[0]
	r, err := a.bucket.NewReader(ctx, a.key, nil)
	if err != nil {
		return nil, fmt.Errorf("read the archive %s: %w", a.key, err)
	}
	defer r.Close()
	// One byte past the listed size tells an object that grew.
	data, err := io.ReadAll(io.LimitReader(r, o.Size+1))
	if err != nil {
		return nil, fmt.Errorf("read the archive %s: %w", a.key, err)
	}
	if int64(len(data)) != o.Size || (len(o.MD5) > 0 && !bytes.Equal(sumOf(data), o.MD5)) {
		return nil, fmt.Errorf("read the archive %s: the object changed since the listing", a.key)
	}
	root, err := unpack(data, a.limit)
	if err != nil {
		return nil, fmt.Errorf("read the archive %s: %w", a.key, err)
	}
	return root, nil
}

// unpack reads the gzip-compressed tar archive data into a root handle
// whose files hold at most limit bytes in all; a limit of 0 means none.
func unpack(data []byte, limit int64) (fs.FS, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gz.Close()
	root := fstest.MapFS{}
	dirs := map[string]bool{} // every directory of a file
	var unpacked int64
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("not a tar archive: %w", err)
		}
		name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		switch {
		case hdr.Typeflag == tar.TypeDir || name == ".":
			continue
		case hdr.Typeflag != tar.TypeReg:
			return nil, fmt.Errorf("the entry %q is not a file", hdr.Name)
		case !fs.ValidPath(name):
			return nil, fmt.Errorf("the entry %q is not a valid path", hdr.Name)
		case root[name] != nil:
			return nil, fmt.Errorf("the entry %q appears twice", hdr.Name)
		}
		// One byte past the allowance tells content over the limit.
		allowance := int64(1 << 62)
		if limit > 0 {
			allowance = limit - unpacked + 1
		}
		content, err := io.ReadAll(io.LimitReader(tr, allowance))
		if err != nil {
			return nil, fmt.Errorf("read the entry %q: %w", hdr.Name, err)
		}
		if unpacked += int64(len(content)); limit > 0 && unpacked > limit {
			return nil, fmt.Errorf("the unpacked content holds over %d bytes, the size limit", limit)
		}
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = true
		}
		root[name] = &fstest.MapFile{Data: content, Mode: 0o444, ModTime: hdr.ModTime}
	}
	for name := range root {
		if dirs[name] {
			return nil, fmt.Errorf("the entry %q names both a file and a directory", name)
		}
	}
	return root, nil
}
