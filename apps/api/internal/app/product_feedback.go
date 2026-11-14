package app

import (
	"bytes"
	"context"
	"image"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/db"
)

const MaxFeedbackAttachmentBytes = 5 << 20
const MaxFeedbackAttachments = 10

// Product feedback belongs to the platform operator, not the workspace's media library or event stream.
type ProductFeedbackInput struct {
	Kind        string
	Description string
	PagePath    string
}

type ProductFeedback struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	WorkspaceName   string `json:"workspace_name"`
	ReporterID      string `json:"reporter_id"`
	ReporterName    string `json:"reporter_name"`
	ReporterEmail   string `json:"reporter_email"`
	Kind            string `json:"kind"`
	Description     string `json:"description"`
	PagePath        string `json:"page_path"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	Attachment      bool   `json:"attachment"`
	AttachmentCount int64  `json:"attachment_count"`
}

// CreateProductFeedback accepts people (all workspace roles) and local installs; API keys cannot submit as a person.
func (s *Service) CreateProductFeedback(ctx context.Context, p Principal, in ProductFeedbackInput, screenshots []io.Reader) (string, error) {
	if p.WorkspaceID == "" {
		return "", ErrUnauthorized
	}
	if p.Kind != PrincipalSession && p.Kind != PrincipalLocal {
		return "", &Error{Code: "forbidden", Status: 403, Message: "feedback requires a signed-in person"}
	}
	in.Description = strings.TrimSpace(in.Description)
	if in.Kind != "problem" && in.Kind != "feature" {
		return "", invalid("kind", "choose problem or feature")
	}
	if !utf8.ValidString(in.Description) || utf8.RuneCountInString(in.Description) < 1 || utf8.RuneCountInString(in.Description) > 5000 || strings.ContainsRune(in.Description, 0) {
		return "", invalid("description", "use 1 to 5000 characters")
	}
	// Decode then keep only the path, even when an older/malicious client sends a full URL.
	u, err := url.Parse(in.PagePath)
	if err != nil || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.ContainsAny(u.Path, "\x00\r\n\\") {
		return "", invalid("page_path", "use a page path starting with /")
	}
	pagePath := u.EscapedPath()
	if len(pagePath) > 2048 {
		return "", invalid("page_path", "use at most 2048 bytes")
	}
	reporter := p.UserID
	if reporter == "" {
		reporter = p.Kind + ":" + p.ID
	}
	if len(screenshots) > MaxFeedbackAttachments {
		return "", invalid("screenshot", "attach at most 10 images")
	}
	id := newID("fb")
	attachments := make([]db.CreateProductFeedbackAttachmentParams, 0, len(screenshots))
	saved := false
	defer func() {
		if !saved {
			for _, attachment := range attachments {
				_ = s.Cfg.Blob.Delete(context.WithoutCancel(ctx), attachment.StorageKey)
			}
		}
	}()
	for i, screenshot := range screenshots {
		data, err := io.ReadAll(io.LimitReader(screenshot, MaxFeedbackAttachmentBytes+1))
		if err != nil {
			return "", err
		}
		if len(data) == 0 || len(data) > MaxFeedbackAttachmentBytes {
			return "", invalid("screenshot", "use an image up to 5 MB")
		}
		ct := http.DetectContentType(data)
		ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[ct]
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if ext == "" || err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 20_000_000 {
			return "", invalid("screenshot", "use a PNG, JPEG or WebP image up to 20 megapixels")
		}
		key := workspaceKey(p.WorkspaceID) + "feedback/" + id + "/" + strconv.Itoa(i) + ext
		attachments = append(attachments, db.CreateProductFeedbackAttachmentParams{FeedbackID: id, Position: int64(i), StorageKey: key, MimeType: ct})
		if err := s.Cfg.Blob.Put(ctx, key, bytes.NewReader(data), int64(len(data)), ct); err != nil {
			return "", err
		}
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		n, err := q.CountProductFeedbackSince(ctx, db.CountProductFeedbackSinceParams{ReporterID: reporter, CreatedAt: formatTime(s.Now().Add(-time.Hour))})
		if p.UserID == "" && err == nil {
			n, err = q.CountWorkspaceProductFeedbackSince(ctx, db.CountWorkspaceProductFeedbackSinceParams{WorkspaceID: p.WorkspaceID, CreatedAt: formatTime(s.Now().Add(-time.Hour))})
		}
		if err != nil {
			return err
		}
		if n >= 5 {
			return &Error{Code: "rate_limited", Status: 429, Message: "send at most 5 feedback reports per hour; try again later"}
		}
		if err := q.CreateProductFeedback(ctx, db.CreateProductFeedbackParams{ID: id, WorkspaceID: p.WorkspaceID, ReporterID: reporter, Kind: in.Kind, Description: in.Description, PagePath: pagePath, CreatedAt: s.now()}); err != nil {
			return err
		}
		for _, attachment := range attachments {
			if err := q.CreateProductFeedbackAttachment(ctx, attachment); err != nil {
				return err
			}
		}
		return nil
	})
	saved = err == nil
	return id, err
}

func (s *Service) requireFeedbackAdmin(ctx context.Context, a Admin) error {
	if !s.AdminEnabled() {
		return ErrUnauthorized
	}
	ok, err := s.isAdmin(ctx, a.Email)
	if err != nil {
		return err
	}
	if !ok {
		return &Error{Code: "forbidden", Status: 403, Message: "platform administrator required"}
	}
	return nil
}

type ProductFeedbackPage struct {
	Items      []ProductFeedback `json:"items"`
	NextCursor string            `json:"next_cursor"`
}

func (s *Service) AdminListProductFeedback(ctx context.Context, a Admin, cursor string) (ProductFeedbackPage, error) {
	page := ProductFeedbackPage{Items: []ProductFeedback{}}
	if err := s.requireFeedbackAdmin(ctx, a); err != nil {
		return page, err
	}
	at, id, err := decodeCursor(cursor)
	if err != nil {
		return page, err
	}
	rows, err := s.Q.AdminListProductFeedback(ctx, db.AdminListProductFeedbackParams{CursorAt: at, CursorID: id, PageLimit: 51})
	if err != nil {
		return page, err
	}
	if len(rows) > 50 {
		rows = rows[:50]
		last := rows[49]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	for _, r := range rows {
		page.Items = append(page.Items, ProductFeedback{ID: r.ID, WorkspaceID: r.WorkspaceID, WorkspaceName: r.WorkspaceName, ReporterID: r.ReporterID, ReporterName: r.ReporterName, ReporterEmail: r.ReporterEmail, Kind: r.Kind, Description: r.Description, PagePath: r.PagePath, Status: r.Status, CreatedAt: r.CreatedAt, Attachment: r.AttachmentCount > 0, AttachmentCount: r.AttachmentCount})
	}
	return page, nil
}

func (s *Service) AdminSetProductFeedbackStatus(ctx context.Context, a Admin, id, status string) error {
	if err := s.requireFeedbackAdmin(ctx, a); err != nil {
		return err
	}
	if status != "new" && status != "in_progress" && status != "closed" {
		return invalid("status", "choose new, in_progress or closed")
	}
	return s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetProductFeedback(ctx, id)
		if isNoRows(err) {
			return notFound("feedback")
		}
		if err != nil {
			return err
		}
		if _, err := q.SetProductFeedbackStatus(ctx, db.SetProductFeedbackStatusParams{ID: id, Status: status}); err != nil {
			return err
		}
		return q.InsertAdminAudit(ctx, db.InsertAdminAuditParams{ID: newID("ada"), Email: a.Email, Action: "feedback_status", WorkspaceID: row.WorkspaceID, Target: id, Details: `{"status":"` + status + `"}`, CreatedAt: s.now()})
	})
}

func (s *Service) AdminOpenProductFeedbackAttachment(ctx context.Context, a Admin, id string, index int64) (io.ReadSeekCloser, string, error) {
	if err := s.requireFeedbackAdmin(ctx, a); err != nil {
		return nil, "", err
	}
	if index < 0 || index >= MaxFeedbackAttachments {
		return nil, "", invalid("index", "choose an attachment from 0 to 9")
	}
	row, err := s.Q.GetProductFeedbackAttachment(ctx, db.GetProductFeedbackAttachmentParams{FeedbackID: id, Position: index})
	if isNoRows(err) {
		return nil, "", notFound("attachment")
	}
	if err != nil {
		return nil, "", err
	}
	file, err := s.Cfg.Blob.Open(ctx, row.StorageKey)
	return file, row.MimeType, err
}
