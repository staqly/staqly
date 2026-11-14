package app

import (
	"context"
	"errors"
	"testing"
)

func TestArchivedPostsLeaveTheList(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	listed := func(include bool) bool {
		page, err := s.ListPosts(ctx, p, PostFilter{IncludeArchived: include})
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range page.Data {
			if x.ID == post.ID {
				return true
			}
		}
		return false
	}
	var e *Error
	if _, err := s.ArchivePost(ctx, p, post.ID, true); !errors.As(err, &e) || e.Status != 409 {
		t.Fatalf("archiving a scheduled post: %v", err)
	}
	if _, err := s.CancelPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ArchivePost(ctx, p, post.ID, true)
	if err != nil || got.ArchivedAt == nil {
		t.Fatalf("archive %+v %v", got.ArchivedAt, err)
	}
	if listed(false) || !listed(true) {
		t.Fatal("an archived post shows only with include_archived")
	}
	if got, err = s.ArchivePost(ctx, p, post.ID, false); err != nil || got.ArchivedAt != nil {
		t.Fatalf("unarchive %+v %v", got.ArchivedAt, err)
	}
	if !listed(false) {
		t.Fatal("an unarchived post is back in the list")
	}
}
