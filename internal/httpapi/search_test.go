package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestSearchHTTPWorkflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := app.New(db)
	var firstSnapshot string
	for i, label := range []string{"source", "second", "third"} {
		disk, err := a.CreateDisk(t.Context(), label, "", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		input := "# version 1\n5,0,sha256," +
			fixtureSHA256AB + " foo/report.txt\n"
		if i == 0 {
			input += ",0,sha256," + fixtureSHA256AB + " backup/report.txt\n" +
				",,md5," + fixtureMD5AB + " foobar/report.txt\n"
		}
		file := filepath.Join(t.TempDir(), "input.cshd")
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := a.Import(t.Context(), app.ImportRequest{
			DiskID: disk, Path: file, CapturedAt: time.Unix(10, 0),
		})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstSnapshot = decimal(snapshot.Id)
		}
	}
	server := httptest.NewServer(New(a))
	t.Cleanup(func() { server.Close() })
	document := openAPIDocument(t)
	get := func(path string, status int, dst any, route string) {
		t.Helper()
		resp, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, data)
		}
		if err := json.Unmarshal(data, dst); err != nil {
			t.Fatal(err)
		}
		if status == 200 {
			operation := document["paths"].(map[string]any)[route].(map[string]any)["get"].(map[string]any)
			response := operation["responses"].(map[string]any)["200"].(map[string]any)
			if ref, ok := response["$ref"].(string); ok {
				response = resolveReference(t, document, ref)
			}
			schema := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			assertResponseSchema(t, document, schema, value)
		}
	}
	var page searchPageDTO
	get("/api/v1/search?q=report&limit=1", 200, &page, "/api/v1/search")
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("search page: %+v", page)
	}
	first := page.Items[0]
	if first.Observation.Path != "backup/report.txt" || first.Content.LocationCount != "4" ||
		first.Content.DiskCount != "3" || first.Content.Size == nil || *first.Content.Size != "0" ||
		first.Observation.MTime != nil || first.Relevance != "prefix" || !first.IsCurrent {
		t.Fatalf("search result: %+v", first)
	}
	cursor := *page.NextCursor
	get("/api/v1/search?q=report&limit=1&cursor="+url.QueryEscape(cursor), 200,
		&page, "/api/v1/search")
	if page.Items[0].Observation.ID == first.Observation.ID {
		t.Fatal("pagination repeated observation")
	}
	var content contentDTO
	get("/api/v1/contents/"+first.Content.ID, 200, &content, "/api/v1/contents/{id}")
	if content.LocationCount != "4" || content.DiskCount != "3" {
		t.Fatalf("content: %+v", content)
	}
	var locations observationPageDTO
	count := 0
	locationURL := "/api/v1/contents/" + content.ID + "/observations?limit=1"
	for {
		get(locationURL, 200, &locations, "/api/v1/contents/{id}/observations")
		count += len(locations.Items)
		for _, item := range locations.Items {
			if item.Snapshot.CapturedAt != "1970-01-01T00:00:10Z" ||
				item.Snapshot.ImportedAt == item.Snapshot.CapturedAt {
				t.Fatal("inventory dates")
			}
			if item.Observation.Path == "foo/report.txt" &&
				(item.Observation.MTime == nil || *item.Observation.MTime != "1970-01-01T00:00:05Z") {
				t.Fatal("source date")
			}
		}
		if locations.NextCursor == nil {
			break
		}
		locationURL = "/api/v1/contents/" + content.ID + "/observations?limit=1&cursor=" +
			url.QueryEscape(*locations.NextCursor)
	}
	if count != 4 {
		t.Fatalf("locations: %d", count)
	}
	var observation observationSummaryDTO
	get("/api/v1/observations/"+first.Observation.ID, 200, &observation, "/api/v1/observations/{id}")
	if observation.OtherDiskCount != "2" || observation.OtherLocationCount != "3" {
		t.Fatalf("observation: %+v", observation)
	}
	var snapshot snapshotDTO
	get("/api/v1/snapshots/"+first.Observation.SnapshotID, 200, &snapshot, "/api/v1/snapshots/{id}")
	if snapshot.CapturedAt != first.Snapshot.CapturedAt || snapshot.ImportedAt != first.Snapshot.ImportedAt {
		t.Fatal("snapshot context")
	}
	get("/api/v1/search?q=report&snapshot_id="+firstSnapshot+"&directory=foo", 200,
		&page, "/api/v1/search")
	if len(page.Items) != 1 || page.Items[0].Content.DiskCount != "3" {
		t.Fatal("directory membership changed catalog-wide counts")
	}
	for _, query := range []string{
		"", "q=", "q=a&q=b", "q=a&field=bad", "q=a&match=fuzzy",
		"q=a&disk_id=0", "q=a&snapshot_id=-1", "q=a&directory=foo",
		"q=a&scope=bad", "q=a&limit=201", "q=a&cursor=bad", "q=a&unknown=x",
		"q=a&other_replicas=-1", "q=a&other_replicas=0&min_other_replicas=0",
		"q=a&min_other_replicas=2&max_other_replicas=1", "q=a&scope=history&other_replicas=0",
		"q=%FF", "q=%00", "q=a&replica_metric=bad",
		"q=a&disk_id=2&snapshot_id=" + firstSnapshot,
	} {
		var failure errorDTO
		get("/api/v1/search?"+query, 400, &failure, "")
		if failure.Error.Code != "invalid_request" {
			t.Fatalf("%s: %+v", query, failure)
		}
	}
	for _, query := range []string{"q=a&disk_id=9999", "q=a&snapshot_id=9999"} {
		var failure errorDTO
		get("/api/v1/search?"+query, 404, &failure, "")
	}
	for _, method := range []string{http.MethodHead, http.MethodPost} {
		r, err := http.NewRequest(method, server.URL+"/api/v1/search?q=report", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := 200
		if method == http.MethodPost {
			want = 405
		}
		if resp.StatusCode != want {
			t.Fatalf("%s: %d", method, resp.StatusCode)
		}
	}
	server.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = app.New(db)
	server = httptest.NewServer(New(a))
	get("/api/v1/search?q=report&limit=1&cursor="+url.QueryEscape(cursor), 200,
		&page, "/api/v1/search")
	file := filepath.Join(t.TempDir(), "next.cshd")
	if err := os.WriteFile(file, []byte(",sha256,"+fixtureSHA256AB+" newest/report.txt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(t.Context(), app.ImportRequest{
		DiskID: 1, Path: file, CapturedAt: time.Unix(20, 0),
	}); err != nil {
		t.Fatal(err)
	}
	var failure errorDTO
	get("/api/v1/search?q=report&limit=1&cursor="+url.QueryEscape(cursor), 409, &failure, "")
	if failure.Error.Code != "stale_cursor" {
		t.Fatalf("stale cursor: %+v", failure)
	}
}
