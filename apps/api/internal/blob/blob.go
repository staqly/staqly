// Package blob stores media files: on local disk (the default, a local install's DATA_DIR/uploads) or in
// any S3-compatible bucket (AWS S3, Cloudflare R2, MaxIOFS in the dev stack, ...).
//
// Keys are relative, slash-separated paths chosen by the server ("workspaces/<id>/media/abcdef.jpg",
// "workspaces/<id>/thumbs/abcdef_320.jpg"),
// the same layout on disk and in a bucket, so moving files between the two is a plain copy. A key is never
// taken from a request: callers look the file up in the database (scoped by workspace) first.
package blob

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// ErrNotExist is returned for a key that has no file.
var ErrNotExist = fs.ErrNotExist

// Store is where media files live.
type Store interface {
	// Put stores r (size bytes, -1 when unknown) under key, replacing any file there.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Open reads a file; it seeks (ranged reads on S3), so it can serve HTTP Range requests.
	Open(ctx context.Context, key string) (io.ReadSeekCloser, error)
	// Stat returns a file's size, or ErrNotExist.
	Stat(ctx context.Context, key string) (int64, error)
	// Delete removes a file; a missing file is not an error.
	Delete(ctx context.Context, key string) error
	// Walk calls fn for every file under prefix ("" for all), in no particular order.
	Walk(ctx context.Context, prefix string, fn func(key string, size int64) error) error
	// SignedURL is a time-limited GET URL for platforms that fetch media themselves; "" when the store has
	// none (disk: the API serves the file).
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	// TempDir is where uploads are written before Put (on the same filesystem as the store for disk, so Put
	// is a rename).
	TempDir() string
	// Check verifies the store is reachable and writable at startup; create makes a missing bucket.
	Check(ctx context.Context, create bool) error
	// Name is "disk" or "s3", for logs.
	Name() string
}

// cleanKey rejects keys that could escape the store's root.
func cleanKey(key string) (string, error) {
	k := path.Clean(strings.TrimPrefix(key, "/"))
	if k == "." || k == "" || strings.HasPrefix(k, "../") || k == ".." || strings.Contains(key, "\\") {
		return "", errors.New("blob: invalid key " + key)
	}
	return k, nil
}
