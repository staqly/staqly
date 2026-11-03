package adminapi_test

import (
	"encoding/json"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func TestProductFeedbackAdminAuthorization(t *testing.T) {
	e := newEnv(t, "a@x.io")
	ws := e.workspace()
	id, err := e.svc.CreateProductFeedback(t.Context(), app.Principal{Kind: app.PrincipalSession, WorkspaceID: ws}, app.ProductFeedbackInput{Kind: "feature", Description: "Idea", PagePath: "/"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.want(e.do("GET", "/v1/admin/feedback", nil), 401)
	e.want(e.do("GET", "/v1/admin/feedback/"+id+"/attachment", nil), 401)
	e.want(e.do("PATCH", "/v1/admin/feedback/"+id, map[string]string{"status": "closed"}), 401)
	e.signIn()
	w := e.do("GET", "/v1/admin/feedback", nil)
	e.want(w, 200)
	var page app.ProductFeedbackPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("page: %+v %v", page, err)
	}
	csrf := e.csrf
	e.csrf = ""
	e.want(e.do("PATCH", "/v1/admin/feedback/"+id, map[string]string{"status": "closed"}), 403)
	e.csrf = csrf
	e.want(e.do("PATCH", "/v1/admin/feedback/"+id, map[string]string{"status": "closed"}), 204)
	e.want(e.do("GET", "/v1/admin/feedback/"+id+"/attachment", nil), 404)
}
