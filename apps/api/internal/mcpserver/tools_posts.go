package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Posts, channels, schedules, jobs and requested work: the heart of the
// scheduler as tools, thin wrappers over the same services REST uses.

func (t *tools) addPostTools(s *mcp.Server) {
	add(s, t, &mcp.Tool{Name: "list_providers", Description: "The platform catalog: every provider staqly can publish to, whether it is available in this build, how it connects (oauth2 in the dashboard, token here with connect_channel, none for your own automation), its limits (characters, media) and the per-post settings it takes.",
		Annotations: ro()}, t.listProviders)
	add(s, t, &mcp.Tool{Name: "list_channels", Description: "List connected channels (accounts) with provider, handle and status (connected, needs_reconnect, disabled). Requires channels:read.", Annotations: ro()}, t.listChannels)
	add(s, t, &mcp.Tool{Name: "get_channel", Description: "Get one channel. Credentials are never returned. Requires channels:read.", Annotations: ro()}, t.getChannel)
	add(s, t, &mcp.Tool{Name: "connect_channel", Description: "Connect a channel on a token platform (see list_providers credential_fields; e.g. Telegram bot token + chat id, Bluesky handle + app password) or your own automation (provider custom, no fields). OAuth platforms are connected in the dashboard. Requires channels:manage.",
		Annotations: wr()}, t.connectChannel)
	add(s, t, &mcp.Tool{Name: "update_channel", Description: "Rename a channel, change its default settings, turn it off (status disabled) or on (connected), or reconnect a token channel with fresh fields. Requires channels:manage.",
		Annotations: wr()}, t.updateChannel)
	add(s, t, &mcp.Tool{Name: "delete_channel", Description: "Remove a channel; targets on it go with it. Requires channels:manage.", Annotations: destructive()}, t.deleteChannel)
	add(s, t, &mcp.Tool{Name: "test_channel", Description: "Check a channel's stored credentials against its platform. Rate-limited. Requires channels:manage.", Annotations: wr()}, t.testChannel)

	add(s, t, &mcp.Tool{Name: "create_post", Description: "Create a post in a collection: content, media (ids from upload_media), targets (one per channel, each with an optional content override and settings such as title), tags, metadata. status draft (default) or scheduled (needs publish_at, RFC3339 in the future, and a target); publish_now true publishes at once. repeat_every (1d to 6d, 1w, 2w, 1mo) schedules a copy that long after each time the post is published. thread: follow-up parts (content, media, delay_seconds) published as replies under the post on platforms that take replies (X); your own automation receives them with the request. Requires posts:write.",
		Annotations: wr()}, t.createPost)
	add(s, t, &mcp.Tool{Name: "get_post", Description: "Get one post with its targets (status, link, error per channel).", Annotations: ro()}, t.getPost)
	add(s, t, &mcp.Tool{Name: "list_posts", Description: "List posts, optionally by collection, status, channel or tag. Default order is by publish time (unscheduled last); publish_after/publish_before make it the calendar; sort created_desc lists the newest first.", Annotations: ro()}, t.listPosts)
	add(s, t, &mcp.Tool{Name: "search_posts", Description: "Find posts whose content contains a text, with the same filters as list_posts.", Annotations: ro()}, t.searchPosts)
	add(s, t, &mcp.Tool{Name: "update_post", Description: "Edit a draft, scheduled or failed post: content, media, targets (the full list replaces the current one), publish_at (null clears), tags, metadata, collection, repeat_every (an empty string stops repeating; edit the next scheduled copy of a repeating post to stop the chain), thread (replaces the replies; [] clears them). A published post is refused (duplicate it). Pass expected_version to refuse the edit when someone changed it meanwhile. Requires posts:write.",
		Annotations: wr()}, t.updatePost)
	add(s, t, &mcp.Tool{Name: "schedule_post", Description: "Set when a post goes out (RFC3339, in the future) and make it scheduled; the post needs at least one target. Requires posts:publish.",
		Annotations: wr()}, t.schedulePost)
	add(s, t, &mcp.Tool{Name: "publish_post", Description: "Publish a draft, scheduled or failed post now. Requires posts:publish.", Annotations: wr()}, t.publishPost)
	add(s, t, &mcp.Tool{Name: "cancel_post", Description: "Take a scheduled post off the schedule; it becomes a draft again. Requires posts:publish.", Annotations: wr()}, t.cancelPost)
	add(s, t, &mcp.Tool{Name: "duplicate_post", Description: "Copy a post (content, media, tags, metadata, targets) into a new draft. Requires posts:write.", Annotations: wr()}, t.duplicatePost)
	add(s, t, &mcp.Tool{Name: "delete_post", Description: "Delete a post (its event history is kept). Requires posts:delete (posts:write alone is not enough).", Annotations: destructive()}, t.deletePost)
	add(s, t, &mcp.Tool{Name: "get_post_history", Description: "The events of a post, newest first: created, edited, scheduled, published or failed per channel.", Annotations: ro()}, t.postHistory)
	add(s, t, &mcp.Tool{Name: "preview_post", Description: "How each target will carry the post: the text per channel and what its provider objects to (too long, missing title...).", Annotations: ro()}, t.previewPost)
	add(s, t, &mcp.Tool{Name: "report_publish_result", Description: "Close a target on your own automation (provider custom) that waits for a result: status published with the url (and external_id), or failed with the error. Only for work you did yourself. Requires posts:publish.",
		Annotations: wr()}, t.reportResult)
	add(s, t, &mcp.Tool{Name: "retry_target", Description: "Try a failed target once more, now. Requires posts:publish.", Annotations: wr()}, t.retryTarget)
	add(s, t, &mcp.Tool{Name: "list_requested_work", Description: "What people's setup asks your automation for: targets on your own automation waiting for report_publish_result (kind target, with the post and the channel) and schedules that found nothing to publish and ask for posts (kind posts; answer by creating draft posts in that collection). Oldest first.",
		Annotations: ro()}, t.listRequestedWork)
	add(s, t, &mcp.Tool{Name: "list_tags", Description: "List the workspace's post tags with how many posts carry each, most used first.", Annotations: ro()}, t.listTags)

	add(s, t, &mcp.Tool{Name: "list_schedules", Description: "List a collection's (or every) publishing schedules with their next run.", Annotations: ro()}, t.listSchedules)
	add(s, t, &mcp.Tool{Name: "create_schedule", Description: "Create a publishing schedule for a collection: kind once (run_at) or recurring (rule: times like [\"09:00\",\"17:00\"] with optional weekdays 0-6 and tz, or every_minutes), the channels it publishes to, pick (next waiting post, selected post_ids, or all), and what to do when nothing waits (source none, agent = emit schedule.posts_requested, endpoint = POST a signed request to endpoint_url and create the posts it returns). Requires schedules:manage.",
		Annotations: wr()}, t.createSchedule)
	add(s, t, &mcp.Tool{Name: "update_schedule", Description: "Change a schedule (any field of create_schedule, enabled true/false); next_run_at is recomputed. Requires schedules:manage.", Annotations: wr()}, t.updateSchedule)
	add(s, t, &mcp.Tool{Name: "delete_schedule", Description: "Delete a schedule. Requires schedules:manage.", Annotations: destructive()}, t.deleteSchedule)
	add(s, t, &mcp.Tool{Name: "run_schedule", Description: "Run a schedule now (its regular next run is unchanged). Requires schedules:manage.", Annotations: wr()}, t.runSchedule)

	add(s, t, &mcp.Tool{Name: "list_jobs", Description: "The background queue: publish runs, schedule runs and token refreshes with status (queued, running, done, failed, cancelled), attempts and the last error. Newest first; filter by status or kind.", Annotations: ro()}, t.listJobs)
	add(s, t, &mcp.Tool{Name: "retry_job", Description: "Queue a failed job again, now. Requires posts:publish.", Annotations: wr()}, t.retryJob)
}

// ---- providers & channels ----

func (t *tools) listProviders(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	p, err := t.svc.ListProviders(ctx, t.principal(ctx))
	return nil, map[string]any{"providers": p}, err
}

func (t *tools) listChannels(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	c, err := t.svc.ListChannels(ctx, t.principal(ctx))
	return nil, map[string]any{"channels": c}, err
}

type channelRef struct {
	ChannelID string `json:"channel_id" jsonschema:"channel ID (ch_...)"`
}

func (t *tools) getChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetChannel(ctx, t.principal(ctx), in.ChannelID))
}

type connectChannelIn struct {
	Provider string            `json:"provider" jsonschema:"provider id from list_providers (custom, telegram, bluesky, ...)"`
	Name     string            `json:"name,omitempty" jsonschema:"what to call the channel; defaults to the account's name"`
	Fields   map[string]string `json:"fields,omitempty" jsonschema:"the provider's credential_fields by key"`
	Settings map[string]any    `json:"settings,omitempty" jsonschema:"provider-level defaults"`
}

func (t *tools) connectChannel(ctx context.Context, _ *mcp.CallToolRequest, in connectChannelIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.ConnectChannel(ctx, t.principal(ctx), app.ChannelInput{Provider: in.Provider, Name: in.Name, Fields: in.Fields, Settings: in.Settings}))
}

type updateChannelIn struct {
	ChannelID string            `json:"channel_id" jsonschema:"channel ID (ch_...)"`
	Name      *string           `json:"name,omitempty"`
	Settings  map[string]any    `json:"settings,omitempty"`
	Status    *string           `json:"status,omitempty" jsonschema:"connected or disabled"`
	Fields    map[string]string `json:"fields,omitempty" jsonschema:"token platforms: fresh credential fields to reconnect"`
}

func (t *tools) updateChannel(ctx context.Context, _ *mcp.CallToolRequest, in updateChannelIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateChannel(ctx, t.principal(ctx), in.ChannelID, app.ChannelPatch{Name: in.Name, Settings: in.Settings, Status: in.Status, Fields: in.Fields}))
}

func (t *tools) deleteChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.ChannelID}, t.svc.DeleteChannel(ctx, t.principal(ctx), in.ChannelID))
}

func (t *tools) testChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelRef) (*mcp.CallToolResult, any, error) {
	if err := t.allowTest(ctx); err != nil {
		return nil, nil, err
	}
	return nil0(t.svc.TestChannel(ctx, t.principal(ctx), in.ChannelID))
}

// ---- posts ----

type targetIn struct {
	ChannelID string            `json:"channel_id" jsonschema:"channel ID (ch_...)"`
	Content   *string           `json:"content,omitempty" jsonschema:"text for this channel instead of the post's content"`
	Settings  map[string]string `json:"settings,omitempty" jsonschema:"the provider's settings_fields by key (title, subreddit, visibility...)"`
}

func targets(in []targetIn) []app.PostTargetInput {
	out := make([]app.PostTargetInput, 0, len(in))
	for _, t := range in {
		out = append(out, app.PostTargetInput{ChannelID: t.ChannelID, Content: t.Content, Settings: t.Settings})
	}
	return out
}

// threadIn is one reply under the post.
type threadIn struct {
	Content      string   `json:"content,omitempty" jsonschema:"the reply's text"`
	Media        []string `json:"media,omitempty" jsonschema:"media IDs (med_...)"`
	DelaySeconds int64    `json:"delay_seconds,omitempty" jsonschema:"wait this long after the part before (0 to 86400)"`
}

func thread(in []threadIn) []app.ThreadPart {
	out := make([]app.ThreadPart, len(in))
	for i, p := range in {
		out[i] = app.ThreadPart{Content: p.Content, Media: p.Media, DelaySeconds: p.DelaySeconds}
	}
	return out
}

type createPostIn struct {
	Collection  string         `json:"collection" jsonschema:"collection ID (col_...) or exact name"`
	Content     string         `json:"content,omitempty" jsonschema:"the post's text"`
	Media       []string       `json:"media,omitempty" jsonschema:"media IDs (med_...) in order"`
	Targets     []targetIn     `json:"targets,omitempty"`
	PublishAt   string         `json:"publish_at,omitempty" jsonschema:"RFC3339, in the future"`
	Status      string         `json:"status,omitempty" jsonschema:"draft (default) or scheduled"`
	PublishNow  bool           `json:"publish_now,omitempty" jsonschema:"publish right away"`
	Tags        []string       `json:"tags,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty" jsonschema:"free JSON for your own use"`
	ExternalRef string         `json:"external_ref,omitempty" jsonschema:"your own reference"`
	RepeatEvery string         `json:"repeat_every,omitempty" jsonschema:"repeat after publishing: 1d to 6d, 1w, 2w or 1mo (empty: no repeat)"`
	Thread      []threadIn     `json:"thread,omitempty" jsonschema:"replies published under the post, in order (at most 25)"`
}

func (t *tools) createPost(ctx context.Context, _ *mcp.CallToolRequest, in createPostIn) (*mcp.CallToolResult, any, error) {
	pi := app.PostInput{Collection: in.Collection, Content: in.Content, Media: in.Media, Targets: targets(in.Targets), Status: in.Status,
		PublishNow: in.PublishNow, Tags: in.Tags, ExternalRef: in.ExternalRef, RepeatEvery: in.RepeatEvery, Thread: thread(in.Thread)}
	if in.PublishAt != "" {
		pi.PublishAt = &in.PublishAt
	}
	if in.Metadata != nil {
		pi.Metadata, _ = json.Marshal(in.Metadata)
	}
	return nil0(t.svc.CreatePost(ctx, t.principal(ctx), pi))
}

type postRef struct {
	PostID string `json:"post_id" jsonschema:"post ID (post_...)"`
}

func (t *tools) getPost(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetPost(ctx, t.principal(ctx), in.PostID))
}

type listPostsIn struct {
	Collection    string `json:"collection,omitempty" jsonschema:"collection ID or name"`
	Status        string `json:"status,omitempty" jsonschema:"draft, scheduled, publishing, published or failed"`
	ChannelID     string `json:"channel_id,omitempty" jsonschema:"posts with a target on this channel"`
	Tag           string `json:"tag,omitempty"`
	PublishAfter  string `json:"publish_after,omitempty" jsonschema:"RFC3339 or YYYY-MM-DD, inclusive"`
	PublishBefore string `json:"publish_before,omitempty" jsonschema:"RFC3339 or YYYY-MM-DD, exclusive"`
	Sort          string `json:"sort,omitempty" jsonschema:"publish_asc (default), publish_desc, created_desc, created_asc"`
	Cursor        string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
	Limit         int    `json:"limit,omitempty" jsonschema:"page size (default 50, max 200)"`
}

func (t *tools) postFilter(ctx context.Context, in listPostsIn, q string) (app.PostFilter, error) {
	col, err := t.resolveCollection(ctx, in.Collection)
	if err != nil {
		return app.PostFilter{}, err
	}
	return app.PostFilter{CollectionID: col, Status: in.Status, ChannelID: in.ChannelID, Tag: in.Tag, Q: q, PublishAfter: in.PublishAfter,
		PublishBefore: in.PublishBefore, Sort: in.Sort, Cursor: in.Cursor, Limit: in.Limit}, nil
}

func (t *tools) listPosts(ctx context.Context, _ *mcp.CallToolRequest, in listPostsIn) (*mcp.CallToolResult, any, error) {
	f, err := t.postFilter(ctx, in, "")
	if err != nil {
		return nil, nil, err
	}
	return nil0(t.svc.ListPosts(ctx, t.principal(ctx), f))
}

type searchPostsIn struct {
	Query string `json:"query" jsonschema:"text the content contains"`
	listPostsIn
}

func (t *tools) searchPosts(ctx context.Context, _ *mcp.CallToolRequest, in searchPostsIn) (*mcp.CallToolResult, any, error) {
	f, err := t.postFilter(ctx, in.listPostsIn, in.Query)
	if err != nil {
		return nil, nil, err
	}
	return nil0(t.svc.ListPosts(ctx, t.principal(ctx), f))
}

type updatePostIn struct {
	PostID          string         `json:"post_id" jsonschema:"post ID (post_...)"`
	Collection      *string        `json:"collection,omitempty"`
	Content         *string        `json:"content,omitempty"`
	Media           *[]string      `json:"media,omitempty"`
	Targets         *[]targetIn    `json:"targets,omitempty" jsonschema:"replaces the whole list"`
	PublishAt       any            `json:"publish_at,omitempty" jsonschema:"RFC3339 in the future, or null to clear"`
	Tags            *[]string      `json:"tags,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ExternalRef     *string        `json:"external_ref,omitempty"`
	RepeatEvery     *string        `json:"repeat_every,omitempty" jsonschema:"1d to 6d, 1w, 2w or 1mo; an empty string stops repeating"`
	Thread          *[]threadIn    `json:"thread,omitempty" jsonschema:"replaces the replies; [] clears them"`
	ExpectedVersion *int64         `json:"expected_version,omitempty"`
}

func (t *tools) updatePost(ctx context.Context, _ *mcp.CallToolRequest, in updatePostIn) (*mcp.CallToolResult, any, error) {
	pp := app.PostPatch{Collection: in.Collection, Content: in.Content, Media: in.Media, Tags: in.Tags, ExternalRef: in.ExternalRef, ExpectedVersion: in.ExpectedVersion}
	if in.Targets != nil {
		tg := targets(*in.Targets)
		pp.Targets = &tg
	}
	if in.PublishAt != nil {
		pp.PublishAt, _ = json.Marshal(in.PublishAt)
	}
	if in.Metadata != nil {
		pp.Metadata, _ = json.Marshal(in.Metadata)
	}
	if in.RepeatEvery != nil {
		pp.RepeatEvery, _ = json.Marshal(*in.RepeatEvery)
	}
	if in.Thread != nil {
		th := thread(*in.Thread)
		pp.Thread = &th
	}
	return nil0(t.svc.UpdatePost(ctx, t.principal(ctx), in.PostID, pp))
}

type schedulePostIn struct {
	PostID    string `json:"post_id" jsonschema:"post ID (post_...)"`
	PublishAt string `json:"publish_at" jsonschema:"RFC3339, in the future"`
}

func (t *tools) schedulePost(ctx context.Context, _ *mcp.CallToolRequest, in schedulePostIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.SchedulePost(ctx, t.principal(ctx), in.PostID, in.PublishAt))
}

func (t *tools) publishPost(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.PublishPost(ctx, t.principal(ctx), in.PostID))
}

func (t *tools) cancelPost(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CancelPost(ctx, t.principal(ctx), in.PostID))
}

func (t *tools) duplicatePost(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.DuplicatePost(ctx, t.principal(ctx), in.PostID))
}

func (t *tools) deletePost(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.PostID}, t.svc.DeletePost(ctx, t.principal(ctx), in.PostID))
}

func (t *tools) postHistory(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.ListEvents(ctx, t.principal(ctx), app.EventFilter{PostID: in.PostID, Limit: 100}))
}

func (t *tools) previewPost(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.PreviewPost(ctx, t.principal(ctx), in.PostID))
}

type reportResultIn struct {
	PostID     string `json:"post_id" jsonschema:"post ID (post_...)"`
	ChannelID  string `json:"channel_id" jsonschema:"the custom channel the target is on"`
	Status     string `json:"status" jsonschema:"published or failed"`
	URL        string `json:"url,omitempty" jsonschema:"where the post lives now"`
	ExternalID string `json:"external_id,omitempty" jsonschema:"the platform's id of the post"`
	Error      string `json:"error,omitempty" jsonschema:"why it failed"`
}

func (t *tools) reportResult(ctx context.Context, _ *mcp.CallToolRequest, in reportResultIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.ReportTargetResult(ctx, t.principal(ctx), in.PostID, in.ChannelID, app.ResultInput{Status: in.Status, URL: in.URL, ExternalID: in.ExternalID, Error: in.Error}))
}

type targetRef struct {
	PostID    string `json:"post_id" jsonschema:"post ID (post_...)"`
	ChannelID string `json:"channel_id" jsonschema:"channel ID (ch_...)"`
}

func (t *tools) retryTarget(ctx context.Context, _ *mcp.CallToolRequest, in targetRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.RetryTarget(ctx, t.principal(ctx), in.PostID, in.ChannelID))
}

func (t *tools) listRequestedWork(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	w, err := t.svc.ListRequestedWork(ctx, t.principal(ctx))
	return nil, map[string]any{"work": w}, err
}

func (t *tools) listTags(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	tags, err := t.svc.ListTags(ctx, t.principal(ctx))
	return nil, map[string]any{"tags": tags}, err
}

// ---- schedules ----

type listSchedulesIn struct {
	Collection string `json:"collection,omitempty" jsonschema:"collection ID or name; empty lists every schedule"`
}

func (t *tools) listSchedules(ctx context.Context, _ *mcp.CallToolRequest, in listSchedulesIn) (*mcp.CallToolResult, any, error) {
	col, err := t.resolveCollection(ctx, in.Collection)
	if err != nil {
		return nil, nil, err
	}
	s, err := t.svc.ListSchedules(ctx, t.principal(ctx), col)
	return nil, map[string]any{"schedules": s}, err
}

type scheduleFields struct {
	Name           *string           `json:"name,omitempty"`
	Channels       *[]string         `json:"channels,omitempty" jsonschema:"channel IDs the chosen posts go to (a post's own targets win)"`
	Kind           *string           `json:"kind,omitempty" jsonschema:"once or recurring"`
	RunAt          *string           `json:"run_at,omitempty" jsonschema:"once: RFC3339 in the future"`
	Rule           *app.ScheduleRule `json:"rule,omitempty" jsonschema:"recurring: times (HH:MM) with optional weekdays (0 Sunday to 6 Saturday) and tz (IANA), or every_minutes"`
	Pick           *string           `json:"pick,omitempty" jsonschema:"next (oldest waiting draft), selected (post_ids) or all"`
	PostIDs        *[]string         `json:"post_ids,omitempty"`
	Source         *string           `json:"source,omitempty" jsonschema:"when nothing waits: none, agent (emit schedule.posts_requested) or endpoint (POST to endpoint_url)"`
	EndpointURL    *string           `json:"endpoint_url,omitempty"`
	EndpointSecret *string           `json:"endpoint_secret,omitempty" jsonschema:"signs the request like a webhook; generated when empty"`
	Enabled        *bool             `json:"enabled,omitempty"`
}

func (f scheduleFields) input(collection *string) app.ScheduleInput {
	return app.ScheduleInput{Collection: collection, Name: f.Name, Channels: f.Channels, Kind: f.Kind, RunAt: f.RunAt, Rule: f.Rule, Pick: f.Pick,
		PostIDs: f.PostIDs, Source: f.Source, EndpointURL: f.EndpointURL, EndpointSecret: f.EndpointSecret, Enabled: f.Enabled}
}

type createScheduleIn struct {
	Collection string `json:"collection" jsonschema:"collection ID (col_...) or exact name"`
	scheduleFields
}

func (t *tools) createSchedule(ctx context.Context, _ *mcp.CallToolRequest, in createScheduleIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CreateSchedule(ctx, t.principal(ctx), in.input(&in.Collection)))
}

type updateScheduleIn struct {
	ScheduleID string `json:"schedule_id" jsonschema:"schedule ID (sch_...)"`
	scheduleFields
}

func (t *tools) updateSchedule(ctx context.Context, _ *mcp.CallToolRequest, in updateScheduleIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateSchedule(ctx, t.principal(ctx), in.ScheduleID, in.input(nil)))
}

type scheduleRef struct {
	ScheduleID string `json:"schedule_id" jsonschema:"schedule ID (sch_...)"`
}

func (t *tools) deleteSchedule(ctx context.Context, _ *mcp.CallToolRequest, in scheduleRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.ScheduleID}, t.svc.DeleteSchedule(ctx, t.principal(ctx), in.ScheduleID))
}

func (t *tools) runSchedule(ctx context.Context, _ *mcp.CallToolRequest, in scheduleRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.RunScheduleNow(ctx, t.principal(ctx), in.ScheduleID))
}

// ---- jobs ----

type listJobsIn struct {
	Status string `json:"status,omitempty" jsonschema:"queued, running, done, failed or cancelled"`
	Kind   string `json:"kind,omitempty" jsonschema:"publish_post, publish_target, target_timeout, run_schedule or refresh_channel"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (t *tools) listJobs(ctx context.Context, _ *mcp.CallToolRequest, in listJobsIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.ListJobs(ctx, t.principal(ctx), app.JobFilter{Status: in.Status, Kind: in.Kind, Cursor: in.Cursor, Limit: in.Limit}))
}

type jobRef struct {
	JobID string `json:"job_id" jsonschema:"job ID (job_...)"`
}

func (t *tools) retryJob(ctx context.Context, _ *mcp.CallToolRequest, in jobRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.RetryJob(ctx, t.principal(ctx), in.JobID))
}

var _ = providers.StatusAvailable // the catalog's statuses are what list_providers reports
