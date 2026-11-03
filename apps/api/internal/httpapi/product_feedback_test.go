package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

func TestProductFeedbackSubmission(t *testing.T) {
	e := newEnv(t)
	submit := func(c *http.Client, csrf, auth string, want int) string {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("kind", "problem")
		_ = mw.WriteField("description", "Something broke")
		_ = mw.WriteField("page_path", "/posts?token=secret#hidden")
		_ = mw.Close()
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/feedback", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("status %d want %d: %s", res.StatusCode, want, data)
		}
		var out struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(data, &out)
		return out.ID
	}
	submit(http.DefaultClient, "", "", 401)
	submit(e.browser, "", "", 403)
	submit(http.DefaultClient, "", e.admin, 403)
	id := submit(e.browser, e.csrf, "", 201)
	row, err := e.svc.Q.GetProductFeedback(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if row.PagePath != "/posts" || row.WorkspaceID == "" || row.ReporterID == "" {
		t.Fatalf("row: %+v", row)
	}
	// No reading endpoints exist on the workspace API, even for its admin.
	for _, path := range []string{"/v1/feedback", "/v1/feedback/" + id, "/v1/feedback/" + id + "/attachment", "/v1/admin/feedback", "/v1/admin/feedback/" + id + "/attachment"} {
		res := e.do(e.browser, "GET", path, "", nil, 0, nil)
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			t.Fatalf("workspace can read %s", path)
		}
	}
}

func TestProductFeedbackAccountRolesAndIdentity(t *testing.T) {
	e := newAccountsEnv(t, "")
	owner, admin, editor, viewer, outsider := e.team()
	for _, p := range []*person{owner, admin, editor, viewer, outsider} {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("kind", "feature")
		_ = mw.WriteField("description", "An idea")
		_ = mw.WriteField("page_path", "/schedule?token=private#hidden")
		_ = mw.WriteField("workspace_id", "forged")
		_ = mw.WriteField("reporter_id", "forged")
		_ = mw.Close()
		req, _ := http.NewRequest("POST", e.srv.URL+"/v1/feedback", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-CSRF-Token", p.csrf)
		res, err := p.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 201 {
			t.Fatalf("%s: %d %s", p.id.Email, res.StatusCode, data)
		}
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		row, err := e.svc.Q.GetProductFeedback(t.Context(), out.ID)
		if err != nil {
			t.Fatal(err)
		}
		u, err := e.svc.Q.GetUser(t.Context(), row.ReporterID)
		if err != nil || u.Email != p.id.Email {
			t.Fatalf("reporter: %+v %v", u, err)
		}
		if row.WorkspaceID == "forged" || row.PagePath != "/schedule" {
			t.Fatalf("metadata: %+v", row)
		}
	}
}
