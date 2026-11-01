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
	add(s, t, &mcp.Tool{Name: "list_providers", Description: "The platform catalog: every provider staqly can publish to, whether it is available in this build, how it connects (oauth2 in the dashboard, token here with connect_channel, none for your own automation), its limits (characters, media, what it does with links: limits.links and the link_modes a channel may choose) and the per-post settings it takes.",
		Annotations: ro()}, t.listProviders)
	add(s, t, &mcp.Tool{Name: "list_channels", Description: "List connected channels (accounts) with provider, handle, status (connected, needs_reconnect, disabled), group_id (its channel group) and activity (last published post and time, next scheduled post time, posts published in the last 30 days). group_id keeps one channel group. Requires channels:read.", Annotations: ro()}, t.listChannels)
	add(s, t, &mcp.Tool{Name: "get_channel", Description: "Get one channel with its group and activity. Credentials are never returned. Requires channels:read.", Annotations: ro()}, t.getChannel)
	add(s, t, &mcp.Tool{Name: "connect_channel", Description: connectChannelDescription(),
		Annotations: wr()}, t.connectChannel)
	add(s, t, &mcp.Tool{Name: "update_channel", Description: "Rename a channel, change its default settings (settings replaces them; settings.links = keep, remove or defang sets what the channel does with links in a post, within its provider's link_modes: X allows only remove and defang), turn it off (status disabled) or on (connected), or reconnect a token channel with fresh fields; group_id moves it to another channel group. Requires channels:manage.",
		Annotations: wr()}, t.updateChannel)
	add(s, t, &mcp.Tool{Name: "list_channel_groups", Description: "List the workspace's channel groups (every channel is in exactly one; the Default group always exists) with how many channels each holds. Move a channel with update_channel group_id. Requires channels:read.", Annotations: ro()}, t.listChannelGroups)
	add(s, t, &mcp.Tool{Name: "create_channel_group", Description: "Create a channel group (name, unique in the workspace; optional color #rrggbb and position). Requires channels:manage.", Annotations: wr()}, t.createChannelGroup)
	add(s, t, &mcp.Tool{Name: "update_channel_group", Description: "Rename a channel group, or change its color or position. Requires channels:manage.", Annotations: wr()}, t.updateChannelGroup)
	add(s, t, &mcp.Tool{Name: "delete_channel_group", Description: "Delete a channel group; its channels move to the Default group, which can't be deleted. Requires channels:manage.", Annotations: destructive()}, t.deleteChannelGroup)
	add(s, t, &mcp.Tool{Name: "create_channel_link", Description: "A connect link: someone without access to Staqly (a client) opens it and connects a channel to this workspace by signing in at the platform; nothing else is reachable through it. Optional note shown to them, providers (platform ids to offer; empty = every available one), group_id (the channel group connected channels join; empty = Default), expires_in_hours (default 168, max 720) and max_uses (channels, default 1, max 50). The url is returned once. Requires channels:manage.",
		Annotations: wr()}, t.createChannelLink)
	add(s, t, &mcp.Tool{Name: "list_channel_links", Description: "The workspace's connect links (status open, used, expired or revoked; uses; expiry); the links themselves are not shown again. Requires channels:manage.", Annotations: ro()}, t.listChannelLinks)
	add(s, t, &mcp.Tool{Name: "revoke_channel_link", Description: "Stop a connect link from working; channels already connected through it stay. Requires channels:manage.", Annotations: destructive()}, t.revokeChannelLink)
	add(s, t, &mcp.Tool{Name: "delete_channel", Description: "Remove a channel; its targets that have not gone out go with it, the published ones stay on their posts. Requires channels:manage.", Annotations: destructive()}, t.deleteChannel)
	add(s, t, &mcp.Tool{Name: "test_channel", Description: "Check a channel's stored credentials against its platform. Rate-limited. Requires channels:manage.", Annotations: wr()}, t.testChannel)
	add(s, t, &mcp.Tool{Name: "list_channel_options", Description: "The choices of a channel's remote setting (a settings_fields entry of kind remote), from its platform right now: for Slack, key channel lists the workspace's channels a post can go to. Requires channels:read.", Annotations: ro()}, t.listChannelOptions)

	add(s, t, &mcp.Tool{Name: "create_post", Description: "Create a post in a collection: content, media (ids from upload_media), targets (one per channel, each with an optional content override and settings: the provider's settings_fields from list_providers, as strings, e.g. X reply_settings, made_with_ai, thread_finisher, reposters as comma-separated channel ids), tags, metadata. status draft (default) or scheduled (needs publish_at, RFC3339 in the future, and a target); publish_now true publishes at once. repeat_every (1d to 6d, 1w, 2w, 1mo) schedules a copy that long after each time the post is published. thread: follow-up parts (content, media, delay_seconds) published as replies under the post on platforms that take replies (X); your own automation receives them with the request. Requires posts:write.",
		Annotations: wr()}, t.createPost)
	add(s, t, &mcp.Tool{Name: "get_post", Description: "Get one post with its targets (status, link, error per channel).", Annotations: ro()}, t.getPost)
	add(s, t, &mcp.Tool{Name: "list_posts", Description: "List posts, optionally by collection, status, channel or tag. Default order is by publish time (unscheduled last); publish_after/publish_before make it the calendar; hide_past leaves out posts that went out; sort created_desc lists the newest first.", Annotations: ro()}, t.listPosts)
	add(s, t, &mcp.Tool{Name: "search_posts", Description: "Find posts whose content contains a text, with the same filters as list_posts.", Annotations: ro()}, t.searchPosts)
	add(s, t, &mcp.Tool{Name: "update_post", Description: "Edit a draft, scheduled or failed post: content, media, targets (the full list replaces the current one), publish_at (an empty string clears it), tags, metadata, collection, repeat_every (an empty string stops repeating; edit the next scheduled copy of a repeating post to stop the chain), thread (replaces the replies; [] clears them). A published post is refused (duplicate it). Pass expected_version to refuse the edit when someone changed it meanwhile. Requires posts:write.",
		Annotations: wr()}, t.updatePost)
	add(s, t, &mcp.Tool{Name: "schedule_post", Description: "Set when a post goes out (RFC3339, in the future) and make it scheduled; the post needs at least one target. Requires posts:publish.",
		Annotations: wr()}, t.schedulePost)
	add(s, t, &mcp.Tool{Name: "publish_post", Description: "Publish a draft, scheduled or failed post now. Requires posts:publish.", Annotations: wr()}, t.publishPost)
	add(s, t, &mcp.Tool{Name: "unpublish_post", Description: "Take a published post down from its channels: all the channels it is published on, or only channel_ids. The post stays as history; each target shows removal removing, then removed, failed (the platform refused) or manual (the platform has no delete: remove it yourself). Requires posts:publish.", Annotations: destructive()}, t.unpublishPost)
	add(s, t, &mcp.Tool{Name: "cancel_post", Description: "Take a scheduled post off the schedule; it becomes a draft again. Requires posts:publish.", Annotations: wr()}, t.cancelPost)
	add(s, t, &mcp.Tool{Name: "duplicate_post", Description: "Copy a post (content, media, tags, metadata, targets) into a new draft. Requires posts:write.", Annotations: wr()}, t.duplicatePost)
	add(s, t, &mcp.Tool{Name: "delete_post", Description: "Delete a draft, scheduled or failed post (its event history is kept). A post that went out (published, publishing, or published on any channel) is history and cannot be deleted (conflict). Requires posts:delete (posts:write alone is not enough).", Annotations: destructive()}, t.deletePost)
	add(s, t, &mcp.Tool{Name: "delete_posts", Description: "Delete several posts at once (up to 200; their event history is kept). Returns the deleted ids, the ids not found, and the ids kept because they went out (published, publishing, or published on any channel: history, never deleted). Requires posts:delete.", Annotations: destructive()}, t.deletePosts)
	add(s, t, &mcp.Tool{Name: "get_post_history", Description: "The events of a post, newest first: created, edited, scheduled, published or failed per channel.", Annotations: ro()}, t.postHistory)
	add(s, t, &mcp.Tool{Name: "preview_post", Description: "How each target will carry the post: the text per channel (with the channel's links mode applied), the links it removes or defangs (links_found, with a warning naming the channel) and what its provider objects to (too long, missing title...).", Annotations: ro()}, t.previewPost)
	add(s, t, &mcp.Tool{Name: "get_post_metrics", Description: "A post's numbers per channel (views, likes, reposts, replies, quotes, bookmarks, link clicks where the platform gives them), read from the platform when older than ten minutes or with refresh true. available false: the platform reports none.", Annotations: ro()}, t.postMetrics)
	add(s, t, &mcp.Tool{Name: "get_analytics", Description: "The workspace's publishing analytics for a range of days (from and to, YYYY-MM-DD in the workspace's time zone, both included; default the last 30 days, at most 366): totals with the previous period of the same length (posts published, targets published and failed, success rate, retries, average and p90 delay after the publish time), what waits now, published and failed per day, per channel (with the most common failure and the platform numbers already cached: views, engagements, link clicks), per source (dashboard, each agent over MCP or the API, schedules, repeats), posting times by weekday and hour with average engagements (low_confidence under min_samples), top posts and failure reasons. Optional channels, collection and sources filters. Never calls a platform. Requires posts:read.", Annotations: ro()}, t.analytics)
	add(s, t, &mcp.Tool{Name: "get_post_responses", Description: "What people answered under a published post (comments, replies), per channel and overall, each with sentiment counts, a summary, themes and questions. channel_id limits it to one channel. available false: staqly can't read that platform.", Annotations: ro()}, t.postResponses)
	add(s, t, &mcp.Tool{Name: "refresh_post_responses", Description: "Read the post's responses from the platforms now and return what is stored; the AI reading follows shortly. Requires posts:write.", Annotations: wr()}, t.refreshPostResponses)
	add(s, t, &mcp.Tool{Name: "report_post_metrics", Description: "Your own automation (provider custom) sends the numbers of a post it published, replacing the stored ones. Requires posts:publish.", Annotations: wr()}, t.reportMetrics)
	add(s, t, &mcp.Tool{Name: "create_preview_link", Description: "A link to a read-only preview page of the post (as each channel shows it) where anyone with the link can comment without signing in. The url is returned once; expires_in_days 0 never expires.", Annotations: wr()}, t.createPreviewLink)
	add(s, t, &mcp.Tool{Name: "list_preview_links", Description: "A post's preview links (active, expiry, revoked); the links themselves are not shown again.", Annotations: ro()}, t.listPreviewLinks)
	add(s, t, &mcp.Tool{Name: "revoke_preview_link", Description: "Stop a preview link from working.", Annotations: destructive()}, t.revokePreviewLink)
	add(s, t, &mcp.Tool{Name: "list_post_comments", Description: "A post's feedback threads, oldest first: comments reviewers left on its preview pages (name, email) and the team's replies. A thread's first comment has parent_id empty and may mark a passage: quote, the channel's version (channel_id, empty: the shared text), part (0 the post's text, n its n-th comment), where the words are now (anchor_start/anchor_end in UTF-16 units, -1 when gone: text_changed) and resolved_at when resolved.", Annotations: ro()}, t.listPostComments)
	add(s, t, &mcp.Tool{Name: "reply_to_post_comment", Description: "Reply to a feedback thread of a post as the team (comment_id: any comment of the thread). Requires posts:write.", Annotations: wr()}, t.replyToPostComment)
	add(s, t, &mcp.Tool{Name: "resolve_post_comment", Description: "Resolve a feedback thread by its first comment (resolved false reopens it). Requires posts:write.", Annotations: wr()}, t.resolvePostComment)
	add(s, t, &mcp.Tool{Name: "report_publish_result", Description: "Close a target on your own automation (provider custom) that waits for a result: status published with the url (and external_id), or failed with the error. Only for work you did yourself. Requires posts:publish.",
		Annotations: wr()}, t.reportResult)
	add(s, t, &mcp.Tool{Name: "retry_target", Description: "Try a failed target once more, now. Requires posts:publish.", Annotations: wr()}, t.retryTarget)
	add(s, t, &mcp.Tool{Name: "list_requested_work", Description: "What people's setup asks your automation for: targets on your own automation waiting for report_publish_result (kind target, with the post and the channel) and schedules that found nothing to publish and ask for posts (kind posts; answer by creating draft posts in that collection). Oldest first.",
		Annotations: ro()}, t.listRequestedWork)
	add(s, t, &mcp.Tool{Name: "list_tags", Description: "List the workspace's post tags with how many posts carry each, most used first.", Annotations: ro()}, t.listTags)

	add(s, t, &mcp.Tool{Name: "list_schedules", Description: "List a collection's (or every) publishing schedules with their next run.", Annotations: ro()}, t.listSchedules)
	add(s, t, &mcp.Tool{Name: "create_schedule", Description: "Create a publishing schedule for a collection: kind once (run_at) or recurring (rule: times like [\"09:00\",\"17:00\"] with optional weekdays 0-6 and tz, or every_minutes), the channels it publishes to, pick (next waiting post, selected post_ids, or all), and what to do when nothing waits (source none, agent = emit schedule.posts_requested, endpoint = POST a signed request to endpoint_url and create the posts it returns; the response carries endpoint_secret in full once, so the endpoint can verify X-Staqly-Signature). Requires schedules:manage.",
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

type listChannelsIn struct {
	GroupID string `json:"group_id,omitempty" jsonschema:"only channels in this channel group (grp_..., see list_channel_groups)"`
}

func (t *tools) listChannels(ctx context.Context, _ *mcp.CallToolRequest, in listChannelsIn) (*mcp.CallToolResult, any, error) {
	c, err := t.svc.ListChannels(ctx, t.principal(ctx), app.ChannelFilter{Group: in.GroupID})
	return nil, map[string]any{"channels": c}, err
}

type channelRef struct {
	ChannelID string `json:"channel_id" jsonschema:"channel ID (ch_...)"`
}

func (t *tools) getChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetChannel(ctx, t.principal(ctx), in.ChannelID))
}

type connectChannelIn struct {
	Provider string            `json:"provider" jsonschema:"provider id from list_providers with status available, e.g. custom"`
	Name     string            `json:"name,omitempty" jsonschema:"what to call the channel; defaults to the account's name"`
	Fields   map[string]string `json:"fields,omitempty" jsonschema:"the provider's credential_fields by key"`
	Settings map[string]any    `json:"settings,omitempty" jsonschema:"provider-level defaults"`
	GroupID  string            `json:"group_id,omitempty" jsonschema:"channel group to put it in (grp_...); default: the Default group"`
}

func (t *tools) connectChannel(ctx context.Context, _ *mcp.CallToolRequest, in connectChannelIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.ConnectChannel(ctx, t.principal(ctx), app.ChannelInput{Provider: in.Provider, Name: in.Name, Fields: in.Fields, Settings: in.Settings, GroupID: in.GroupID}))
}

type updateChannelIn struct {
	ChannelID string            `json:"channel_id" jsonschema:"channel ID (ch_...)"`
	Name      *string           `json:"name,omitempty"`
	Settings  map[string]any    `json:"settings,omitempty"`
	Status    *string           `json:"status,omitempty" jsonschema:"connected or disabled"`
	Fields    map[string]string `json:"fields,omitempty" jsonschema:"token platforms: fresh credential fields to reconnect"`
	GroupID   *string           `json:"group_id,omitempty" jsonschema:"move the channel to this channel group (grp_...; empty: Default)"`
}

func (t *tools) updateChannel(ctx context.Context, _ *mcp.CallToolRequest, in updateChannelIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateChannel(ctx, t.principal(ctx), in.ChannelID, app.ChannelPatch{Name: in.Name, Settings: in.Settings, Status: in.Status, Fields: in.Fields, GroupID: in.GroupID}))
}

func (t *tools) listChannelGroups(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	g, err := t.svc.ListChannelGroups(ctx, t.principal(ctx))
	return nil, map[string]any{"groups": g}, err
}

type channelGroupIn struct {
	Name     *string `json:"name,omitempty" jsonschema:"what to call the group, unique in the workspace"`
	Color    *string `json:"color,omitempty" jsonschema:"#rrggbb, or empty"`
	Position *int64  `json:"position,omitempty" jsonschema:"sort order (smaller first; Default always leads)"`
}

// updateChannelGroupIn: the group to change plus the fields of create_channel_group.
type updateChannelGroupIn struct {
	GroupID string `json:"group_id" jsonschema:"channel group ID (grp_...)"`
	channelGroupIn
}

func (t *tools) createChannelGroup(ctx context.Context, _ *mcp.CallToolRequest, in channelGroupIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CreateChannelGroup(ctx, t.principal(ctx), app.ChannelGroupInput{Name: in.Name, Color: in.Color, Position: in.Position}))
}

func (t *tools) updateChannelGroup(ctx context.Context, _ *mcp.CallToolRequest, in updateChannelGroupIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateChannelGroup(ctx, t.principal(ctx), in.GroupID, app.ChannelGroupInput{Name: in.Name, Color: in.Color, Position: in.Position}))
}

type channelGroupRef struct {
	GroupID string `json:"group_id" jsonschema:"channel group ID (grp_...)"`
}

func (t *tools) deleteChannelGroup(ctx context.Context, _ *mcp.CallToolRequest, in channelGroupRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.GroupID}, t.svc.DeleteChannelGroup(ctx, t.principal(ctx), in.GroupID))
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

type channelOptionsIn struct {
	ChannelID string `json:"channel_id" jsonschema:"channel ID (ch_...)"`
	Key       string `json:"key" jsonschema:"the setting's key (channel for Slack)"`
}

func (t *tools) listChannelOptions(ctx context.Context, _ *mcp.CallToolRequest, in channelOptionsIn) (*mcp.CallToolResult, any, error) {
	out, err := t.svc.ChannelOptions(ctx, t.principal(ctx), in.ChannelID, in.Key)
	return nil, map[string]any{"options": out}, err
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
	Title       string         `json:"title" jsonschema:"the post's title (required): platforms with a title (YouTube, Reddit, blogs) publish it, the others show it in staqly's lists"`
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
	pi := app.PostInput{Collection: in.Collection, Title: in.Title, Content: in.Content, Media: in.Media, Targets: targets(in.Targets), Status: in.Status,
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
	HidePast      bool   `json:"hide_past,omitempty" jsonschema:"leave out posts that went out (published); failed ones stay"`
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
		PublishBefore: in.PublishBefore, HidePast: in.HidePast, Sort: in.Sort, Cursor: in.Cursor, Limit: in.Limit}, nil
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
	Title           *string        `json:"title,omitempty" jsonschema:"the post's title (can't be emptied)"`
	Content         *string        `json:"content,omitempty"`
	Media           *[]string      `json:"media,omitempty"`
	Targets         *[]targetIn    `json:"targets,omitempty" jsonschema:"replaces the whole list"`
	PublishAt       any            `json:"publish_at,omitempty" jsonschema:"RFC3339 in the future, or an empty string to clear it (back to a draft)"`
	Tags            *[]string      `json:"tags,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ExternalRef     *string        `json:"external_ref,omitempty"`
	RepeatEvery     *string        `json:"repeat_every,omitempty" jsonschema:"1d to 6d, 1w, 2w or 1mo; an empty string stops repeating"`
	Thread          *[]threadIn    `json:"thread,omitempty" jsonschema:"replaces the replies; [] clears them"`
	ExpectedVersion *int64         `json:"expected_version,omitempty"`
}

func (t *tools) updatePost(ctx context.Context, _ *mcp.CallToolRequest, in updatePostIn) (*mcp.CallToolResult, any, error) {
	pp := app.PostPatch{Collection: in.Collection, Title: in.Title, Content: in.Content, Media: in.Media, Tags: in.Tags, ExternalRef: in.ExternalRef, ExpectedVersion: in.ExpectedVersion}
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

type unpublishIn struct {
	PostID     string   `json:"post_id" jsonschema:"post ID (pst_...)"`
	ChannelIDs []string `json:"channel_ids,omitempty" jsonschema:"only these channels (chn_...); default every published one"`
}

func (t *tools) unpublishPost(ctx context.Context, _ *mcp.CallToolRequest, in unpublishIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UnpublishPost(ctx, t.principal(ctx), in.PostID, in.ChannelIDs))
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

type postIDs struct {
	IDs []string `json:"ids" jsonschema:"post IDs (post_...), up to 200"`
}

func (t *tools) deletePosts(ctx context.Context, _ *mcp.CallToolRequest, in postIDs) (*mcp.CallToolResult, any, error) {
	out, err := t.svc.DeletePosts(ctx, t.principal(ctx), in.IDs)
	return done(out, err)
}

func (t *tools) postHistory(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.ListEvents(ctx, t.principal(ctx), app.EventFilter{PostID: in.PostID, Limit: 100}))
}

func (t *tools) previewPost(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.PreviewPost(ctx, t.principal(ctx), in.PostID))
}

type metricsIn struct {
	PostID  string `json:"post_id" jsonschema:"post ID (post_...)"`
	Refresh bool   `json:"refresh,omitempty" jsonschema:"read the numbers from the platforms now"`
}

func (t *tools) postMetrics(ctx context.Context, _ *mcp.CallToolRequest, in metricsIn) (*mcp.CallToolResult, any, error) {
	m, err := t.svc.PostMetrics(ctx, t.principal(ctx), in.PostID, in.Refresh)
	return nil, map[string]any{"targets": m}, err
}

type analyticsIn struct {
	From       string   `json:"from,omitempty" jsonschema:"first day, YYYY-MM-DD in the workspace's time zone"`
	To         string   `json:"to,omitempty" jsonschema:"last day (included), YYYY-MM-DD; default today"`
	Days       int      `json:"days,omitempty" jsonschema:"instead of from: the last n days up to to (1 to 366)"`
	Channels   []string `json:"channels,omitempty" jsonschema:"only these channels (channel IDs)"`
	Collection string   `json:"collection,omitempty" jsonschema:"only posts of this collection (collection ID)"`
	Sources    []string `json:"sources,omitempty" jsonschema:"only posts from these sources, as sources[].source names them (an empty one: posts with none recorded)"`
}

func (t *tools) analytics(ctx context.Context, _ *mcp.CallToolRequest, in analyticsIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.Analytics(ctx, t.principal(ctx), app.AnalyticsQuery{From: in.From, To: in.To, Days: in.Days, Channels: in.Channels,
		Collection: in.Collection, Sources: in.Sources}))
}

type reportMetricsIn struct {
	PostID    string `json:"post_id" jsonschema:"post ID (post_...)"`
	ChannelID string `json:"channel_id" jsonschema:"the custom channel the target is on"`
	providers.Metrics
}

func (t *tools) reportMetrics(ctx context.Context, _ *mcp.CallToolRequest, in reportMetricsIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.ReportTargetMetrics(ctx, t.principal(ctx), in.PostID, in.ChannelID, in.Metrics))
}

type previewLinkIn struct {
	PostID        string `json:"post_id" jsonschema:"post ID (post_...)"`
	ExpiresInDays int    `json:"expires_in_days,omitempty" jsonschema:"days until the link stops working, 0 to 365 (0 never)"`
}

func (t *tools) createPreviewLink(ctx context.Context, _ *mcp.CallToolRequest, in previewLinkIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CreatePreviewLink(ctx, t.principal(ctx), in.PostID, app.PreviewLinkInput{ExpiresInDays: in.ExpiresInDays}, ""))
}

func (t *tools) listPreviewLinks(ctx context.Context, _ *mcp.CallToolRequest, in postRef) (*mcp.CallToolResult, any, error) {
	l, err := t.svc.ListPreviewLinks(ctx, t.principal(ctx), in.PostID)
	return nil, map[string]any{"links": l}, err
}

type channelLinkIn struct {
	Note           string   `json:"note,omitempty" jsonschema:"shown to the person who opens the link"`
	Providers      []string `json:"providers,omitempty" jsonschema:"platform ids to offer; empty offers every available one"`
	GroupID        string   `json:"group_id,omitempty" jsonschema:"channel group connected channels join (empty: Default)"`
	ExpiresInHours int      `json:"expires_in_hours,omitempty" jsonschema:"hours the link works (default 168, at most 720)"`
	MaxUses        int      `json:"max_uses,omitempty" jsonschema:"how many channels may be connected (default 1, at most 50)"`
}

func (t *tools) createChannelLink(ctx context.Context, _ *mcp.CallToolRequest, in channelLinkIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CreateChannelLink(ctx, t.principal(ctx), app.ChannelLinkInput{Note: in.Note, Providers: in.Providers,
		GroupID: in.GroupID, ExpiresInHours: in.ExpiresInHours, MaxUses: in.MaxUses}, ""))
}

func (t *tools) listChannelLinks(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	l, err := t.svc.ListChannelLinks(ctx, t.principal(ctx))
	return nil, map[string]any{"links": l}, err
}

type channelLinkRef struct {
	LinkID string `json:"link_id" jsonschema:"connect link ID (chl_...)"`
}

func (t *tools) revokeChannelLink(ctx context.Context, _ *mcp.CallToolRequest, in channelLinkRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"revoked": true, "id": in.LinkID}, t.svc.RevokeChannelLink(ctx, t.principal(ctx), in.LinkID))
}

type previewRef struct {
	PostID    string `json:"post_id" jsonschema:"post ID (post_...)"`
	PreviewID string `json:"preview_id" jsonschema:"preview link ID (prv_...)"`
}

func (t *tools) revokePreviewLink(ctx context.Context, _ *mcp.CallToolRequest, in previewRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"revoked": true, "id": in.PreviewID}, t.svc.RevokePreviewLink(ctx, t.principal(ctx), in.PostID, in.PreviewID))
}

type listCommentsIn struct {
	PostID string `json:"post_id" jsonschema:"post ID (post_...)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page (500 comments a page), for later comments"`
}

func (t *tools) listPostComments(ctx context.Context, _ *mcp.CallToolRequest, in listCommentsIn) (*mcp.CallToolResult, any, error) {
	c, err := t.svc.ListPostComments(ctx, t.principal(ctx), in.PostID, 0, in.Cursor)
	return nil, map[string]any{"comments": c.Data, "next_cursor": c.NextCursor}, err
}

type commentReplyIn struct {
	PostID    string `json:"post_id" jsonschema:"post ID (post_...)"`
	CommentID string `json:"comment_id" jsonschema:"a comment of the thread (cmt_...)"`
	Body      string `json:"body" jsonschema:"the reply, up to 2000 characters"`
}

func (t *tools) replyToPostComment(ctx context.Context, _ *mcp.CallToolRequest, in commentReplyIn) (*mcp.CallToolResult, any, error) {
	c, err := t.svc.AddPostComment(ctx, t.principal(ctx), in.PostID, app.PostCommentInput{Body: in.Body, ParentID: in.CommentID})
	return nil, c, err
}

type commentResolveIn struct {
	PostID    string `json:"post_id" jsonschema:"post ID (post_...)"`
	CommentID string `json:"comment_id" jsonschema:"the thread's first comment (cmt_...)"`
	Resolved  *bool  `json:"resolved,omitempty" jsonschema:"false reopens a resolved thread; default true"`
}

func (t *tools) resolvePostComment(ctx context.Context, _ *mcp.CallToolRequest, in commentResolveIn) (*mcp.CallToolResult, any, error) {
	c, err := t.svc.SetPostCommentResolved(ctx, t.principal(ctx), in.PostID, in.CommentID, in.Resolved == nil || *in.Resolved)
	return nil, c, err
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
	w, err := t.svc.ListRequestedWork(ctx, t.principal(ctx), 0) // the oldest 100: a queue that drains as results come in
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
	EndpointSecret *string           `json:"endpoint_secret,omitempty" jsonschema:"signs the request like a webhook; generated when empty (an empty string on update generates a new one); returned in full only by the call that set it"`
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

type postResponsesIn struct {
	PostID    string `json:"post_id" jsonschema:"post ID (post_...)"`
	ChannelID string `json:"channel_id,omitempty" jsonschema:"only this channel's responses"`
}

func (t *tools) postResponses(ctx context.Context, _ *mcp.CallToolRequest, in postResponsesIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.PostResponses(ctx, t.principal(ctx), in.PostID, in.ChannelID))
}

type refreshResponsesIn struct {
	PostID string `json:"post_id" jsonschema:"post ID (post_...)"`
}

func (t *tools) refreshPostResponses(ctx context.Context, _ *mcp.CallToolRequest, in refreshResponsesIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.RefreshPostResponses(ctx, t.principal(ctx), in.PostID))
}
