package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
)

// PurgePost on PostgreSQL may run its transaction again (serialization retry): only the run that commits
// counts, so a file is counted (and removed) once, and one kept on the retry is not removed after the commit.
func TestPurgePostCountsOnlyTheCommittedRun(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	m, err := s.UploadMedia(ctx, p, "one.png", &img)
	if err != nil {
		t.Fatal(err)
	}
	withMedia, err := s.CreatePost(ctx, p, PostInput{Collection: post.CollectionID, Content: "with a file", Media: []string{m.ID}})
	if err != nil {
		t.Fatal(err)
	}
	txRehearse = true
	defer func() { txRehearse = false }()
	res, err := s.PurgePost(ctx, p, withMedia.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.MediaDeleted != 1 {
		t.Fatalf("media_deleted: %d, want 1 (a rerun of the transaction must not add up)", res.MediaDeleted)
	}
}

// A scheduled post whose publish_post job runs out of attempts is failed with post.failed (which alerts),
// not left "scheduled" with nothing to publish it; the job gets the usual attempts first.
func TestScheduledPostFailsWhenItsJobDoes(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	jobs := jobsByKey(t, s, "publish_post:"+post.ID)
	if len(jobs) != 1 || jobs[0].MaxAttempts != jobMaxAttempts {
		t.Fatalf("publish_post job: %+v, want %d attempts", jobs, jobMaxAttempts)
	}
	s.jobFailed(ctx, "publish_post", jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{PostID: post.ID}}, errors.New("database is locked"))
	got, err := s.GetPost(ctx, p, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" {
		t.Fatalf("status %q, want failed", got.Status)
	}
	evs, err := s.ListEvents(ctx, p, EventFilter{PostID: post.ID, Type: "post.failed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs.Data) != 1 || !strings.Contains(fmt.Sprint(evs.Data[0].Data), "database is locked") {
		t.Fatalf("post.failed event: %+v", evs.Data)
	}
}
