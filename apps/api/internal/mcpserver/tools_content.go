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

// Collections, items, media and events beyond the core review tools: the rest
// of the REST surface for content, as thin wrappers over the same services.

const (
	// maxMediaInline caps get_media include_content (base64 inflates by 4/3).
	maxMediaInline = 4 << 20
	// maxEventExport caps the text export_events returns in one call.
	maxEventExport = 1 << 20
)

func (t *tools) addContentTools(s *mcp.Server) {
	add(s, t, &mcp.Tool{Name: "update_collection", Description: "Update a collection: name, description, icon, color, default view, actions, or archive/unarchive it (archiving an archived collection, or unarchiving an active one, is a conflict). Requires collections:write.",
		Annotations: wr()}, t.updateCollection)
	add(s, t, &mcp.Tool{Name: "delete_collection", Description: "Permanently delete a collection and all its items. confirm must equal the collection name. Requires collections:write.",
		Annotations: destructive()}, t.deleteCollection)

	add(s, t, &mcp.Tool{Name: "delete_item", Description: "Delete an item (its event history is kept; use purge_item to erase that too). Requires items:write.",
		Annotations: destructive()}, t.deleteItem)
	add(s, t, &mcp.Tool{Name: "purge_item", Description: "Erase an item together with its versions, events, pending webhook deliveries and unshared media (e.g. to remove personal data). Requires admin.",
		Annotations: destructive()}, t.purgeItem)
	add(s, t, &mcp.Tool{Name: "execute_bulk_action", Description: "Run one action (approve, reject or a custom action such as publish) on up to 100 items; returns a result per item. approve/reject need items:review, custom actions actions:execute.",
		Annotations: wr()}, t.bulkAction)
	add(s, t, &mcp.Tool{Name: "get_item_versions", Description: "List the prior versions (snapshots) of an item, newest first.", Annotations: ro()}, t.itemVersions)

	add(s, t, &mcp.Tool{Name: "upload_media", Description: "Upload a file (base64) without creating an item; returns a media ID to reference later. Requires items:write.",
		Annotations: wr()}, t.uploadMedia)
	add(s, t, &mcp.Tool{Name: "get_media", Description: "Get a media file's metadata (type, size, name, SHA-256, URL); with include_content=true also its base64 content (files up to 4 MiB).",
		Annotations: ro()}, t.getMedia)
	add(s, t, &mcp.Tool{Name: "delete_media", Description: "Delete a media file; items that reference it lose their media. Requires items:write.",
		Annotations: destructive()}, t.deleteMedia)

	add(s, t, &mcp.Tool{Name: "list_events", Description: "List the activity log (item created, approved, rejected, actions, settings changes), newest first, with optional filters.",
		Annotations: ro()}, t.listEvents)
	add(s, t, &mcp.Tool{Name: "export_events", Description: "Export the activity log as NDJSON or CSV text, oldest first. Output is capped at 1 MiB (truncated=true then); narrow from/to to get the rest.",
		Annotations: ro()}, t.exportEvents)
}

// ---- collections ----

type updateCollectionIn struct {
	Collection  string           `json:"collection" jsonschema:"collection ID (col_...) or exact name"`
	Name        *string          `json:"name,omitempty" jsonschema:"new name"`
	Description *string          `json:"description,omitempty" jsonschema:"new description"`
	Icon        *string          `json:"icon,omitempty" jsonschema:"icon name shown in the sidebar"`
	Color       *string          `json:"color,omitempty" jsonschema:"accent color, e.g. #7c3aed"`
	DefaultView *string          `json:"default_view,omitempty" jsonschema:"gallery, table or list"`
	Actions     *[]app.ActionDef `json:"actions,omitempty" jsonschema:"replaces the custom actions (name, label, event, requires_feedback, set_status, allowed_statuses)"`
	Archived    *bool            `json:"archived,omitempty" jsonschema:"true archives, false unarchives"`
}

func (t *tools) updateCollection(ctx context.Context, _ *mcp.CallToolRequest, in updateCollectionIn) (*mcp.CallToolResult, any, error) {
	id, err := t.collectionID(ctx, in.Collection)
	if err != nil {
		return nil, nil, err
	}
	return nil0(t.svc.UpdateCollection(ctx, t.principal(ctx), id, app.CollectionInput{Name: in.Name, Description: in.Description,
		Icon: in.Icon, Color: in.Color, DefaultView: in.DefaultView, Actions: in.Actions, Archived: in.Archived}))
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

// ---- items ----

func (t *tools) deleteItem(ctx context.Context, _ *mcp.CallToolRequest, in itemRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.ItemID}, t.svc.DeleteItem(ctx, t.principal(ctx), in.ItemID))
}

func (t *tools) purgeItem(ctx context.Context, _ *mcp.CallToolRequest, in itemRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.PurgeItem(ctx, t.principal(ctx), in.ItemID))
}

type bulkIn struct {
	ItemIDs  []string       `json:"item_ids" jsonschema:"1 to 100 item IDs (itm_...)"`
	Action   string         `json:"action" jsonschema:"approve, reject or a custom action name such as publish"`
	Feedback string         `json:"feedback,omitempty" jsonschema:"note stored with each decision; required by reject and some custom actions"`
	Payload  map[string]any `json:"payload,omitempty" jsonschema:"extra JSON passed to webhook receivers"`
}

func (t *tools) bulkAction(ctx context.Context, _ *mcp.CallToolRequest, in bulkIn) (*mcp.CallToolResult, any, error) {
	ai := app.ActionInput{Feedback: in.Feedback}
	if in.Payload != nil {
		ai.Payload, _ = json.Marshal(in.Payload)
	}
	res, err := t.svc.BulkAction(ctx, t.principal(ctx), in.ItemIDs, in.Action, ai)
	return nil, map[string]any{"results": res}, err
}

func (t *tools) itemVersions(ctx context.Context, _ *mcp.CallToolRequest, in itemRef) (*mcp.CallToolResult, any, error) {
	v, err := t.svc.ListItemVersions(ctx, t.principal(ctx), in.ItemID)
	return nil, map[string]any{"versions": v}, err
}

// ---- media ----

type uploadMediaIn struct {
	MediaBase64 string `json:"media_base64" jsonschema:"base64-encoded file (image, video, pdf or any other file)"`
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
		"sha256": m.Sha256, "created_at": m.CreatedAt, "url": strings.TrimRight(t.svc.Cfg.PublicURL, "/") + "/v1/media/" + m.ID}
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

func (t *tools) deleteMedia(ctx context.Context, _ *mcp.CallToolRequest, in mediaRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.MediaID}, t.svc.DeleteMedia(ctx, t.principal(ctx), in.MediaID))
}

// ---- events ----

type listEventsIn struct {
	ItemID     string `json:"item_id,omitempty" jsonschema:"only events about this item (itm_...)"`
	Collection string `json:"collection,omitempty" jsonschema:"collection ID or exact name"`
	Type       string `json:"type,omitempty" jsonschema:"event type, e.g. item.approved"`
	ActorType  string `json:"actor_type,omitempty" jsonschema:"session, api_key, local or system"`
	ActorID    string `json:"actor_id,omitempty" jsonschema:"only events by this actor, e.g. an API key ID (key_...)"`
	TypePrefix string `json:"type_prefix,omitempty" jsonschema:"event type prefix, e.g. webhook."`
	From       string `json:"from,omitempty" jsonschema:"YYYY-MM-DD or RFC3339 (inclusive)"`
	To         string `json:"to,omitempty" jsonschema:"YYYY-MM-DD or RFC3339 (exclusive)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max events (default 50, max 200)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call"`
}

func (t *tools) listEvents(ctx context.Context, _ *mcp.CallToolRequest, in listEventsIn) (*mcp.CallToolResult, any, error) {
	col, err := t.resolveCollection(ctx, in.Collection)
	if err != nil {
		return nil, nil, err
	}
	return nil0(t.svc.ListEvents(ctx, t.principal(ctx), app.EventFilter{ItemID: in.ItemID, CollectionID: col, Type: in.Type,
		ActorType: in.ActorType, ActorID: in.ActorID, TypePrefix: in.TypePrefix, From: in.From, To: in.To,
		Before: in.Cursor, Limit: in.Limit}))
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
	ItemID     string `json:"item_id,omitempty" jsonschema:"only events about this item (itm_...)"`
}

var errExportFull = errors.New("export cap reached")

func (t *tools) exportEvents(ctx context.Context, _ *mcp.CallToolRequest, in exportEventsIn) (*mcp.CallToolResult, any, error) {
	col, err := t.resolveCollection(ctx, in.Collection)
	if err != nil {
		return nil, nil, err
	}
	x, err := t.svc.PrepareEventExport(ctx, t.principal(ctx), app.EventExportFilter{Format: in.Format, From: in.From, To: in.To,
		Type: in.Type, TypePrefix: in.TypePrefix, ActorType: in.ActorType, ActorID: in.ActorID, CollectionID: col, ItemID: in.ItemID})
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
	n, truncated := 0, false
	err = x.Each(ctx, func(e app.Event) error {
		line.Reset()
		if err := render(e); err != nil {
			return err
		}
		if buf.Len()+line.Len() > maxEventExport {
			truncated = true
			return errExportFull
		}
		buf.Write(line.Bytes())
		n++
		return nil
	})
	if err != nil && !errors.Is(err, errExportFull) {
		return nil, nil, err
	}
	return nil, map[string]any{"format": x.Format, "events": n, "truncated": truncated, "content": buf.String()}, nil
}

// done adapts a service call without a result value.
func done(v any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
