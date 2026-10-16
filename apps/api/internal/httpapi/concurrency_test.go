package httpapi_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// TestConcurrentItemWritesNeverBusy reproduces the perf seeding run that got
// HTTP 500 SQLITE_BUSY: many parallel writers creating items (JSON and
// multipart with a file) through the HTTP handler and the service, while
// readers list and search. No request may fail with a 5xx.
func TestConcurrentItemWritesNeverBusy(t *testing.T) {
	svc, srv := newLocal(t)
	ctx := context.Background()
	p, err := svc.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := "Load"
	col, err := svc.CreateCollection(ctx, p, app.CollectionInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	const writers, perWriter = 24, 12
	var fails atomic.Int32
	var mu sync.Mutex
	var msgs []string
	fail := func(format string, a ...any) {
		fails.Add(1)
		mu.Lock()
		if len(msgs) < 10 {
			msgs = append(msgs, fmt.Sprintf(format, a...))
		}
		mu.Unlock()
	}
	post := func(body io.Reader, ct string) {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/collections/"+col.ID+"/items", body)
		req.Header.Set("Content-Type", ct)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fail("request: %v", err)
			return
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 201 {
			fail("POST item: %d %s", resp.StatusCode, raw)
		}
	}
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for r := range 4 {
		readers.Go(func() {
			paths := []string{"/v1/items?limit=50", "/v1/items?q=seed&limit=50", "/v1/collections/" + col.ID + "/items?limit=100", "/v1/events?limit=50"}
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				resp, err := http.Get(srv.URL + paths[(r+i)%len(paths)])
				if err != nil {
					fail("GET: %v", err)
					return
				}
				raw, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 500 {
					fail("GET %s: %d %s", paths[(r+i)%len(paths)], resp.StatusCode, raw)
				}
			}
		})
	}
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for i := range perWriter {
				title := fmt.Sprintf("seed %d-%d", w, i)
				switch (w + i) % 3 {
				case 0: // JSON over HTTP
					post(strings.NewReader(`{"title":"`+title+`","content":"seed body text"}`), "application/json")
				case 1: // multipart: file + item in one call
					body, ct := multipartBody(t, map[string]string{"item": `{"title":"` + title + `"}`}, "a.png", pngBytes)
					post(body, ct)
				default: // service directly, with a separately uploaded file
					m, err := svc.UploadMedia(ctx, p, "b.png", strings.NewReader(string(pngBytes)))
					if err != nil {
						fail("upload: %v", err)
						continue
					}
					mid := m.ID
					in := app.CreateItemInput{}
					in.Title, in.MediaID = &title, &mid
					if _, err := svc.CreateItem(ctx, p, col.ID, in); err != nil {
						fail("service create: %v", err)
					}
				}
			}
		})
	}
	wg.Wait()
	close(stop)
	readers.Wait()
	if n := fails.Load(); n > 0 {
		t.Fatalf("%d failures, e.g.:\n%s", n, strings.Join(msgs, "\n"))
	}
	total, cursor := 0, ""
	for {
		page, err := svc.ListItems(ctx, p, app.ItemFilter{CollectionID: col.ID, Limit: 100, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		total += len(page.Data)
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if total != writers*perWriter {
		t.Fatalf("got %d items, want %d", total, writers*perWriter)
	}
}
