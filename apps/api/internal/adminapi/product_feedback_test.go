package adminapi_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"strconv"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/blob"
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

func TestProductFeedbackAdminMultipleDownloads(t *testing.T) {
	e := newEnv(t, "a@x.io")
	ws := e.workspace()
	e.svc.Cfg.Blob = blob.NewDisk(t.TempDir())
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	images := make([]io.Reader, 10)
	for i := range images {
		images[i] = bytes.NewReader(data.Bytes())
	}
	id, err := e.svc.CreateProductFeedback(t.Context(), app.Principal{Kind: app.PrincipalSession, WorkspaceID: ws}, app.ProductFeedbackInput{Kind: "problem", Description: "Screenshots", PagePath: "/"}, images)
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/admin/feedback/" + id + "/attachment"
	e.want(e.do("GET", path+"?index=9", nil), 401)
	e.signIn()
	for i := range 10 {
		w := e.do("GET", path+"?index="+strconv.Itoa(i), nil)
		e.want(w, 200)
		if !bytes.Equal(w.Body.Bytes(), data.Bytes()) || w.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("wrong image %d", i)
		}
	}
	e.want(e.do("GET", path, nil), 200)
	e.want(e.do("GET", path+"?index=10", nil), 422)
	e.want(e.do("GET", path+"?index=-1", nil), 422)
	e.want(e.do("GET", path+"?index=bad", nil), 400)
}
