package blob

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config is an S3-compatible bucket (STORAGE_DRIVER=s3).
type S3Config struct {
	Endpoint  string // https://s3.eu-central-1.amazonaws.com, https://<account>.r2.cloudflarestorage.com, http://maxiofs:8080
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool   // bucket in the path (MaxIOFS, MinIO and most self-hosted servers) instead of the host name
	Prefix    string // optional folder in the bucket, e.g. "staqly/"
	PublicURL string // optional public base URL (a CDN in front of a public bucket); used instead of signed URLs
	TempDir   string // where uploads are staged before they are sent (DATA_DIR/tmp)
}

// Missing lists the settings an S3 store can't start without.
func (c S3Config) Missing() []string {
	var out []string
	for name, v := range map[string]string{"S3_ENDPOINT": c.Endpoint, "S3_BUCKET": c.Bucket,
		"S3_ACCESS_KEY_ID": c.AccessKey, "S3_SECRET_ACCESS_KEY": c.SecretKey} {
		if v == "" {
			out = append(out, name)
		}
	}
	return out
}

// S3 keeps files in a bucket through minio-go (any S3-compatible server, SigV4).
type S3 struct {
	c   *minio.Client
	cfg S3Config
}

func NewS3(cfg S3Config) (*S3, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("S3_ENDPOINT must be an http(s) URL, got %q", cfg.Endpoint)
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	c, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       u.Scheme == "https",
		Region:       region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}
	if cfg.TempDir == "" {
		cfg.TempDir = os.TempDir()
	}
	if cfg.Prefix != "" && !strings.HasSuffix(cfg.Prefix, "/") {
		cfg.Prefix += "/"
	}
	return &S3{c: c, cfg: cfg}, nil
}

func (s *S3) Name() string    { return "s3" }
func (s *S3) TempDir() string { return s.cfg.TempDir }

func (s *S3) object(key string) (string, error) {
	k, err := cleanKey(key)
	if err != nil {
		return "", err
	}
	return s.cfg.Prefix + k, nil
}

func notFound(err error) bool {
	return minio.ToErrorResponse(err).StatusCode == 404
}

func (s *S3) Check(ctx context.Context, create bool) error {
	if err := os.MkdirAll(s.cfg.TempDir, 0o750); err != nil {
		return err
	}
	ok, err := s.c.BucketExists(ctx, s.cfg.Bucket)
	if err != nil {
		return fmt.Errorf("S3 bucket %q is not reachable at %s: %w", s.cfg.Bucket, s.cfg.Endpoint, err)
	}
	if !ok {
		if !create {
			return fmt.Errorf("S3 bucket %q does not exist at %s (create it, or set S3_CREATE_BUCKET=true)", s.cfg.Bucket, s.cfg.Endpoint)
		}
		if err := s.c.MakeBucket(ctx, s.cfg.Bucket, minio.MakeBucketOptions{Region: s.cfg.Region}); err != nil {
			return fmt.Errorf("create S3 bucket %q: %w", s.cfg.Bucket, err)
		}
	}
	return nil
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	obj, err := s.object(key)
	if err != nil {
		return err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err = s.c.PutObject(ctx, s.cfg.Bucket, obj, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	obj, err := s.object(key)
	if err != nil {
		return nil, err
	}
	o, err := s.c.GetObject(ctx, s.cfg.Bucket, obj, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// GetObject is lazy: Stat surfaces a missing key now. The object then reads (and seeks) with ranged GETs.
	if _, err := o.Stat(); err != nil {
		o.Close()
		if notFound(err) {
			return nil, ErrNotExist
		}
		return nil, err
	}
	return o, nil
}

func (s *S3) Stat(ctx context.Context, key string) (int64, error) {
	obj, err := s.object(key)
	if err != nil {
		return 0, err
	}
	info, err := s.c.StatObject(ctx, s.cfg.Bucket, obj, minio.StatObjectOptions{})
	if err != nil {
		if notFound(err) {
			return 0, ErrNotExist
		}
		return 0, err
	}
	return info.Size, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	obj, err := s.object(key)
	if err != nil {
		return err
	}
	err = s.c.RemoveObject(ctx, s.cfg.Bucket, obj, minio.RemoveObjectOptions{})
	if err != nil && notFound(err) {
		return nil
	}
	return err
}

func (s *S3) Walk(ctx context.Context, prefix string, fn func(string, int64) error) error {
	for o := range s.c.ListObjects(ctx, s.cfg.Bucket, minio.ListObjectsOptions{Prefix: s.cfg.Prefix + prefix, Recursive: true}) {
		if o.Err != nil {
			return o.Err
		}
		if err := fn(strings.TrimPrefix(o.Key, s.cfg.Prefix), o.Size); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *S3) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	obj, err := s.object(key)
	if err != nil {
		return "", err
	}
	if s.cfg.PublicURL != "" {
		// A public bucket behind a CDN: its plain URL (a signature would not match the CDN's host).
		return strings.TrimRight(s.cfg.PublicURL, "/") + "/" + (&url.URL{Path: obj}).EscapedPath(), nil
	}
	u, err := s.c.PresignedGetObject(ctx, s.cfg.Bucket, obj, ttl, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
