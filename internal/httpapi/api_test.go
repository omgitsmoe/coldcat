package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestContentWorkflow(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { db.Close() })
	a := app.New(db)

	var firstDisk base.DiskId
	for i := range 3 {
		disk, err := a.CreateDisk(t.Context(), fmt.Sprintf("disk-%d", i), "", "", 100)
		if err != nil {
			t.Fatal(err)
		}

		if i == 0 {
			firstDisk = disk
		}

		input := "# version 1\n,0,sha256,ab zéro/a\n"
		if i == 0 {
			input += ",0,sha256,ab backup/a\n,,md5,ab unrelated\n"
		}

		file := filepath.Join(t.TempDir(), "fixture.cshd")
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}

		if _, err := a.Import(t.Context(), app.ImportRequest{DiskID: disk, Path: file, CapturedAt: time.Unix(10, 123)}); err != nil {
			t.Fatal(err)
		}
	}

	server := httptest.NewServer(New(a))
	defer server.Close()
	document := openAPIDocument(t)
	get := func(path string, status int, dst any) {
		t.Helper()
		resp, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			data, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: %d %s", path, resp.StatusCode, data)
		}

		if resp.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing JSON content type")
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}

		if err := json.Unmarshal(data, dst); err != nil {
			t.Fatal(err)
		}

		if status == 200 {
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}

			route := strings.Split(path, "?")[0]
			if route != "/healthz" && route != "/api/v1/contents/lookup" &&
				route != "/api/v1/contents" {
				parts := strings.Split(route, "/")
				parts[4] = "{id}"
				route = strings.Join(parts, "/")
			}

			operation := document["paths"].(map[string]any)[route].(map[string]any)["get"].(map[string]any)
			response := operation["responses"].(map[string]any)["200"].(map[string]any)
			if ref, ok := response["$ref"].(string); ok {
				response = resolveReference(t, document, ref)
			}

			schema := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
			assertResponseSchema(t, document, schema, value)
		}
	}

	var content contentDTO
	var contents contentPageDTO
	get("/api/v1/contents?other_replicas=2", 200, &contents)
	if len(contents.Items) != 1 || contents.Items[0].DiskCount != "3" {
		t.Fatalf("redundancy list: %+v", contents)
	}
	get("/api/v1/contents/"+contents.Items[0].ID, 200, &content)
	get("/api/v1/contents/lookup?hash_type=sha256&hash=AB", 200, &content)
	if content.Hash.Hex != "ab" || content.Size == nil || *content.Size != "0" ||
		content.LocationCount != "4" ||
		content.DiskCount != "3" ||
		content.Scope != "current" {
		t.Fatalf("lookup: %+v", content)
	}

	var detail contentDTO
	get("/api/v1/contents/"+content.ID, 200, &detail)
	if detail.ID != content.ID {
		t.Fatal("wrong content detail")
	}

	next := "/api/v1/contents/" + content.ID + "/observations?limit=1"
	seen := map[string]bool{}

	var first observationDTO
	for next != "" {
		var page observationPageDTO
		get(next, 200, &page)
		if len(page.Items) != 1 || page.Scope != "current" || page.Revision != "3" {
			t.Fatalf("page: %+v", page)
		}

		item := page.Items[0]
		if seen[item.Observation.ID] || !item.IsCurrent || item.Observation.MTime != nil {
			t.Fatalf("bad observation: %+v", item)
		}

		seen[item.Observation.ID] = true
		if first.ID == "" {
			first = item.Observation
		}

		next = ""
		if page.NextCursor != nil {
			next = "/api/v1/contents/" + content.ID + "/observations?limit=1&cursor=" + *page.NextCursor
		}
	}

	if len(seen) != 4 {
		t.Fatalf("locations: %v", seen)
	}

	var observation observationSummaryDTO
	get("/api/v1/observations/"+first.ID, 200, &observation)
	if observation.OtherLocationCount != "3" || observation.OtherDiskCount != "2" ||
		observation.Disk.ID != fmt.Sprint(firstDisk) ||
		observation.Snapshot.CapturedAt != "1970-01-01T00:00:10.000000123Z" {
		t.Fatalf("observation: %+v", observation)
	}

	var snapshot snapshotDTO
	get("/api/v1/snapshots/"+observation.Snapshot.ID, 200, &snapshot)
	if snapshot.State != "complete" || snapshot.FileCount != "3" {
		t.Fatalf("snapshot: %+v", snapshot)
	}

	var unknown contentDTO
	get("/api/v1/contents/lookup?hash_type=md5&hash=ab", 200, &unknown)
	if unknown.Size != nil || unknown.ID == content.ID {
		t.Fatalf("unknown size/algorithm identity: %+v", unknown)
	}

	var health map[string]string
	get("/healthz", 200, &health)
	for _, path := range []string{
		"/api/v1/contents/0", "/api/v1/contents/9223372036854775808", "/api/v1/contents/1?scope=", "/api/v1/contents/1?scope=invalid",
		"/api/v1/contents/lookup?hash_type=unsupported&hash=ab", "/api/v1/contents/lookup?hash_type=sha256&hash=xyz",
		"/api/v1/contents/1/observations?limit=0", "/api/v1/contents/1/observations?limit=201", "/api/v1/contents/1/observations?limit=1&limit=2",
		"/api/v1/contents/1/observations?cursor=bad", "/api/v1/contents/1?unknown=1",
	} {
		var result errorDTO
		get(path, 400, &result)
		if result.Error.Code != "invalid_request" {
			t.Fatalf("%s: %+v", path, result)
		}
	}

	for _, path := range []string{"/missing", "/api/v1/contents/999", "/api/v1/observations/999", "/api/v1/snapshots/999", "/api/v1/contents/lookup?hash_type=sha256&hash=ff"} {
		var result errorDTO
		get(path, 404, &result)
		if result.Error.Code != "not_found" {
			t.Fatalf("%s: %+v", path, result)
		}
	}

	resp, err := server.Client().Post(server.URL+"/api/v1/contents/1", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp.Body.Close()
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("method: %d", resp.StatusCode)
	}

	release, err := db.AcquireImport(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	var unavailable errorDTO
	get("/healthz", 503, &unavailable)
	release()
	if unavailable.Error.Code != "catalog_unavailable" {
		t.Fatalf("readiness: %+v", unavailable)
	}
}

func TestWireIntegers(t *testing.T) {
	result, err := contentResponse(
		base.ContentSummary{
			Content: base.Content{
				Id:       9007199254740993,
				HashType: mustHash(t),
				Hash:     []byte{0xab},
			},
			LocationCount: 9007199254740993,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["id"] != "9007199254740993" || decoded["location_count"] != "9007199254740993" ||
		decoded["size"] != nil {
		t.Fatalf("unsafe encoding: %s", data)
	}
}

func mustHash(t *testing.T) base.HashType {
	t.Helper()
	h, err := base.FromIdentifier("sha256")
	if err != nil {
		t.Fatal(err)
	}

	return h
}

func TestHTTPPaginationAfterRestartAndImports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { db.Close() })
	a := app.New(db)
	disk, err := a.CreateDisk(t.Context(), "disk", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(t.TempDir(), "fixture.cshd")
	if err := os.WriteFile(file, []byte(",sha256,ab a\n,sha256,ab b\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Import(t.Context(), app.ImportRequest{DiskID: disk, Path: file, CapturedAt: time.Unix(10, 0)}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(a))
	t.Cleanup(func() { server.Close() })
	request := func(route string, status int, dst any) {
		t.Helper()
		resp, err := server.Client().Get(server.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: %d %s", route, resp.StatusCode, body)
		}

		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			t.Fatal(err)
		}
	}

	var first observationPageDTO
	request("/api/v1/contents/1/observations?limit=1", 200, &first)
	if first.NextCursor == nil {
		t.Fatal("missing cursor")
	}

	route := "/api/v1/contents/1/observations?limit=1&cursor=" + *first.NextCursor
	server.Close()
	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	a = app.New(db)
	server = httptest.NewServer(New(a))

	var next observationPageDTO
	request(route, 200, &next)
	if next.Items[0].Observation.ID == first.Items[0].Observation.ID {
		t.Fatal("repeated observation after restart")
	}

	var mismatch errorDTO
	request(route+"&scope=history", 400, &mismatch)
	server.Close()
	if err := os.WriteFile(file, []byte(",sha256,ab a\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Import(t.Context(), app.ImportRequest{DiskID: disk, Path: file, CapturedAt: time.Unix(5, 0)}); err != nil {
		t.Fatal(err)
	}

	server = httptest.NewServer(New(a))

	var stale errorDTO
	request(route, 409, &stale)
	if stale.Error.Code != "stale_cursor" {
		t.Fatalf("stale: %+v", stale)
	}

	var history observationPageDTO
	request("/api/v1/contents/1/observations?scope=history", 200, &history)
	if len(history.Items) != 3 || !history.Items[0].IsCurrent || history.Items[2].IsCurrent {
		t.Fatalf("history: %+v", history)
	}

	var content contentDTO
	request("/api/v1/contents/1?scope=history", 200, &content)
	if content.ObservationCount != "3" || content.LocationCount != "2" ||
		content.CurrentLocationCount != "2" {
		t.Fatalf("scope counts: %+v", content)
	}

	server.Close()
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Import(t.Context(), app.ImportRequest{DiskID: disk, Path: file, CapturedAt: time.Unix(20, 0)}); err != nil {
		t.Fatal(err)
	}

	server = httptest.NewServer(New(a))

	var empty observationPageDTO
	request("/api/v1/contents/1/observations", 200, &empty)
	if len(empty.Items) != 0 || empty.Items == nil || empty.NextCursor != nil {
		t.Fatalf("empty current list: %+v", empty)
	}

	request("/api/v1/contents/1", 200, &content)
	if content.LocationCount != "0" || content.DiskCount != "0" {
		t.Fatalf("historical-only counts: %+v", content)
	}
}

func TestHTTPIncompleteVisibilityRecoveryAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { db.Close() })
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance) VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit');
INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',X'AB');
INSERT INTO observation(id,snapshot_id,content_id,path) VALUES(1,1,1,'unfinished');
INSERT INTO import_content(snapshot_id,content_id) VALUES(1,1);`); err != nil {
		t.Fatal(err)
	}

	handler := New(app.New(db))
	for _, route := range []string{"/api/v1/snapshots/1", "/api/v1/contents/1", "/api/v1/contents/1/observations", "/api/v1/observations/1", "/api/v1/contents/lookup?hash_type=sha256&hash=ab"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", route, nil))
		if response.Code != 404 {
			t.Fatalf("incomplete resource %s: %d", route, response.Code)
		}
	}

	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	handler = New(app.New(db))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/healthz", nil))
	if response.Code != 200 {
		t.Fatalf("readiness after recovery: %d", response.Code)
	}

	var count int
	if err := raw.QueryRow("SELECT COUNT(*) FROM observation").Scan(&count); err != nil ||
		count != 0 {
		t.Fatalf("recovery before serving: %d %v", count, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response = httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/contents/1/observations", nil).
			WithContext(ctx),
	)
	if response.Body.Len() != 0 {
		t.Fatalf("wrote response for cancelled query: %s", response.Body.String())
	}

	db.Close()
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/healthz", nil))
	if response.Code != 503 {
		t.Fatalf("closed catalog readiness: %d", response.Code)
	}
}
