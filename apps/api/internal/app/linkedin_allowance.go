package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// LinkedIn's API quotas are per day and shared by every workspace of staqly's app (posts and video uploads), so
// a cloud plan includes a monthly allowance of each: every post to a LinkedIn channel (a profile or a Page)
// counts once on MeterLinkedIn, and one with a video once more on MeterLinkedInVideo. Comments (thread replies)
// don't count, and there are no bought posts. Counted in usage_events like X, once per target.
const (
	MeterLinkedIn      = "linkedin"
	MeterLinkedInVideo = "linkedin_video"
)

// isLinkedIn: a LinkedIn channel, a personal profile or a Page.
func isLinkedIn(provider string) bool { return provider == "linkedin" || provider == "linkedin_page" }

// linkedInAllowance is one of the two allowances with what a post would need of it (next: what to do instead).
type linkedInAllowance struct {
	meter, limit, code, what, next string
	max, need                      int64
}

func linkedInAllowances(l PlanLimits, posts, videos int64) []linkedInAllowance {
	return []linkedInAllowance{
		{MeterLinkedIn, LimitLinkedInPosts, "linkedin_posts_exhausted", "LinkedIn posts", "Wait until", l.LinkedInPostsPerPeriod, posts},
		{MeterLinkedInVideo, LimitLinkedInVideos, "linkedin_videos_exhausted", "LinkedIn videos", "Post without a video or wait until",
			l.LinkedInVideosPerPeriod, videos},
	}
}

// checkLinkedIn fails when the workspace can't publish posts more posts to LinkedIn, videos of them with a video
// (a cloud plan's allowances).
func (s *Service) checkLinkedIn(ctx context.Context, q *db.Queries, workspaceID string, posts, videos int64) error {
	if posts <= 0 || s.Cfg.Mode == ModeLocal {
		return nil
	}
	plan, err := s.planFor(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	for _, a := range linkedInAllowances(plan.Limits, posts, videos) {
		if a.need <= 0 || a.max == Unlimited {
			continue
		}
		used, err := q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: workspaceID, Provider: a.meter, Period: s.period()})
		if err != nil {
			return err
		}
		if used+a.need > a.max {
			end := s.monthEnd()
			return &Error{Code: a.code, Status: 402,
				Message: fmt.Sprintf("Your %s for this period are used up. %s %s.", a.what, a.next, end.Format("Jan 2")),
				Details: map[string]any{"limit": a.limit, "needed": a.need, "left": max(a.max-used, 0), "period_end": formatTime(end)}}
		}
	}
	return nil
}

// meterLinkedIn charges a reserved or published LinkedIn post once (ref: post:<post>:<channel>), and once more on the video
// allowance when it has a video. In the caller's transaction; a retried job counts once.
func (s *Service) meterLinkedIn(ctx context.Context, q *db.Queries, workspaceID, ref string, video bool) error {
	if s.Cfg.Mode == ModeLocal {
		return nil
	}
	meters := []string{MeterLinkedIn}
	if video {
		meters = append(meters, MeterLinkedInVideo)
	}
	for _, m := range meters {
		if _, err := q.InsertUsageEvent(ctx, db.InsertUsageEventParams{WorkspaceID: workspaceID, Provider: m, Ref: ref, Period: s.period(),
			Units: 1, Included: 1, CreatedAt: s.now()}); err != nil {
			return err
		}
	}
	return nil
}

// hasVideo: whether a post's media (its JSON list of ids) holds a video.
func hasVideo(ctx context.Context, q *db.Queries, workspaceID, media string) (bool, error) {
	var ids []string
	_ = json.Unmarshal([]byte(media), &ids)
	for _, id := range ids {
		m, err := q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: workspaceID, ID: id})
		if isNoRows(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if strings.HasPrefix(m.MimeType, "video/") {
			return true, nil
		}
	}
	return false, nil
}

// linkedInForPost: the posts (and videos) publishing a post would count on LinkedIn: one on every LinkedIn
// channel it still goes to.
func (s *Service) linkedInForPost(ctx context.Context, q *db.Queries, row db.Post) (int64, int64, error) {
	targets, err := q.ListTargets(ctx, row.ID)
	if err != nil {
		return 0, 0, err
	}
	var posts int64
	for _, t := range targets {
		if t.Status == "published" {
			continue
		}
		if ch, err := q.GetChannelByID(ctx, t.ChannelID); err == nil && isLinkedIn(ch.Provider) {
			posts++
		}
	}
	if posts == 0 {
		return 0, 0, nil
	}
	video, err := hasVideo(ctx, q, row.WorkspaceID, row.Media)
	if err != nil || !video {
		return posts, 0, err
	}
	return posts, posts, nil
}
