package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// Collections, media and events: the rest of the REST surface for content, as
// thin wrappers over the same services.

// maxMediaInline caps get_media include_content (base64 inflates by 4/3).
const maxMediaInline = 4 << 20

// MaxEventExport caps the text export_events returns in one call (a variable for tests): small
// enough for agents' tool-output limits (OpenCode keeps 50 KB, Claude Code about 25k tokens);
// a larger result reached the model only as "output saved to a file".
var MaxEventExport = 32 << 10

func (t *tools) addContentTools(s *mcp.Server) {
	add(s, t, &mcp.Tool{Name: "update_collection", Description: "Update a collection: name, description, icon, colour, the wait for your own automation's result (processing_timeout_seconds), or archive/unarchive it (archiving an archived collection, or unarchiving an active one, is a conflict). Requires collections:write.",
		Annotations: wr()}, t.updateCollection)
	add(s, t, &mcp.Tool{Name: "delete_collection", Description: "Permanently delete a collection with its posts and schedules. confirm must equal the collection name. Requires collections:delete (collections:write alone is not enough).",
		Annotations: destructive()}, t.deleteCollection)

	add(s, t, &mcp.Tool{Name: "upload_media", Description: "Upload a file (base64); returns a media ID to put in a post's media list. Requires media:write.",
		Annotations: wr()}, t.uploadMedia)
	add(s, t, &mcp.Tool{Name: "get_media", Description: "Get a media file's metadata (type, size, name, SHA-256, URL); with include_content=true also its base64 content (files up to 4 MiB).",
		Annotations: ro()}, t.getMedia)
	add(s, t, &mcp.Tool{Name: "list_media", Description: "The media library: uploaded pictures, videos and audio, newest first, with how many posts use each (uses). Filter by name (q), kind (image, video, audio), used (used or unused), folder (a folder id or root) and tag; sort created_desc, created_asc, size_desc or name_asc; page with offset and limit (next_offset). Requires media:read.",
		Annotations: ro()}, t.listMedia)
	add(s, t, &mcp.Tool{Name: "update_media", Description: "Rename a media file, set its alt text (a description for people who can't see it; platforms that take alt text receive it), or set a video's width, height and duration_ms. Requires media:write.",
		Annotations: wr()}, t.updateMedia)
	add(s, t, &mcp.Tool{Name: "get_media_usage", Description: "The posts that use a media file (in their files or a comment), newest first, at most 50, with each post's title, status and publish time. Check it before deleting a file. Requires media:read and posts:read.",
		Annotations: ro()}, t.mediaUsage)
	add(s, t, &mcp.Tool{Name: "list_media_folders", Description: "The media library's virtual folders (id, parent_id, name, files directly in it), the number of files at the top level and in total, and every file tag with its count. Requires media:read.",
		Annotations: ro()}, t.listMediaFolders)
	add(s, t, &mcp.Tool{Name: "create_media_folder", Description: "Create a media library folder at the top level or inside another (parent_id). Names are unique among a folder's subfolders, ignoring case. Requires media:write.",
		Annotations: wr()}, t.createMediaFolder)
	add(s, t, &mcp.Tool{Name: "update_media_folder", Description: "Rename a media library folder or move it under another folder (parent_id; empty = the top level). A folder can't go inside itself. Requires media:write.",
		Annotations: wr()}, t.updateMediaFolder)
	add(s, t, &mcp.Tool{Name: "delete_media_folder", Description: "Delete a media library folder. Its files and subfolders move up to its parent; no file is deleted. Requires media:write.",
		Annotations: destructive()}, t.deleteMediaFolder)
	add(s, t, &mcp.Tool{Name: "organize_media", Description: "Move up to 200 media files to a folder (folder_id; empty = the top level) and/or add and remove tags on them. Requires media:write.",
		Annotations: wr()}, t.organizeMedia)
	add(s, t, &mcp.Tool{Name: "delete_media", Description: "Move a media file to Deleted, where it can be restored for 30 days before it is deleted for good (refused while a scheduled post uses it); with permanent=true delete it for good now, and posts that reference it lose that file. Requires posts:delete (posts:write alone is not enough).",
		Annotations: destructive()}, t.deleteMedia)
	add(s, t, &mcp.Tool{Name: "restore_media", Description: "Take a media file out of Deleted, back where it was. Requires posts:delete.",
		Annotations: wr()}, t.restoreMedia)
	add(s, t, &mcp.Tool{Name: "keep_media", Description: "Keep this media file of a set of identical ones (list_media with duplicates=true): posts that use a copy use this file instead, and the copies move to Deleted. Requires posts:delete and posts:write.",
		Annotations: destructive()}, t.keepMedia)

	add(s, t, &mcp.Tool{Name: "list_events", Description: "List the activity log (posts created, scheduled, published or failed, channels connected, schedules run, settings changes), newest first, with optional filters.",
		Annotations: ro()}, t.listEvents)
	add(s, t, &mcp.Tool{Name: "export_events", Description: "Export the activity log as NDJSON or CSV text, oldest first; events is the number of events (lines) in this call. Output is capped at 32 KiB per call: when truncated=true, call again with the same filters and after_seq=next_after_seq for the rest (the whole log is the sum of all calls).",
		Annotations: ro()}, t.exportEvents)
	add(s, t, &mcp.Tool{Name: "purge_post", Description: "Delete a post together with its whole history: every event about it (and their webhook deliveries), its media when no other post uses them. For removing personal data; one content-free post.purged event remains. Requires admin.",
		Annotations: destructive()}, t.purgePost)
}

// ---- collections ----

type updateCollectionIn struct {
	Collection  string  `json:"collection" jsonschema:"collection ID (col_...) or exact name"`
	Name        *string `json:"name,omitempty" jsonschema:"new name"`
	Description *string `json:"description,omitempty" jsonschema:"new description"`
	Icon        *string `json:"icon,omitempty" jsonschema:"icon name"`
	Color       *string `json:"color,omitempty" jsonschema:"accent colour, e.g. #7c3aed"`
	// ProcessingTimeoutSeconds: a number of seconds or null (back to the server default); any, so null is seen.
	ProcessingTimeoutSeconds any   `json:"processing_timeout_seconds,omitempty" jsonschema:"how long a post on your own automation waits for a reported result before it fails, 60-86400 seconds, or null for the server default"`
	Archived                 *bool `json:"archived,omitempty" jsonschema:"true archives, false unarchives"`
}

func (t *tools) updateCollection(ctx context.Context, _ *mcp.CallToolRequest, in updateCollectionIn) (*mcp.CallToolResult, any, error) {
	id, err := t.collectionID(ctx, in.Collection)
	if err != nil {
		return nil, nil, err
	}
	ci := app.CollectionInput{Name: in.Name, Description: in.Description, Icon: in.Icon, Color: in.Color, Archived: in.Archived}
	if in.ProcessingTimeoutSeconds != nil {
		ci.ProcessingTimeoutSeconds, _ = json.Marshal(in.ProcessingTimeoutSeconds)
	}
	return nil0(t.svc.UpdateCollection(ctx, t.principal(ctx), id, ci))
}

type deleteCollectionIn struct {
	Collection string `json:"collection" jsonschema:"collection ID (col_...) or exact name"`
	Confirm    string `json:"confirm" jsonschema:"must be exactly the collection name"`
}

func (t *tools) deleteCollection(ctx context.Context, _ *mcp.CallToolRequest, in deleteCollectionIn) (*mcp.CallToolResult, any, error) {
	id, err := t.collectionID(ctx, in.Collection)
	if err != nil {
		return nil, nil, err
	}
	return done(map[string]any{"deleted": true, "id": id}, t.svc.DeleteCollection(ctx, t.principal(ctx), id, in.Confirm))
}

// collectionID resolves a required collection reference (ID or name).
func (t *tools) collectionID(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", &app.Error{Code: "validation_failed", Status: 422, Message: "collection is required"}
	}
	return t.resolveCollection(ctx, ref)
}

// ---- media ----

type uploadMediaIn struct {
	MediaBase64 string `json:"media_base64" jsonschema:"base64-encoded picture, video or audio file"`
	Filename    string `json:"filename" jsonschema:"file name including the extension, e.g. avatar.png"`
}

func (t *tools) uploadMedia(ctx context.Context, _ *mcp.CallToolRequest, in uploadMediaIn) (*mcp.CallToolResult, any, error) {
	raw, err := base64.StdEncoding.DecodeString(in.MediaBase64)
	if err != nil {
		return nil, nil, &app.Error{Code: "validation_failed", Status: 422, Message: "media_base64 is not valid base64"}
	}
	name := in.Filename
	if name == "" {
		name = "upload"
	}
	return nil0(t.svc.UploadMedia(ctx, t.principal(ctx), name, bytes.NewReader(raw)))
}

type getMediaIn struct {
	MediaID        string `json:"media_id" jsonschema:"media ID (med_...)"`
	IncludeContent bool   `json:"include_content,omitempty" jsonschema:"also return the file as base64 (files up to 4 MiB)"`
}

func (t *tools) getMedia(ctx context.Context, _ *mcp.CallToolRequest, in getMediaIn) (*mcp.CallToolResult, any, error) {
	f, m, err := t.svc.OpenMedia(ctx, t.principal(ctx), in.MediaID)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	out := map[string]any{"id": m.ID, "mime_type": m.MimeType, "size_bytes": m.SizeBytes, "original_name": m.OriginalName,
		"sha256": m.Sha256, "created_at": m.CreatedAt, "url": strings.TrimRight(t.svc.Cfg.PublicURL, "/") + "/v1/media/" + m.ID,
		"alt_text": m.AltText, "width": m.Width, "height": m.Height, "duration_ms": m.DurationMs}
	if in.IncludeContent {
		if m.SizeBytes > maxMediaInline {
			return nil, nil, &app.Error{Code: "too_large", Status: 413, Message: "file is larger than 4 MiB; download it from url instead"}
		}
		raw, err := io.ReadAll(io.LimitReader(f, maxMediaInline+1))
		if err != nil {
			return nil, nil, err
		}
		out["content_base64"] = base64.StdEncoding.EncodeToString(raw)
	}
	return nil, out, nil
}

type mediaRef struct {
	MediaID string `json:"media_id" jsonschema:"media ID (med_...)"`
}

type listMediaIn struct {
	Q      string `json:"q,omitempty" jsonschema:"part of the file name"`
	Kind   string `json:"kind,omitempty" jsonschema:"image, video or audio"`
	Used   string `json:"used,omitempty" jsonschema:"used (posts carry it) or unused"`
	Folder string `json:"folder,omitempty" jsonschema:"a folder ID (files directly in it) or root (the top level)"`
	Tag    string `json:"tag,omitempty" jsonschema:"files carrying this tag"`
	// Deleted and Duplicates switch the listing.
	Deleted    bool   `json:"deleted,omitempty" jsonschema:"list the files in Deleted (with deleted_at and purge_at) instead"`
	Duplicates bool   `json:"duplicates,omitempty" jsonschema:"only files with an identical twin, grouped by content"`
	Sort       string `json:"sort,omitempty" jsonschema:"created_desc (default), created_asc, size_desc or name_asc"`
	Offset     int    `json:"offset,omitempty" jsonschema:"next_offset of the previous page"`
	Limit      int    `json:"limit,omitempty" jsonschema:"page size (default 60, max 200)"`
}

func (t *tools) listMedia(ctx context.Context, _ *mcp.CallToolRequest, in listMediaIn) (*mcp.CallToolResult, any, error) {
	out, err := t.svc.ListMedia(ctx, t.principal(ctx), app.MediaFilter{Query: in.Q, Kind: in.Kind, Used: in.Used,
		Folder: in.Folder, Tag: in.Tag, Deleted: in.Deleted, Duplicates: in.Duplicates, Sort: in.Sort, Offset: in.Offset,
		Limit: in.Limit})
	return done(out, err)
}

type updateMediaIn struct {
	MediaID    string   `json:"media_id" jsonschema:"the media ID"`
	Name       *string  `json:"name,omitempty" jsonschema:"new file name"`
	AltText    *string  `json:"alt_text,omitempty" jsonschema:"alt text, up to 1500 characters; empty clears it"`
	Width      *int64   `json:"width,omitempty" jsonschema:"pixels"`
	Height     *int64   `json:"height,omitempty" jsonschema:"pixels"`
	DurationMs *int64   `json:"duration_ms,omitempty" jsonschema:"a video's length in milliseconds"`
	FolderID   *string  `json:"folder_id,omitempty" jsonschema:"move the file: a folder ID, or empty for the top level"`
	Tags       []string `json:"tags,omitempty" jsonschema:"replace the file's tags"`
}

func (t *tools) updateMedia(ctx context.Context, _ *mcp.CallToolRequest, in updateMediaIn) (*mcp.CallToolResult, any, error) {
	return done(t.svc.UpdateMedia(ctx, t.principal(ctx), in.MediaID, app.MediaPatch{Name: in.Name, AltText: in.AltText,
		Width: in.Width, Height: in.Height, DurationMs: in.DurationMs, FolderID: in.FolderID, Tags: in.Tags}))
}

func (t *tools) listMediaFolders(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return done(t.svc.ListMediaFolders(ctx, t.principal(ctx)))
}

type newMediaFolderIn struct {
	Name     string `json:"name" jsonschema:"the folder name"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"the folder it goes in; empty or omitted for the top level"`
}

func (t *tools) createMediaFolder(ctx context.Context, _ *mcp.CallToolRequest, in newMediaFolderIn) (*mcp.CallToolResult, any, error) {
	return done(t.svc.CreateMediaFolder(ctx, t.principal(ctx), app.MediaFolderInput{Name: &in.Name, ParentID: &in.ParentID}))
}

type mediaFolderIn struct {
	FolderID string  `json:"folder_id" jsonschema:"the folder ID"`
	Name     *string `json:"name,omitempty" jsonschema:"the new name"`
	ParentID *string `json:"parent_id,omitempty" jsonschema:"move it into this folder; empty for the top level"`
}

func (t *tools) updateMediaFolder(ctx context.Context, _ *mcp.CallToolRequest, in mediaFolderIn) (*mcp.CallToolResult, any, error) {
	return done(t.svc.UpdateMediaFolder(ctx, t.principal(ctx), in.FolderID, app.MediaFolderInput{Name: in.Name, ParentID: in.ParentID}))
}

type folderRef struct {
	FolderID string `json:"folder_id" jsonschema:"the folder ID"`
}

func (t *tools) deleteMediaFolder(ctx context.Context, _ *mcp.CallToolRequest, in folderRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.FolderID}, t.svc.DeleteMediaFolder(ctx, t.principal(ctx), in.FolderID))
}

type organizeMediaIn struct {
	IDs        []string `json:"ids" jsonschema:"media IDs (med_...), up to 200"`
	FolderID   *string  `json:"folder_id,omitempty" jsonschema:"move them to this folder; empty for the top level; omit to leave folders alone"`
	AddTags    []string `json:"add_tags,omitempty" jsonschema:"tags to add (lowercase, no #, spaces become dashes)"`
	RemoveTags []string `json:"remove_tags,omitempty" jsonschema:"tags to remove"`
}

func (t *tools) organizeMedia(ctx context.Context, _ *mcp.CallToolRequest, in organizeMediaIn) (*mcp.CallToolResult, any, error) {
	return done(t.svc.BulkUpdateMedia(ctx, t.principal(ctx), app.MediaBulk{IDs: in.IDs, FolderID: in.FolderID, AddTags: in.AddTags, RemoveTags: in.RemoveTags}))
}

func (t *tools) mediaUsage(ctx context.Context, _ *mcp.CallToolRequest, in mediaRef) (*mcp.CallToolResult, any, error) {
	out, err := t.svc.MediaUsage(ctx, t.principal(ctx), in.MediaID)
	return done(map[string]any{"data": out}, err)
}

type deleteMediaIn struct {
	MediaID   string `json:"media_id" jsonschema:"media ID (med_...)"`
	Permanent bool   `json:"permanent,omitempty" jsonschema:"delete it for good now instead of moving it to Deleted"`
}

func (t *tools) deleteMedia(ctx context.Context, _ *mcp.CallToolRequest, in deleteMediaIn) (*mcp.CallToolResult, any, error) {
	if in.Permanent {
		return done(map[string]any{"deleted": true, "id": in.MediaID}, t.svc.DeleteMedia(ctx, t.principal(ctx), in.MediaID))
	}
	return done(map[string]any{"deleted": true, "id": in.MediaID, "restorable_days": app.MediaTrashDays},
		t.svc.TrashMedia(ctx, t.principal(ctx), in.MediaID))
}

func (t *tools) restoreMedia(ctx context.Context, _ *mcp.CallToolRequest, in mediaRef) (*mcp.CallToolResult, any, error) {
	return done(t.svc.RestoreMedia(ctx, t.principal(ctx), in.MediaID))
}

func (t *tools) keepMedia(ctx context.Context, _ *mcp.CallToolRequest, in mediaRef) (*mcp.CallToolResult, any, error) {
	out, err := t.svc.KeepMedia(ctx, t.principal(ctx), in.MediaID)
	return done(map[string]any{"deleted": out}, err)
}

// ---- events ----

type listEventsIn struct {
	PostID     string `json:"post_id,omitempty" jsonschema:"only events about this post"`
	Collection string `json:"collection,omitempty" jsonschema:"only events of this collection (ID or name)"`
	ChannelID  string `json:"channel_id,omitempty" jsonschema:"only events about this channel"`
	ScheduleID string `json:"schedule_id,omitempty" jsonschema:"only events about this schedule"`
	Type       string `json:"type,omitempty" jsonschema:"exact event type, e.g. post.published"`
	TypePrefix string `json:"type_prefix,omitempty" jsonschema:"event type prefix, e.g. post."`
	From       string `json:"from,omitempty" jsonschema:"RFC3339 or YYYY-MM-DD, inclusive"`
	To         string `json:"to,omitempty" jsonschema:"RFC3339 or YYYY-MM-DD, exclusive"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
	Limit      int    `json:"limit,omitempty" jsonschema:"page size (default 50, max 200)"`
}

func (t *tools) listEvents(ctx context.Context, _ *mcp.CallToolRequest, in listEventsIn) (*mcp.CallToolResult, any, error) {
	col, err := t.resolveCollection(ctx, in.Collection)
	if err != nil {
		return nil, nil, err
	}
	return nil0(t.svc.ListEvents(ctx, t.principal(ctx), app.EventFilter{PostID: in.PostID, CollectionID: col, ChannelID: in.ChannelID,
		ScheduleID: in.ScheduleID, Type: in.Type, TypePrefix: in.TypePrefix, From: in.From, To: in.To, Before: in.Cursor, Limit: in.Limit}))
}

type exportEventsIn struct {
	Format     string `json:"format,omitempty" jsonschema:"ndjson (default) or csv"`
	From       string `json:"from,omitempty" jsonschema:"YYYY-MM-DD or RFC3339 (inclusive)"`
	To         string `json:"to,omitempty" jsonschema:"YYYY-MM-DD or RFC3339 (exclusive)"`
	Type       string `json:"type,omitempty" jsonschema:"exact event type, e.g. item.approved"`
	TypePrefix string `json:"type_prefix,omitempty" jsonschema:"event type prefix, e.g. item."`
	ActorType  string `json:"actor_type,omitempty" jsonschema:"session, api_key, local or system"`
	ActorID    string `json:"actor_id,omitempty" jsonschema:"actor ID, e.g. an API key ID"`
	Collection string `json:"collection,omitempty" jsonschema:"collection ID or exact name"`
	PostID     string `json:"post_id,omitempty" jsonschema:"only events about this post (post_...)"`
	AfterSeq   int64  `json:"after_seq,omitempty" jsonschema:"continue a truncated export: next_after_seq of the previous call"`
}

var errExportFull = errors.New("export cap reached")

// exportEventsOut puts the counts first: a client that cuts a long result keeps them.
type exportEventsOut struct {
	Events       int    `json:"events" jsonschema:"events (lines) in this call"`
	Truncated    bool   `json:"truncated" jsonschema:"more events match: call again with after_seq=next_after_seq"`
	NextAfterSeq *int64 `json:"next_after_seq,omitempty" jsonschema:"after_seq for the next call (only when truncated)"`
	Format       string `json:"format" jsonschema:"ndjson or csv"`
	Content      string `json:"content" jsonschema:"the events, one per line (csv: after a header line)"`
}

func (t *tools) exportEvents(ctx context.Context, _ *mcp.CallToolRequest, in exportEventsIn) (*mcp.CallToolResult, *exportEventsOut, error) {
	col, err := t.resolveCollection(ctx, in.Collection)
	if err != nil {
		return nil, nil, err
	}
	x, err := t.svc.PrepareEventExport(ctx, t.principal(ctx), app.EventExportFilter{Format: in.Format, From: in.From, To: in.To,
		Type: in.Type, TypePrefix: in.TypePrefix, ActorType: in.ActorType, ActorID: in.ActorID, CollectionID: col, PostID: in.PostID,
		AfterSeq: in.AfterSeq})
	if err != nil {
		return nil, nil, err
	}
	var buf, line bytes.Buffer
	render := func(e app.Event) error {
		b, err := json.Marshal(e)
		line.Write(b)
		line.WriteByte('\n')
		return err
	}
	if x.Format == "csv" {
		cw := app.NewEventCSVWriter(&buf)
		_ = cw.Write(app.EventCSVHeader)
		cw.Flush()
		render = func(e app.Event) error {
			w := app.NewEventCSVWriter(&line)
			_ = w.Write(app.EventCSVRecord(e))
			w.Flush()
			return w.Error()
		}
	}
	n, truncated, last := 0, false, int64(0)
	err = x.Each(ctx, func(e app.Event) error {
		line.Reset()
		if err := render(e); err != nil {
			return err
		}
		if n > 0 && buf.Len()+line.Len() > MaxEventExport { // one event always fits, so every call makes progress
			truncated = true
			return errExportFull
		}
		buf.Write(line.Bytes())
		n, last = n+1, e.Seq
		return nil
	})
	if err != nil && !errors.Is(err, errExportFull) {
		return nil, nil, err
	}
	out := &exportEventsOut{Events: n, Truncated: truncated, Format: x.Format, Content: buf.String()}
	if truncated {
		// the next call continues exactly here (agents had narrowed from/to by guessing, dozens of calls)
		out.NextAfterSeq = &last
	}
	// the text in field order (the default text has sorted keys: content first)
	text, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, out, nil
}

// done adapts a service call without a result value.
func done(v any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type purgePostIn struct {
	PostID string `json:"post_id" jsonschema:"post ID (post_...)"`
}

func (t *tools) purgePost(ctx context.Context, _ *mcp.CallToolRequest, in purgePostIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.PurgePost(ctx, t.principal(ctx), in.PostID))
}
