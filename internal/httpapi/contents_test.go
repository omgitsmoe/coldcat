package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestContentListHTTPContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := app.New(db)
	disk, err := a.CreateDisk(t.Context(), "source", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "fixture.cshd")
	importFiles := func(input string, captured int64, success bool) {
		t.Helper()
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := a.Import(t.Context(), app.ImportRequest{
			DiskID: disk, Path: file, CapturedAt: time.Unix(captured, 0),
		})
		if (err == nil) != success {
			t.Fatalf("import: %v", err)
		}
	}
	importFiles("# version 1\n,,sha256,ab foo/a\n,,sha256,ab foo/copy\n,0,md5,ab foobar/b\n", 10, true)
	handler := New(a)
	document := openAPIDocument(t)
	operation := document["paths"].(map[string]any)["/api/v1/contents"].(map[string]any)["get"].(map[string]any)
	media := operation["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)
	get := func(query string, status int) contentPageDTO {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/contents"+query, nil))
		if recorder.Code != status {
			t.Fatalf("%s: %d %s", query, recorder.Code, recorder.Body)
		}
		if recorder.Header().Get("Content-Type") != "application/json" {
			t.Fatal("content type")
		}
		var page contentPageDTO
		if status == 200 {
			if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			var value any
			if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
			assertResponseSchema(t, document, media["schema"].(map[string]any), value)
		} else {
			var body errorDTO
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			want := map[int]string{400: "invalid_request", 404: "not_found", 409: "stale_cursor"}[status]
			if body.Error.Code != want {
				t.Fatalf("error: %+v", body)
			}
		}
		return page
	}
	page := get("?limit=1", 200)
	if len(page.Items) != 1 || page.NextCursor == nil || page.Items[0].Size != nil ||
		page.Filters.ReplicaMetric != base.ReplicaDisks {
		t.Fatalf("first page: %+v", page)
	}
	cursor := *page.NextCursor
	get("?limit=2&cursor="+cursor, 400)
	get("?scope=history&limit=1&cursor="+cursor, 400)
	page = get("?replica_metric=locations&other_replicas=1", 200)
	if len(page.Items) != 1 || page.Items[0].LocationCount != "2" {
		t.Fatalf("locations: %+v", page)
	}
	page = get("?replica_metric=disks&max_other_replicas=0", 200)
	if len(page.Items) != 2 {
		t.Fatalf("disks: %+v", page)
	}
	for _, directory := range []string{"foo", ""} {
		page = get(fmt.Sprintf("?disk_id=%d&directory=%s", disk, url.QueryEscape(directory)), 200)
		want := 1
		if directory == "" {
			want = 2
		}
		if len(page.Items) != want {
			t.Fatalf("directory: %+v", page)
		}
	}
	page = get(fmt.Sprintf("?disk_id=%d&directory=missing", disk), 200)
	if len(page.Items) != 0 || page.Items == nil || page.NextCursor != nil {
		t.Fatalf("empty: %+v", page)
	}
	for _, query := range []string{
		"?scope=", "?scope=bad", "?scope=history&other_replicas=0", "?replica_metric=bad",
		"?replica_metric=", "?other_replicas=-1", "?other_replicas=9223372036854775808",
		"?min_other_replicas=no", "?min_other_replicas=2&max_other_replicas=1",
		"?other_replicas=1&min_other_replicas=1", "?other_replicas=1&other_replicas=1",
		"?disk_id=0", "?disk_id=-1", "?disk_id=9223372036854775808", "?directory=foo",
		"?directory=", "?disk_id=1&directory=../foo", "?disk_id=1&directory=foo/",
		"?disk_id=1&directory=/foo", "?limit=0", "?limit=201", "?limit=1&limit=1",
		"?disk_id=1&directory=C:/foo",
		"?cursor=", "?cursor=!", "?unexpected=1", "?directory=%ZZ",
	} {
		get(query, 400)
	}
	get("?disk_id=999", 404)
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(method, "/api/v1/contents", nil))
		if r.Code != 405 || r.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("method: %d", r.Code)
		}
	}
	importFiles(",sha256,cd failed\ninvalid\n", 20, false)
	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = app.New(db)
	handler = New(a)
	page = get("?limit=1&cursor="+cursor, 200)
	if len(page.Items) != 1 || page.NextCursor != nil || page.Items[0].Size == nil ||
		*page.Items[0].Size != "0" {
		t.Fatalf("reopen page: %+v", page)
	}
	importFiles("", 20, true)
	get("?limit=1&cursor="+cursor, 409)
	page = get("?scope=history", 200)
	if len(page.Items) != 2 || page.Items[0].CurrentLocationCount != "0" {
		t.Fatalf("history: %+v", page)
	}
	page = get("", 200)
	if len(page.Items) != 0 {
		t.Fatalf("current after deletion: %+v", page)
	}
}
