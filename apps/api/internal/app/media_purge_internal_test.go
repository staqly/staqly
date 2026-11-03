package app

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/blob"
)

type beforeMediaDeleteStore struct {
	blob.Store
	before func()
	err    error
}

func (b beforeMediaDeleteStore) Delete(ctx context.Context, key string) error {
	if b.before != nil {
		b.before()
	}
	if b.err != nil {
		return b.err
	}
	return b.Store.Delete(ctx, key)
}

func TestRetentionClaimsMediaBeforeDeletingFile(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := t.Context()
	m, err := s.UploadMedia(ctx, p, "photo.png", feedbackPNG(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TrashMedia(ctx, p, m.ID); err != nil {
		t.Fatal(err)
	}
	now := s.Now().Add((MediaTrashDays + 1) * 24 * time.Hour)
	s.Now = func() time.Time { return now }
	restored := false
	var once sync.Once
	store := s.Cfg.Blob
	s.Cfg.Blob = beforeMediaDeleteStore{Store: store, before: func() {
		once.Do(func() {
			restored = true
			_, err := s.RestoreMedia(ctx, p, m.ID)
			if !isCode(err, "conflict") {
				t.Errorf("restore while retention deletes the file: %v, want conflict", err)
			}
		})
	}}
	count, err := s.purgeTrashedMedia(ctx, now)
	if err != nil || count != 1 || !restored {
		t.Fatalf("purge: %d %v restore attempted %v", count, err, restored)
	}
	if _, err := s.GetMediaInfo(ctx, p, m.ID); !isNotFound(err) {
		t.Fatalf("purged media still present: %v", err)
	}
}

func TestRetentionRetriesClaimedMediaAfterStorageFailure(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := t.Context()
	m, err := s.UploadMedia(ctx, p, "photo.png", feedbackPNG(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TrashMedia(ctx, p, m.ID); err != nil {
		t.Fatal(err)
	}
	now := s.Now().Add((MediaTrashDays + 1) * 24 * time.Hour)
	s.Now = func() time.Time { return now }
	store := s.Cfg.Blob
	s.Cfg.Blob = beforeMediaDeleteStore{Store: store, err: errors.New("object store unavailable")}
	count, err := s.purgeTrashedMedia(ctx, now)
	if err != nil || count != 0 {
		t.Fatalf("failed purge: %d %v", count, err)
	}
	if _, err := s.GetMediaInfo(ctx, p, m.ID); err != nil {
		t.Fatalf("retryable row lost: %v", err)
	}
	_, err = s.RestoreMedia(ctx, p, m.ID)
	if !isCode(err, "conflict") {
		t.Fatalf("restore claimed file: %v, want conflict", err)
	}
	f, _, err := s.OpenMedia(ctx, p, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil || len(data) == 0 {
		t.Fatalf("storage failure lost file: %v", err)
	}
	s.Cfg.Blob = store
	count, err = s.purgeTrashedMedia(ctx, now)
	if err != nil || count != 1 {
		t.Fatalf("retry purge: %d %v", count, err)
	}
}
