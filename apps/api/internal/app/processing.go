package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// defaultProcessingTimeout: an item waits this long in "processing" for its
// automation to report a result, then fails. A workspace can set its own
// value (1 minute to 24 hours); otherwise PROCESSING_TIMEOUT applies (0 = never).
const (
	defaultProcessingTimeout = time.Hour
	MinProcessingTimeout     = time.Minute
	MaxProcessingTimeout     = 24 * time.Hour
)

// serverProcessingTimeout is PROCESSING_TIMEOUT; 0 means never (also for an
// invalid value, so items never fail by accident).
func (s *Service) serverProcessingTimeout() time.Duration {
	v := s.Env("PROCESSING_TIMEOUT", "")
	if v == "" {
		return defaultProcessingTimeout
	}
	d, err := time.ParseDuration(v)
	if v == "0" || err != nil || d < 0 {
		return 0
	}
	return d
}

// processingTimeout is the workspace's own limit if set, else the server's.
func (s *Service) processingTimeout(col sql.NullInt64) time.Duration {
	if col.Valid && col.Int64 > 0 {
		return time.Duration(col.Int64) * time.Second
	}
	return s.serverProcessingTimeout()
}

// failStaleProcessing marks items that have been processing for longer than
// their workspace's timeout as failed, so a lost automation doesn't leave them
// waiting forever. Runs with the expiry tick.
// ponytail: "processing since" is updated_at, so an edit while processing
// restarts the clock; add a processing_since column if that matters.
func (s *Service) failStaleProcessing(ctx context.Context) {
	// Only rows older than the shortest possible limit can be due.
	shortest := MinProcessingTimeout
	if d := s.serverProcessingTimeout(); d > 0 && d < shortest {
		shortest = d
	}
	now := s.Now()
	rows, err := s.Q.StaleProcessingItems(ctx, formatTime(now.Add(-shortest)))
	if err != nil {
		return
	}
	for _, d := range rows {
		ttl := s.processingTimeout(d.ProcessingTimeoutSeconds)
		if d.ItemTimeoutSeconds.Valid && d.ItemTimeoutSeconds.Int64 > 0 {
			ttl = time.Duration(d.ItemTimeoutSeconds.Int64) * time.Second
		}
		since, err := time.Parse(timeFormat, d.UpdatedAt)
		if ttl == 0 || err != nil || now.Sub(since) < ttl {
			continue
		}
		s.failProcessing(ctx, d.WorkspaceID, d.ID, ttl)
	}
}

func (s *Service) failProcessing(ctx context.Context, workspaceID, itemID string, ttl time.Duration) {
	msg := "No result reported within " + formatTimeout(ttl)
	actor := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "processing-timeout", Name: "Processing timeout"}
	err := s.tx(ctx, func(q *db.Queries) error {
		_, err := s.mutateItem(ctx, q, actor, itemID, nil, func(it *db.Item, _ Collection) (string, map[string]any, error) {
			if it.Status != "processing" {
				return "", nil, conflict("no longer processing")
			}
			if _, err := s.applyFields(ctx, q, it, itemFields{MetadataPatch: map[string]json.RawMessage{
				"error": json.RawMessage(mustJSON(msg))}}); err != nil {
				return "", nil, err
			}
			it.Status = "failed"
			return "item.failed", map[string]any{"error": msg, "timeout": true}, nil
		})
		return err
	})
	if err != nil && !isConflict(err) {
		s.logger(ctx).Warn("processing timeout", "workspace_id", workspaceID, "item_id", itemID, "err", err)
	}
}

// formatTimeout writes 1h, 90m or 24h rather than Go's 1h0m0s.
func formatTimeout(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}
