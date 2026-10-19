package app

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// The workspace time zone (workspaces.timezone, an IANA name, default UTC): post repeats keep their local
// wall-clock time in it across DST, and a schedule rule sent without a zone runs in it.

// checkTimezone accepts an IANA zone name the server knows ("Local" and "" are not names).
func checkTimezone(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || v == "Local" || len(v) > 64 {
		return "", invalid("timezone", "must be an IANA time zone name such as Europe/Warsaw or UTC")
	}
	if _, err := time.LoadLocation(v); err != nil {
		return "", invalid("timezone", "unknown time zone "+v)
	}
	return v, nil
}

// localZoneName is this machine's zone as an IANA name, for the local workspace: TZ when set (empty = UTC,
// as Go reads it), else the /etc/localtime link; UTC when neither gives a name the server knows.
func localZoneName() string {
	name := ""
	if tz, ok := os.LookupEnv("TZ"); ok {
		name = strings.TrimPrefix(tz, ":")
	} else if n := time.Local.String(); n != "Local" {
		name = n
	} else if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, after, ok := strings.Cut(target, "zoneinfo/"); ok {
			name = after
		}
	}
	if v, err := checkTimezone(name); err == nil {
		return v
	}
	return "UTC"
}

// workspaceLocation is the workspace's zone (UTC when it can't be read).
func (s *Service) workspaceLocation(ctx context.Context, q *db.Queries, workspaceID string) *time.Location {
	w, err := q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return time.UTC
	}
	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// setWorkspaceTimezone stores a zone on a workspace being created (CreateWorkspace leaves the default, UTC).
func (s *Service) setWorkspaceTimezone(ctx context.Context, q *db.Queries, ws *db.Workspace, tz string) error {
	if tz == "" || tz == "UTC" {
		ws.Timezone = "UTC"
		return nil
	}
	ws.Timezone = tz
	return q.UpdateWorkspaceTimezone(ctx, db.UpdateWorkspaceTimezoneParams{Timezone: tz, UpdatedAt: ws.CreatedAt, ID: ws.ID})
}
