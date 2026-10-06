package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestDirectoryHTTPWorkflowAndPagination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	a := app.New(db)
	disk, err := a.CreateDisk(t.Context(), "source", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	importFiles := func(input string, capture int64) error {
		t.Helper()
		file := filepath.Join(t.TempDir(), "input.cshd")
		if err := os.WriteFile(file, []byte("# version 1\n"+input), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := a.Import(t.Context(), app.ImportRequest{
			DiskID: disk, Path: file, CapturedAt: time.Unix(capture, 0),
		})
		return err
	}
	if err := importFiles(",4,sha256,"+fixtureSHA256AB+" foo/a\n"+
		",4,sha256,"+fixtureSHA256AB+" foo/nested/a\n"+
		",,sha256,"+fixtureSHA256CD+" foo/unknown\n"+
		",0,sha256,"+fixtureSHA256EF+" foobar/empty\n"+
		",0,sha256,"+fixtureSHA256EF+" %_*/é\\file\n", 1); err != nil {
		t.Fatal(err)
	}
	handler := New(a)
	document := openAPIDocument(t)
	get := func(path string, status int, target any) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != status {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
			t.Fatal(err)
		}
		if status == 200 {
			var value any
			if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
			route := strings.Split(path, "?")[0]
			parts := strings.Split(route, "/")
			parts[4] = "{id}"
			route = strings.Join(parts, "/")
			operation := document["paths"].(map[string]any)[route].(map[string]any)["get"].(map[string]any)
			resp := operation["responses"].(map[string]any)["200"].(map[string]any)
			if ref, ok := resp["$ref"].(string); ok {
				resp = resolveReference(t, document, ref)
			}
			content := resp["content"].(map[string]any)["application/json"].(map[string]any)
			schema := content["schema"].(map[string]any)
			assertResponseSchema(t, document, schema, value)
		}
	}
	var detail directoryDetailDTO
	get("/api/v1/snapshots/1/directory?path=foo", 200, &detail)
	if detail.Directory.KnownBytes != "8" || detail.Directory.UniqueContentKnownBytes != "4" ||
		detail.Directory.SizeComplete || detail.Directory.UnknownSizeFileCount != "1" ||
		!detail.IsCurrent || detail.Snapshot.CapturedAt != "1970-01-01T00:00:01Z" {
		t.Fatalf("detail: %+v", detail)
	}
	var page directoryPageDTO
	get("/api/v1/snapshots/1/directories?parent=&limit=1", 200, &page)
	if len(page.Items) != 1 || page.Items[0].Path != "%_*" || page.NextCursor == nil ||
		!page.Filters.DirectoriesOnly {
		t.Fatalf("directory page: %+v", page)
	}
	get("/api/v1/snapshots/1/directory/entries?path="+url.QueryEscape("%_*"), 200, &page)
	if len(page.Items) != 1 || page.Items[0].Path != "%_*/é\\file" ||
		page.Items[0].File.Size == nil || *page.Items[0].File.Size != "0" {
		t.Fatalf("literal directory: %+v", page)
	}
	get("/api/v1/snapshots/1/directory/entries?path=foo&other_replicas=99", 200, &page)
	if len(page.Items) != 1 || page.Items[0].Kind != "directory" {
		t.Fatalf("filtered navigation: %+v", page)
	}
	get("/api/v1/snapshots/1/directory/entries?path=foo&recursive=true&"+
		"replica_metric=locations&other_replicas=1", 200, &page)
	if len(page.Items) != 2 || page.Items[0].File.OtherLocationCount != "1" ||
		page.Items[0].File.OtherDiskCount != "0" {
		t.Fatalf("location bounds: %+v", page)
	}
	get("/api/v1/snapshots/1/directory/entries?path=foo&recursive=true&limit=1", 200, &page)
	cursor := *page.NextCursor
	var content contentDTO
	get("/api/v1/contents/"+page.Items[0].File.Observation.ContentID, 200, &content)
	if content.Size == nil || *content.Size != "4" || content.CurrentLocationCount != "2" {
		t.Fatalf("followed content: %+v", content)
	}
	var observation observationSummaryDTO
	get("/api/v1/observations/"+page.Items[0].File.Observation.ID, 200, &observation)
	if observation.OtherLocationCount != "1" {
		t.Fatalf("followed observation: %+v", observation)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = app.New(db)
	handler = New(a)
	next := "/api/v1/snapshots/1/directory/entries?path=foo&recursive=true&limit=1&cursor=" +
		url.QueryEscape(cursor)
	get(next, 200, &page)
	if page.Items[0].Path != "foo/nested/a" {
		t.Fatalf("restart page: %+v", page)
	}
	if err := importFiles(",7,sha256,"+fixtureSHA256CD+" enrichment\nbroken\n", 2); err == nil {
		t.Fatal("malformed import succeeded")
	}
	get(next, 200, &page)
	get("/api/v1/snapshots/1/directory?path=foo", 200, &detail)
	if detail.Directory.SizeComplete {
		t.Fatal("failed import enriched directory")
	}
	if err := importFiles(",7,sha256,"+fixtureSHA256CD+" enrichment\n", 2); err != nil {
		t.Fatal(err)
	}
	var failure errorDTO
	get(next, 409, &failure)
	if failure.Error.Code != "stale_cursor" {
		t.Fatalf("stale cursor: %+v", failure)
	}
	get("/api/v1/snapshots/1/directory?path=foo", 200, &detail)
	if detail.IsCurrent || detail.Directory.KnownBytes != "15" ||
		!detail.Directory.SizeComplete || fmt.Sprint(detail.RedundancyHistogram) != "[{0 3}]" {
		t.Fatalf("historical enriched detail: %+v", detail)
	}
	get("/api/v1/snapshots/1/directory/entries?path=foo&recursive=true&other_replicas=0", 200, &page)
	if len(page.Items) != 3 || page.Items[2].File.OtherLocationCount != "1" {
		t.Fatalf("historical file counts: %+v", page)
	}
	for _, query := range []string{
		"path=/foo", "path=foo/", "path=foo/../a", "path=a&path=b", "scope=current",
		"recursive=1", "recursive=", "replica_metric=bad", "other_replicas=-1",
		"other_replicas=0&min_other_replicas=0", "min_other_replicas=2&max_other_replicas=1",
		"limit=0", "limit=201", "limit=bad", "cursor=bad",
	} {
		get("/api/v1/snapshots/1/directory/entries?"+query, 400, &failure)
		if failure.Error.Code != "invalid_request" {
			t.Fatalf("%s: %+v", query, failure)
		}
	}
	get("/api/v1/snapshots/1/directory?path=absent", 404, &failure)
	get("/api/v1/snapshots/999/directory", 404, &failure)
	get("/api/v1/snapshots/0/directory", 400, &failure)
	for _, route := range []string{"directory", "directories", "directory/entries"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
			"/api/v1/snapshots/1/"+route, nil))
		if response.Code != 405 || response.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("method contract: %d %v", response.Code, response.Header())
		}
	}
}

func TestDirectoryCursorRequestBinding(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := app.New(db)
	disk, err := a.CreateDisk(t.Context(), "disk", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "files.cshd")
	if err := os.WriteFile(file, []byte(",sha256,"+fixtureSHA256AB+" foo/a\n"+
		",sha256,"+fixtureSHA256AB+" foo/b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(t.Context(), app.ImportRequest{
		DiskID: disk, Path: file, CapturedAt: time.Unix(1, 0),
	}); err != nil {
		t.Fatal(err)
	}
	page, err := a.ListDirectoryEntries(t.Context(), app.ListDirectoryEntriesRequest{
		Filters: base.DirectoryFilters{SnapshotID: 1, Path: "foo"}, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(a)
	for _, query := range []string{
		"path=foo&limit=2", "path=&limit=1", "path=foo&recursive=true&limit=1",
		"path=foo&replica_metric=locations&limit=1", "path=foo&other_replicas=0&limit=1",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
			"/api/v1/snapshots/1/directory/entries?"+query+"&cursor="+page.NextCursor, nil))
		if response.Code != 400 {
			t.Fatalf("changed cursor context accepted: %s: %d %s", query,
				response.Code, response.Body.String())
		}
	}
}
