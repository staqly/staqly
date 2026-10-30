package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// The workspace's analytics (GET /v1/analytics, MCP get_analytics): what staqly itself knows about publishing,
// aggregated from the workspace's own posts and targets, plus the platform numbers already cached on targets
// (PostMetrics and ReportTargetMetrics store them). It never calls a platform: refreshing numbers stays per post.
// Not to be confused with analytics.go, the operator's opt-in product analytics.

const (
	analyticsDefaultDays = 30
	analyticsMaxDays     = 366
	// analyticsMinSamples: a posting-time slot with fewer posts that have numbers is marked low confidence.
	analyticsMinSamples = 5
	analyticsTopPosts   = 10
	analyticsFailures   = 10
	analyticsMaxFilter  = 100 // channels or sources in one filter
)

// AnalyticsQuery: from and to are days (YYYY-MM-DD) in the workspace's time zone, both included; Days instead of
// From is the last n days up to To (today by default).
type AnalyticsQuery struct {
	From, To   string
	Days       int
	Channels   []string // nil: every channel
	Collection string
	Sources    []string // nil: every source; "" is posts with none recorded
}

type AnalyticsReport struct {
	Range    AnalyticsRange    `json:"range"`
	Totals   AnalyticsTotals   `json:"totals"`
	Previous AnalyticsTotals   `json:"previous"` // the period of the same length right before, same filters
	Queue    AnalyticsQueueNow `json:"queue"`
	Daily    []AnalyticsDay    `json:"daily"`
	Channels []ChannelStats    `json:"channels"`
	Sources  []SourceStats     `json:"sources"`
	Times    PostingTimes      `json:"posting_times"`
	Top      TopPosts          `json:"top_posts"`
	Failures []FailureReason   `json:"failures"`
	Feedback FeedbackStats     `json:"feedback"`
}

type AnalyticsRange struct {
	From         string `json:"from"` // YYYY-MM-DD, included
	To           string `json:"to"`   // YYYY-MM-DD, included
	Days         int    `json:"days"`
	Timezone     string `json:"timezone"`
	Start        string `json:"start"` // the instants: [start, end)
	End          string `json:"end"`
	PreviousFrom string `json:"previous_from"`
	PreviousTo   string `json:"previous_to"`
}

// AnalyticsTotals are period totals. Rates and delays are null when there is nothing to measure.
type AnalyticsTotals struct {
	PostsCreated     int64    `json:"posts_created"`
	PostsPublished   int64    `json:"posts_published"` // posts with at least one channel published in the period
	TargetsPublished int64    `json:"targets_published"`
	TargetsFailed    int64    `json:"targets_failed"`
	SuccessRate      *float64 `json:"success_rate"` // published / (published + failed), 0..1
	Retried          int64    `json:"retried"`      // targets that needed more than one attempt
	DelayAvgSeconds  *float64 `json:"delay_avg_seconds"`
	DelayP90Seconds  *float64 `json:"delay_p90_seconds"`
	// Platform numbers summed over the period's published targets that have them (null: none has).
	Metrics *MetricTotals `json:"metrics"`
}

// AnalyticsQueueNow is what waits right now (not a period figure).
type AnalyticsQueueNow struct {
	Scheduled     int64   `json:"scheduled"`
	Drafts        int64   `json:"drafts"`
	NextPublishAt *string `json:"next_publish_at"`
}

type AnalyticsDay struct {
	Date      string `json:"date"` // YYYY-MM-DD in the workspace's time zone
	Published int64  `json:"published"`
	Failed    int64  `json:"failed"`
}

// MetricTotals is the shared vocabulary over every platform's numbers. Engagements = likes + replies + reposts +
// quotes + bookmarks. A field is null when no target in the sum reported it.
type MetricTotals struct {
	Impressions *int64 `json:"impressions"`
	Engagements int64  `json:"engagements"`
	Likes       *int64 `json:"likes"`
	Replies     *int64 `json:"replies"`
	Reposts     *int64 `json:"reposts"`
	Quotes      *int64 `json:"quotes"`
	Bookmarks   *int64 `json:"bookmarks"`
	LinkClicks  *int64 `json:"link_clicks"`
}

// MetricsCoverage says how many of the period's published targets have numbers and how fresh the newest are,
// so a total is never read as complete when it isn't.
type MetricsCoverage struct {
	Support   string  `json:"support"` // platform (staqly reads them), automation (your automation reports them), none
	Published int64   `json:"published"`
	WithData  int64   `json:"with_data"`
	UpdatedAt *string `json:"updated_at"` // the newest read in the period
}

type ChannelStats struct {
	ChannelID       string          `json:"channel_id"`
	Name            string          `json:"name"`
	Provider        string          `json:"provider"`
	Handle          string          `json:"handle"`
	AvatarURL       string          `json:"avatar_url"`
	Status          string          `json:"status"`
	Published       int64           `json:"published"`
	Failed          int64           `json:"failed"`
	SuccessRate     *float64        `json:"success_rate"`
	LastPublishedAt *string         `json:"last_published_at"` // in the period
	TopFailure      *string         `json:"top_failure"`
	Metrics         *MetricTotals   `json:"metrics"`
	MetricsCoverage MetricsCoverage `json:"metrics_coverage"`
	AvgEngagements  *float64        `json:"avg_engagements"` // per published target with numbers

	acc       metricAccumator
	metricsAt string // the newest metrics_at
}

type SourceStats struct {
	Source           string   `json:"source"` // as stored on posts ("" when none was recorded)
	Kind             string   `json:"kind"`   // dashboard, api, mcp, schedule, repeat, other
	Label            string   `json:"label"`
	PostsCreated     int64    `json:"posts_created"`
	PostsPublished   int64    `json:"posts_published"`
	TargetsPublished int64    `json:"targets_published"`
	TargetsFailed    int64    `json:"targets_failed"`
	SuccessRate      *float64 `json:"success_rate"`
}

// PostingTimes: published targets per weekday and hour in the workspace's zone, with the average engagements of
// those that have numbers. Weekday 0 is Monday.
type PostingTimes struct {
	Cells      []TimeSlot `json:"cells"`
	Best       []TimeSlot `json:"best"` // the slots with the highest average engagements, best first (at most 3)
	MinSamples int        `json:"min_samples"`
}

type TimeSlot struct {
	Weekday        int      `json:"weekday"`
	Hour           int      `json:"hour"`
	Published      int64    `json:"published"`
	Samples        int64    `json:"samples"` // published targets with numbers
	AvgEngagements *float64 `json:"avg_engagements"`
	LowConfidence  bool     `json:"low_confidence"`
}

type TopPosts struct {
	By    string    `json:"by"` // engagements (platform numbers exist) or recent (none do)
	Posts []TopPost `json:"posts"`
}

type TopPost struct {
	PostID      string        `json:"post_id"`
	Excerpt     string        `json:"excerpt"`
	PublishedAt string        `json:"published_at"`
	Channels    []TopPostLink `json:"channels"`
	Metrics     *MetricTotals `json:"metrics"`
}

type TopPostLink struct {
	ChannelID   string `json:"channel_id"`
	Provider    string `json:"provider"`
	ExternalURL string `json:"external_url"`
}

type FailureReason struct {
	Reason       string   `json:"reason"` // the error as the platform or staqly said it, shortened
	Count        int64    `json:"count"`
	Channels     []string `json:"channels"` // channel ids
	LastAt       string   `json:"last_at"`
	SamplePostID string   `json:"sample_post_id"`
}

type FeedbackStats struct {
	Opened   int64 `json:"opened"`   // preview comment threads started in the period
	Resolved int64 `json:"resolved"` // threads resolved in the period
	Open     int64 `json:"open"`     // threads open now
}

type metricAccumator struct {
	t       MetricTotals
	any     bool
	samples int64
}

func addPtr(dst **int64, v *int64) {
	if v == nil {
		return
	}
	if *dst == nil {
		*dst = new(int64)
	}
	**dst += *v
}

func (a *metricAccumator) add(m providers.Metrics) {
	a.any = true
	a.samples++
	addPtr(&a.t.Impressions, m.Impressions)
	addPtr(&a.t.Likes, m.Likes)
	addPtr(&a.t.Replies, m.Replies)
	addPtr(&a.t.Reposts, m.Reposts)
	addPtr(&a.t.Quotes, m.Quotes)
	addPtr(&a.t.Bookmarks, m.Bookmarks)
	addPtr(&a.t.LinkClicks, m.LinkClicks)
	a.t.Engagements += engagements(m)
}

func (a *metricAccumator) totals() *MetricTotals {
	if a == nil || !a.any {
		return nil
	}
	t := a.t
	return &t
}

func engagements(m providers.Metrics) int64 {
	var n int64
	for _, v := range []*int64{m.Likes, m.Replies, m.Reposts, m.Quotes, m.Bookmarks} {
		if v != nil {
			n += *v
		}
	}
	return n
}

func rate(ok, failed int64) *float64 {
	if ok+failed == 0 {
		return nil
	}
	r := float64(ok) / float64(ok+failed)
	return &r
}

// Analytics reports on the workspace's publishing in a range of days (default the last 30, at most 366).
func (s *Service) Analytics(ctx context.Context, p Principal, in AnalyticsQuery) (AnalyticsReport, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return AnalyticsReport{}, err
	}
	loc := s.workspaceLocation(ctx, s.Q, p.WorkspaceID)
	today := s.Now().In(loc)
	day := func(field, v string, def time.Time) (time.Time, error) {
		if v == "" {
			return time.Date(def.Year(), def.Month(), def.Day(), 0, 0, 0, 0, loc), nil
		}
		t, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return time.Time{}, invalid(field, "must be a day such as 2026-10-01")
		}
		return t, nil
	}
	toDay, err := day("to", in.To, today)
	if err != nil {
		return AnalyticsReport{}, err
	}
	n := analyticsDefaultDays
	if in.Days != 0 {
		if in.From != "" {
			return AnalyticsReport{}, invalid("days", "send days or from, not both")
		}
		if in.Days < 1 || in.Days > analyticsMaxDays {
			return AnalyticsReport{}, invalid("days", "must be 1 to 366")
		}
		n = in.Days
	}
	fromDay, err := day("from", in.From, toDay.AddDate(0, 0, -(n-1)))
	if err != nil {
		return AnalyticsReport{}, err
	}
	if fromDay.After(toDay) {
		return AnalyticsReport{}, invalid("from", "must not be after to")
	}
	days := 0
	for d := fromDay; !d.After(toDay); d = d.AddDate(0, 0, 1) {
		days++
		if days > analyticsMaxDays {
			return AnalyticsReport{}, invalid("from", "the range can be at most 366 days")
		}
	}
	start, end := fromDay, toDay.AddDate(0, 0, 1)
	prevStart := fromDay.AddDate(0, 0, -days)
	f := db.AnalyticsFilter{WorkspaceID: p.WorkspaceID}

	channels, err := s.Q.ListChannels(ctx, p.WorkspaceID)
	if err != nil {
		return AnalyticsReport{}, err
	}
	if len(in.Channels) > analyticsMaxFilter || len(in.Sources) > analyticsMaxFilter {
		return AnalyticsReport{}, invalid("channel", fmt.Sprintf("at most %d channels and %d sources", analyticsMaxFilter, analyticsMaxFilter))
	}
	for _, id := range in.Channels {
		if !slices.ContainsFunc(channels, func(c db.Channel) bool { return c.ID == id }) {
			return AnalyticsReport{}, invalid("channel", "no such channel in this workspace")
		}
	}
	if in.Collection != "" {
		if _, err := s.Q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: in.Collection}); isNoRows(err) {
			return AnalyticsReport{}, invalid("collection", "no such collection in this workspace")
		} else if err != nil {
			return AnalyticsReport{}, err
		}
	}
	for _, src := range in.Sources {
		if utf8.RuneCountInString(src) > 200 {
			return AnalyticsReport{}, invalid("source", "must be at most 200 characters")
		}
	}
	f.Collection, f.Channels, f.Sources = in.Collection, in.Channels, in.Sources

	from, to, prevFrom := formatTime(start), formatTime(end), formatTime(prevStart)
	buckets, err := s.Q.AnalyticsTargetBuckets(ctx, f, prevFrom, to)
	if err != nil {
		return AnalyticsReport{}, err
	}
	published, err := s.Q.AnalyticsPublishedTargets(ctx, f, prevFrom, to)
	if err != nil {
		return AnalyticsReport{}, err
	}
	failures, err := s.Q.AnalyticsFailures(ctx, f, from, to)
	if err != nil {
		return AnalyticsReport{}, err
	}
	createdPrev, err := s.Q.AnalyticsPostsCreated(ctx, f, prevFrom, from)
	if err != nil {
		return AnalyticsReport{}, err
	}
	createdNow, err := s.Q.AnalyticsPostsCreated(ctx, f, from, to)
	if err != nil {
		return AnalyticsReport{}, err
	}
	queue, err := s.Q.AnalyticsQueue(ctx, f)
	if err != nil {
		return AnalyticsReport{}, err
	}
	feedback, err := s.Q.AnalyticsFeedback(ctx, f, from, to)
	if err != nil {
		return AnalyticsReport{}, err
	}
	agents, err := s.Q.ListAgents(ctx, p.WorkspaceID)
	if err != nil {
		return AnalyticsReport{}, err
	}

	out := AnalyticsReport{
		Range: AnalyticsRange{From: fromDay.Format("2006-01-02"), To: toDay.Format("2006-01-02"), Days: days, Timezone: loc.String(),
			Start: from, End: to, PreviousFrom: prevStart.Format("2006-01-02"), PreviousTo: fromDay.AddDate(0, 0, -1).Format("2006-01-02")},
		Queue:    AnalyticsQueueNow{Scheduled: queue.Scheduled, Drafts: queue.Drafts},
		Feedback: FeedbackStats{Opened: feedback.Opened, Resolved: feedback.Resolved, Open: feedback.Open},
		Times:    PostingTimes{MinSamples: analyticsMinSamples},
	}
	if queue.NextPublishAt.Valid {
		out.Queue.NextPublishAt = &queue.NextPublishAt.String
	}

	matchChannel := func(id string) bool { return in.Channels == nil || slices.Contains(in.Channels, id) }
	matchSource := func(src string) bool { return in.Sources == nil || slices.Contains(in.Sources, src) }
	inPeriod := func(ts string) bool { return ts >= from }

	// Channels: every channel of the workspace (one without posts in the range shows zeros).
	byChannel := map[string]*ChannelStats{}
	var channelOrder []string
	for _, c := range channels {
		if !matchChannel(c.ID) {
			continue
		}
		support := "none"
		if e, ok := providers.Get(c.Provider); ok {
			if _, reads := e.Provider.(providers.MetricsReader); reads {
				support = "platform"
			}
		}
		if c.Provider == "custom" {
			support = "automation"
		}
		byChannel[c.ID] = &ChannelStats{ChannelID: c.ID, Name: c.Name, Provider: c.Provider, Handle: c.Handle, AvatarURL: c.AvatarUrl,
			Status: c.Status, MetricsCoverage: MetricsCoverage{Support: support}}
		channelOrder = append(channelOrder, c.ID)
	}

	sourceKinds := map[string]string{}
	for _, a := range agents {
		if k, seen := sourceKinds[a.Name]; !seen || k == "api" {
			sourceKinds[a.Name] = a.Kind // mcp wins when the same name was seen both ways
		}
	}
	bySource := map[string]*SourceStats{}
	source := func(src string) *SourceStats {
		if st := bySource[src]; st != nil {
			return st
		}
		kind, label := classifySource(src, sourceKinds)
		st := &SourceStats{Source: src, Kind: kind, Label: label}
		bySource[src] = st
		return st
	}
	for _, c := range createdNow {
		source(c.Source).PostsCreated += c.N
	}
	out.Totals.PostsCreated, out.Previous.PostsCreated = sumCreated(createdNow, in.Sources), sumCreated(createdPrev, in.Sources)

	// Daily series and posting times from the grouped targets.
	dayIndex := map[string]int{}
	for d := fromDay; d.Before(end); d = d.AddDate(0, 0, 1) {
		dayIndex[d.Format("2006-01-02")] = len(out.Daily)
		out.Daily = append(out.Daily, AnalyticsDay{Date: d.Format("2006-01-02")})
	}
	type slotKey struct{ wd, h int }
	slots := map[slotKey]*TimeSlot{}
	slot := func(t time.Time) *TimeSlot {
		k := slotKey{(int(t.Weekday()) + 6) % 7, t.Hour()}
		if slots[k] == nil {
			slots[k] = &TimeSlot{Weekday: k.wd, Hour: k.h}
		}
		return slots[k]
	}
	for _, b := range buckets {
		if !matchChannel(b.ChannelID) {
			continue
		}
		ok, bad := int64(0), int64(0)
		if b.Status == "published" {
			ok = b.N
		} else {
			bad = b.N
		}
		minute, perr := time.Parse("2006-01-02T15:04", b.Minute)
		if perr != nil {
			continue
		}
		local := minute.In(loc)
		if !inPeriod(b.Minute) {
			if matchSource(b.Source) {
				out.Previous.TargetsPublished += ok
				out.Previous.TargetsFailed += bad
				out.Previous.Retried += b.Retried
			}
			continue
		}
		st := source(b.Source)
		st.TargetsPublished += ok
		st.TargetsFailed += bad
		if !matchSource(b.Source) {
			continue
		}
		out.Totals.TargetsPublished += ok
		out.Totals.TargetsFailed += bad
		out.Totals.Retried += b.Retried
		if i, found := dayIndex[local.Format("2006-01-02")]; found {
			out.Daily[i].Published += ok
			out.Daily[i].Failed += bad
		}
		if ok > 0 {
			slot(local).Published += ok
		}
		if cs := byChannel[b.ChannelID]; cs != nil {
			cs.Published += ok
			cs.Failed += bad
			if ok > 0 && (cs.LastPublishedAt == nil || b.Last > *cs.LastPublishedAt) {
				last := b.Last
				cs.LastPublishedAt = &last
			}
		}
	}

	// Published targets: distinct posts, the delay, platform numbers, top posts.
	type postAgg struct {
		id, first string
		links     []TopPostLink
		m         metricAccumator
	}
	var delays, prevDelays []float64
	totalM, prevM := &metricAccumator{}, &metricAccumator{}
	postsNow, postsPrev := map[string]bool{}, map[string]bool{}
	postsBySource := map[string]map[string]bool{}
	posts := map[string]*postAgg{}
	var postOrder []string
	providerOf := map[string]string{}
	for _, c := range channels {
		providerOf[c.ID] = c.Provider
	}
	for _, t := range published {
		if !matchChannel(t.ChannelID) {
			continue
		}
		now := inPeriod(t.PublishedAt)
		if now {
			if postsBySource[t.Source] == nil {
				postsBySource[t.Source] = map[string]bool{}
			}
			postsBySource[t.Source][t.PostID] = true
		}
		if !matchSource(t.Source) {
			continue
		}
		var m *providers.Metrics
		if t.Metrics != "" {
			var cached providers.Metrics
			if json.Unmarshal([]byte(t.Metrics), &cached) == nil {
				m = &cached
			}
		}
		delay := -1.0
		if t.PublishAt.Valid {
			at, e1 := time.Parse(timeFormat, t.PublishAt.String)
			done, e2 := time.Parse(timeFormat, t.PublishedAt)
			// Published before its time (by hand, ahead of the schedule) is no queue delay.
			if e1 == nil && e2 == nil && !done.Before(at) {
				delay = done.Sub(at).Seconds()
			}
		}
		if !now {
			postsPrev[t.PostID] = true
			if delay >= 0 {
				prevDelays = append(prevDelays, delay)
			}
			if m != nil {
				prevM.add(*m)
			}
			continue
		}
		postsNow[t.PostID] = true
		if delay >= 0 {
			delays = append(delays, delay)
		}
		pa := posts[t.PostID]
		if pa == nil {
			pa = &postAgg{id: t.PostID, first: t.PublishedAt}
			posts[t.PostID] = pa
			postOrder = append(postOrder, t.PostID)
		}
		pa.links = append(pa.links, TopPostLink{ChannelID: t.ChannelID, Provider: providerOf[t.ChannelID], ExternalURL: t.ExternalURL})
		cs := byChannel[t.ChannelID]
		if cs != nil {
			cs.MetricsCoverage.Published++
		}
		if m == nil {
			continue
		}
		totalM.add(*m)
		pa.m.add(*m)
		if cs != nil {
			cs.MetricsCoverage.WithData++
			cs.acc.add(*m)
			if t.MetricsAt.Valid && t.MetricsAt.String > cs.metricsAt {
				cs.metricsAt = t.MetricsAt.String
			}
		}
		if at, perr := time.Parse(timeFormat, t.PublishedAt); perr == nil {
			sl := slot(at.In(loc))
			sl.Samples++
			v := float64(engagements(*m))
			if sl.AvgEngagements == nil {
				sl.AvgEngagements = new(float64)
			}
			*sl.AvgEngagements += v // a sum until the averages are taken below
		}
	}
	out.Totals.PostsPublished, out.Previous.PostsPublished = int64(len(postsNow)), int64(len(postsPrev))
	out.Totals.Metrics, out.Previous.Metrics = totalM.totals(), prevM.totals()
	out.Totals.DelayAvgSeconds, out.Totals.DelayP90Seconds = delayStats(delays)
	out.Previous.DelayAvgSeconds, out.Previous.DelayP90Seconds = delayStats(prevDelays)
	out.Totals.SuccessRate = rate(out.Totals.TargetsPublished, out.Totals.TargetsFailed)
	out.Previous.SuccessRate = rate(out.Previous.TargetsPublished, out.Previous.TargetsFailed)
	for src, ids := range postsBySource {
		source(src).PostsPublished = int64(len(ids))
	}

	// Channels out, busiest first.
	for _, id := range channelOrder {
		cs := byChannel[id]
		cs.SuccessRate = rate(cs.Published, cs.Failed)
		cs.Metrics = cs.acc.totals()
		if cs.Metrics != nil {
			avg := math.Round(float64(cs.Metrics.Engagements)/float64(cs.acc.samples)*10) / 10
			cs.AvgEngagements = &avg
		}
		if cs.metricsAt != "" {
			cs.MetricsCoverage.UpdatedAt = &cs.metricsAt
		}
		out.Channels = append(out.Channels, *cs)
	}
	slices.SortStableFunc(out.Channels, func(a, b ChannelStats) int {
		if d := (b.Published + b.Failed) - (a.Published + a.Failed); d != 0 {
			return sign(d)
		}
		return strings.Compare(a.Name, b.Name)
	})

	// Sources out: those with posts created or targets finished in the period, most active first.
	for _, st := range bySource {
		if st.PostsCreated == 0 && st.TargetsPublished == 0 && st.TargetsFailed == 0 {
			continue
		}
		st.SuccessRate = rate(st.TargetsPublished, st.TargetsFailed)
		out.Sources = append(out.Sources, *st)
	}
	slices.SortFunc(out.Sources, func(a, b SourceStats) int {
		if d := (b.TargetsPublished + b.TargetsFailed + b.PostsCreated) - (a.TargetsPublished + a.TargetsFailed + a.PostsCreated); d != 0 {
			return sign(d)
		}
		return strings.Compare(a.Label, b.Label)
	})

	// Posting times: averages, low confidence under the minimum sample, the best three slots.
	for _, sl := range slots {
		if sl.AvgEngagements != nil && sl.Samples > 0 {
			*sl.AvgEngagements = math.Round(*sl.AvgEngagements/float64(sl.Samples)*10) / 10
		}
		sl.LowConfidence = sl.Samples < analyticsMinSamples
		out.Times.Cells = append(out.Times.Cells, *sl)
	}
	slices.SortFunc(out.Times.Cells, func(a, b TimeSlot) int {
		if a.Weekday != b.Weekday {
			return a.Weekday - b.Weekday
		}
		return a.Hour - b.Hour
	})
	for _, c := range out.Times.Cells {
		if c.AvgEngagements != nil {
			out.Times.Best = append(out.Times.Best, c)
		}
	}
	slices.SortStableFunc(out.Times.Best, func(a, b TimeSlot) int {
		// Confident slots first, then by average.
		if a.LowConfidence != b.LowConfidence {
			if a.LowConfidence {
				return 1
			}
			return -1
		}
		switch {
		case *a.AvgEngagements > *b.AvgEngagements:
			return -1
		case *a.AvgEngagements < *b.AvgEngagements:
			return 1
		}
		return 0
	})
	if len(out.Times.Best) > 3 {
		out.Times.Best = out.Times.Best[:3]
	}

	// Top posts: by engagements where numbers exist, else the most recent.
	out.Top.By = "recent"
	ranked := slices.Clone(postOrder)
	if totalM.any {
		out.Top.By = "engagements"
		ranked = slices.DeleteFunc(ranked, func(id string) bool { return !posts[id].m.any })
		slices.SortStableFunc(ranked, func(a, b string) int {
			if d := posts[b].m.t.Engagements - posts[a].m.t.Engagements; d != 0 {
				return sign(d)
			}
			return strings.Compare(posts[b].first, posts[a].first)
		})
	} else {
		slices.Reverse(ranked)
	}
	if len(ranked) > analyticsTopPosts {
		ranked = ranked[:analyticsTopPosts]
	}
	excerpts, err := s.Q.AnalyticsPostExcerpts(ctx, p.WorkspaceID, ranked)
	if err != nil {
		return AnalyticsReport{}, err
	}
	for _, id := range ranked {
		pa := posts[id]
		out.Top.Posts = append(out.Top.Posts, TopPost{PostID: id, Excerpt: excerpt(excerpts[id]), PublishedAt: pa.first, Channels: pa.links,
			Metrics: pa.m.totals()})
	}

	// Failure reasons: the same error with different ids or numbers is one reason.
	type reasonAgg struct {
		FailureReason
		channels map[string]bool
	}
	reasons := map[string]*reasonAgg{}
	var reasonOrder []string
	topByChannel := map[string]map[string]int64{}
	for _, g := range failures {
		if !matchChannel(g.ChannelID) || !matchSource(g.Source) {
			continue
		}
		key, text := failureKey(g.Error)
		r := reasons[key]
		if r == nil {
			r = &reasonAgg{FailureReason: FailureReason{Reason: text}, channels: map[string]bool{}}
			reasons[key] = r
			reasonOrder = append(reasonOrder, key)
		}
		r.Count += g.N
		r.channels[g.ChannelID] = true
		if g.Last > r.LastAt {
			r.LastAt, r.SamplePostID = g.Last, g.PostID
		}
		if topByChannel[g.ChannelID] == nil {
			topByChannel[g.ChannelID] = map[string]int64{}
		}
		topByChannel[g.ChannelID][key] += g.N
	}
	for _, key := range reasonOrder {
		r := reasons[key]
		for id := range r.channels {
			r.Channels = append(r.Channels, id)
		}
		slices.Sort(r.Channels)
		out.Failures = append(out.Failures, r.FailureReason)
	}
	slices.SortStableFunc(out.Failures, func(a, b FailureReason) int {
		if d := b.Count - a.Count; d != 0 {
			return sign(d)
		}
		return strings.Compare(b.LastAt, a.LastAt)
	})
	if len(out.Failures) > analyticsFailures {
		out.Failures = out.Failures[:analyticsFailures]
	}
	for i := range out.Channels {
		best, n := "", int64(0)
		for key, c := range topByChannel[out.Channels[i].ChannelID] {
			if c > n || (c == n && key < best) {
				best, n = key, c
			}
		}
		if best != "" {
			text := reasons[best].Reason
			out.Channels[i].TopFailure = &text
		}
	}

	if out.Channels == nil {
		out.Channels = []ChannelStats{}
	}
	if out.Sources == nil {
		out.Sources = []SourceStats{}
	}
	if out.Times.Cells == nil {
		out.Times.Cells = []TimeSlot{}
	}
	if out.Times.Best == nil {
		out.Times.Best = []TimeSlot{}
	}
	if out.Top.Posts == nil {
		out.Top.Posts = []TopPost{}
	}
	if out.Failures == nil {
		out.Failures = []FailureReason{}
	}
	return out, nil
}

func sumCreated(rows []db.SourceCount, sources []string) int64 {
	var n int64
	for _, c := range rows {
		if sources == nil || slices.Contains(sources, c.Source) {
			n += c.N
		}
	}
	return n
}

func sign(d int64) int {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0
}

// delayStats: the average and 90th percentile (nearest rank) of how late targets went out.
func delayStats(v []float64) (*float64, *float64) {
	if len(v) == 0 {
		return nil, nil
	}
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	avg := math.Round(sum/float64(len(v))*10) / 10
	sorted := slices.Clone(v)
	slices.Sort(sorted)
	p90 := math.Round(sorted[int(math.Ceil(0.9*float64(len(sorted))))-1]*10) / 10
	return &avg, &p90
}

// classifySource names where posts come from: the dashboard, an agent over MCP or the API (by the agents the
// workspace has seen), a schedule that fetches posts from an endpoint, a repeating post, or another client.
func classifySource(src string, agentKinds map[string]string) (kind, label string) {
	switch {
	case src == "dashboard":
		return "dashboard", "Dashboard"
	case src == "repeat":
		return "repeat", "Repeating posts"
	case strings.HasPrefix(src, "schedule:"):
		return "schedule", strings.TrimPrefix(src, "schedule:")
	case src == "":
		return "other", "Not recorded"
	}
	if k := agentKinds[src]; k == "mcp" || k == "api" {
		return k, src
	}
	return "api", src // a source a client sent with the post
}

var (
	reFailURL    = regexp.MustCompile(`https?://\S+`)
	reFailNumber = regexp.MustCompile(`\d+`)
	reFailSpace  = regexp.MustCompile(`\s+`)
)

// failureKey groups errors that differ only by numbers, ids or links; text is the first one, shortened.
func failureKey(msg string) (key, text string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "No reason recorded"
	}
	text = truncateRunes(reFailSpace.ReplaceAllString(msg, " "), 160)
	key = strings.ToLower(reFailURL.ReplaceAllString(msg, "<url>"))
	key = reFailNumber.ReplaceAllString(key, "#")
	key = truncateRunes(reFailSpace.ReplaceAllString(key, " "), 120)
	return key, text
}

func excerpt(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	return truncateRunes(content, 140)
}
