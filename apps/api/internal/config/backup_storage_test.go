package config

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCloudS3RequiresSeparateBackupBucket(t *testing.T) {
	for _, bucket := range []string{"", "media"} {
		t.Run("bucket="+bucket, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("STAQLY_MODE", "cloud")
			t.Setenv("DATABASE_URL", "postgres://staqly@db/staqly")
			t.Setenv("AUTH_DEV_SIGNIN", "true")
			t.Setenv("STORAGE_DRIVER", "s3")
			t.Setenv("S3_ENDPOINT", "https://s3.example.com")
			t.Setenv("S3_BUCKET", "media")
			t.Setenv("S3_ACCESS_KEY_ID", "test")
			t.Setenv("S3_SECRET_ACCESS_KEY", "test-secret")
			t.Setenv("BACKUP_S3_BUCKET", bucket)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "BACKUP_S3_BUCKET") {
				t.Fatalf("cloud S3 must require a separate backup bucket: %v", err)
			}
		})
	}
}

func TestBackupStorageUsesPrivateS3OnlyInCloud(t *testing.T) {
	clearEnv(t)
	t.Setenv("STAQLY_MODE", "cloud")
	t.Setenv("DATABASE_URL", "postgres://staqly@db/staqly")
	t.Setenv("AUTH_DEV_SIGNIN", "true")
	t.Setenv("STORAGE_DRIVER", "s3")
	t.Setenv("S3_ENDPOINT", "https://s3.example.com")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("S3_BUCKET", "media")
	t.Setenv("S3_ACCESS_KEY_ID", "test")
	t.Setenv("S3_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("S3_FORCE_PATH_STYLE", "true")
	t.Setenv("S3_PREFIX", "instance/")
	t.Setenv("S3_PUBLIC_URL", "https://public-media.example.com")
	t.Setenv("BACKUP_S3_BUCKET", "backups")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App.BackupBlob == nil || cfg.App.BackupBlob.Name() != "s3" {
		t.Fatal("cloud backups must use S3")
	}
	link, err := cfg.App.BackupBlob.SignedURL(context.Background(), "exp_test.zip", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil || u.Host != "s3.example.com" || u.Path != "/backups/instance/exp_test.zip" || u.Query().Get("X-Amz-Signature") == "" {
		t.Fatal("backup store must use a signed URL in its own bucket, never the media CDN")
	}
	t.Setenv("STAQLY_MODE", "local")
	t.Setenv("DATABASE_URL", "")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App.BackupBlob != nil {
		t.Fatal("local backups must stay on disk")
	}
}
