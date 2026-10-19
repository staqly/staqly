package blob

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// incoming is the uploads' temp folder inside the root: on the same filesystem, so Put renames.
const incoming = ".incoming"

// Disk keeps files under Root (UPLOAD_DIR), one file per key.
type Disk struct{ Root string }

func NewDisk(root string) *Disk { return &Disk{Root: root} }

func (d *Disk) Name() string { return "disk" }

func (d *Disk) path(key string) (string, error) {
	k, err := cleanKey(key)
	if err != nil {
		return "", err
	}
	return filepath.Join(d.Root, filepath.FromSlash(k)), nil
}

func (d *Disk) TempDir() string { return filepath.Join(d.Root, incoming) }

func (d *Disk) Check(context.Context, bool) error {
	return os.MkdirAll(d.TempDir(), 0o750)
}

func (d *Disk) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	// An upload already written to TempDir moves into place.
	if f, ok := r.(*os.File); ok && filepath.Dir(f.Name()) == d.TempDir() {
		if err := os.Rename(f.Name(), p); err == nil {
			return os.Chmod(p, 0o640)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	_, err = io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o640)
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (d *Disk) Open(_ context.Context, key string) (io.ReadSeekCloser, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotExist
	}
	return f, err
}

func (d *Disk) Stat(_ context.Context, key string) (int64, error) {
	p, err := d.path(key)
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, ErrNotExist
	}
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func (d *Disk) Delete(_ context.Context, key string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (d *Disk) Walk(ctx context.Context, prefix string, fn func(string, int64) error) error {
	err := filepath.WalkDir(d.Root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if strings.HasPrefix(e.Name(), ".") && p != d.Root { // .incoming, half-written .put-* files
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.IsDir() {
			return ctx.Err()
		}
		rel, err := filepath.Rel(d.Root, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) {
			return nil
		}
		fi, err := e.Info()
		if err != nil {
			return err
		}
		return fn(key, fi.Size())
	})
	return err
}

func (d *Disk) SignedURL(context.Context, string, time.Duration) (string, error) { return "", nil }
