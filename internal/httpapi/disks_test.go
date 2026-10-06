package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestHTTPDiskManagement(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := app.New(db)
	server := httptest.NewServer(New(a))
	t.Cleanup(func() { server.Close() })
	document := openAPIDocument(t)
	request := func(method, route, body, contentType string, status int, schema string, dst any) http.Header {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+route, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if method == "HEAD" {
			data, err := io.ReadAll(resp.Body)
			if err != nil || len(data) != 0 || resp.StatusCode != status {
				t.Fatalf("HEAD: %s %v status %d", data, err, resp.StatusCode)
			}
			return resp.Header
		}
		var value any
		if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status || resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("%s %s: status %d want %d; %#v", method, route, resp.StatusCode, status, value)
		}
		assertResponseSchema(t, document,
			resolveReference(t, document, "#/components/schemas/"+schema), value)
		if dst != nil {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, dst); err != nil {
				t.Fatal(err)
			}
		}
		return resp.Header
	}
	var page diskPageDTO
	request("GET", "/api/v1/disks", "", "", 200, "DiskPage", &page)
	if page.Items == nil || len(page.Items) != 0 || page.NextCursor != nil || page.Revision != "0" {
		t.Fatalf("empty: %+v", page)
	}
	var created diskDetailDTO
	headers := request("POST", "/api/v1/disks",
		`{"label":"Archive α","capacity":"9223372036854775807","notes":"keep","serial":"s"}`,
		"application/json; charset=utf-8", 201, "DiskDetail", &created)
	route := "/api/v1/disks/" + created.ID
	if headers.Get("Location") != route || created.Notes == nil || *created.Notes != "keep" ||
		created.Capacity != "9223372036854775807" || created.LatestSnapshot != nil ||
		created.Cataloged != nil {
		t.Fatalf("created: %+v headers %v", created, headers)
	}
	request("POST", "/api/v1/disks", `{"label":"other","capacity":"0"}`,
		"application/json", 201, "DiskDetail", nil)
	request("POST", "/api/v1/disks", `{"label":"Archive α","capacity":"0"}`,
		"application/json", 409, "Error", nil)
	var detail diskDetailDTO
	request("GET", route, "", "", 200, "DiskDetail", &detail)
	if !reflect.DeepEqual(created, detail) {
		t.Fatalf("create/detail mismatch: %+v %+v", created, detail)
	}
	request("PATCH", route, `{"label":"other","notes":"changed"}`,
		"application/json", 409, "Error", nil)
	request("GET", route, "", "", 200, "DiskDetail", &detail)
	if !reflect.DeepEqual(created, detail) {
		t.Fatalf("conflict changed metadata: %+v", detail)
	}
	request("GET", "/api/v1/disks?limit=1", "", "", 200, "DiskPage", &page)
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("first page: %+v", page)
	}
	continuation := "/api/v1/disks?limit=1&cursor=" + url.QueryEscape(*page.NextCursor)
	request("PATCH", route, `{"label":"renamed","notes":null,"capacity":"0"}`,
		"application/json", 200, "DiskDetail", &detail)
	if detail.Label != "renamed" || detail.Notes != nil || detail.Serial == nil || *detail.Serial != "s" {
		t.Fatalf("patch: %+v", detail)
	}
	request("PATCH", route, `{"serial":""}`, "application/json", 200, "DiskDetail", &detail)
	if detail.Serial != nil {
		t.Fatalf("empty serial: %+v", detail)
	}
	request("GET", continuation, "", "", 200, "DiskPage", &page)
	if len(page.Items) != 1 || page.Items[0].ID != "2" || page.NextCursor != nil {
		t.Fatalf("last page: %+v", page)
	}
	server.Close()
	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = app.New(db)
	server = httptest.NewServer(New(a))
	request("GET", continuation, "", "", 200, "DiskPage", nil)
	file := filepath.Join(t.TempDir(), "inventory.cshd")
	if err := os.WriteFile(file, []byte(",sha256,"+fixtureSHA256AB+" bad\ninvalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ImportByLabel(ctx, "renamed", app.ImportRequest{
		Path: file, CapturedAt: time.Unix(10, 0),
	}); err == nil {
		t.Fatal("accepted invalid input")
	}
	request("GET", continuation, "", "", 200, "DiskPage", nil)
	input := "# version 1\n,10,sha256," + fixtureSHA256AB + " a\n" +
		",10,sha256," + fixtureSHA256AB + " copy\n" +
		",0,sha256," + fixtureSHA256CD + " empty\n,,sha256," + fixtureSHA256EF + " unknown\n"
	if err := os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := a.ImportByLabel(ctx, "renamed", app.ImportRequest{
		Path: file, CapturedAt: time.Unix(10, 123),
	})
	if err != nil {
		t.Fatal(err)
	}
	request("GET", continuation, "", "", 409, "Error", nil)
	request("GET", route, "", "", 200, "DiskDetail", &detail)
	if detail.LatestSnapshot == nil || detail.LatestSnapshot.ID != decimal(snapshot.Id) ||
		detail.LatestSnapshot.CapturedAt != "1970-01-01T00:00:10.000000123Z" ||
		detail.Cataloged == nil || *detail.Cataloged != (diskCatalogedDTO{
		FileCount: "4", ContentCount: "3", KnownBytes: "20", UnknownSizeFileCount: "1",
	}) {
		t.Fatalf("inventory detail: %+v", detail)
	}
	request("GET", "/api/v1/snapshots/"+detail.LatestSnapshot.ID,
		"", "", 200, "Snapshot", nil)
	request("GET", route+"/snapshots", "", "", 200, "SnapshotPage", nil)
	request("PATCH", route, `{"label":"final label"}`, "application/json", 200, "DiskDetail", nil)
	var observation observationSummaryDTO
	request("GET", "/api/v1/observations/1", "", "", 200, "ObservationSummary", &observation)
	if observation.Disk.Label != "final label" || observation.Snapshot.ID != decimal(snapshot.Id) {
		t.Fatalf("observation context: %+v", observation)
	}
	for _, route := range []string{"/api/v1/disks", "/api/v1/disks/1", "/api/v1/disks/1/snapshots"} {
		request("HEAD", route, "", "", 200, "", nil)
	}
	for _, test := range []struct{ route, allow string }{
		{"/api/v1/disks", "GET, HEAD, POST"},
		{"/api/v1/disks/1", "GET, HEAD, PATCH"},
		{"/api/v1/disks/1/snapshots", "GET, HEAD"},
		{"/api/v1/contents", "GET, HEAD"},
	} {
		headers := request("DELETE", test.route, "", "", 405, "Error", nil)
		if headers.Get("Allow") != test.allow {
			t.Fatalf("Allow %s: %s", test.route, headers.Get("Allow"))
		}
	}
	for _, body := range []string{
		`{}`, `null`, `[]`, `{`, `{"label":""}`, `{"label":null}`, `{"label":3}`,
		`{"capacity":null}`, `{"capacity":0}`, `{"capacity":"-1"}`,
		`{"capacity":"+1"}`, `{"capacity":"1KB"}`, `{"capacity":""}`,
		`{"capacity":"9223372036854775808"}`, `{"capacity":" 1"}`,
		`{"notes":false}`, `{"id":"2"}`, `{"label":"x"} {}`,
	} {
		request("PATCH", route, body, "application/json", 400, "Error", nil)
	}
	for _, body := range []string{
		`{}`, `null`, `{"label":"x"}`, `{"capacity":"1"}`, `{"label":null,"capacity":"0"}`,
		`{"label":"","capacity":"0"}`, `{"label":"x","capacity":0}`,
		`{"label":"x","capacity":"0","unknown":true}`,
	} {
		request("POST", "/api/v1/disks", body, "application/json", 400, "Error", nil)
	}
	for _, media := range []string{"", "text/plain", "application/json; broken"} {
		request("PATCH", route, `{"label":"x"}`, media, 415, "Error", nil)
	}
	request("PATCH", route, `{"notes":"`+strings.Repeat("a", 65536)+`"}`,
		"application/json", 413, "Error", nil)
	for _, suffix := range []string{
		"?limit=0", "?limit=201", "?limit=x", "?limit=", "?limit=1&limit=1",
		"?cursor=", "?cursor=!", "?cursor=a&cursor=b", "?scope=current", "?limit=%ZZ",
	} {
		request("GET", "/api/v1/disks"+suffix, "", "", 400, "Error", nil)
	}
	for _, id := range []string{"0", "-1", "x", "9223372036854775808", "999"} {
		status := 400
		if id == "999" {
			status = 404
		}
		for _, method := range []string{"GET", "PATCH"} {
			request(method, "/api/v1/disks/"+id, `{"label":"x"}`,
				"application/json", status, "Error", nil)
		}
	}
	request("PATCH", route+"?x=1", `{"label":"x"}`, "application/json", 400, "Error", nil)
	request("POST", "/api/v1/disks?x=1", `{"label":"x","capacity":"0"}`,
		"application/json", 400, "Error", nil)
	release, err := db.AcquireImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, method := range []string{"GET", "PATCH"} {
		request(method, route, `{"label":"x"}`, "application/json", 503, "Error", nil)
	}
	request("GET", "/api/v1/disks", "", "", 503, "Error", nil)
	request("POST", "/api/v1/disks", `{"label":"x","capacity":"0"}`,
		"application/json", 503, "Error", nil)
}

func TestDiskOpenAPIContract(t *testing.T) {
	document := openAPIDocument(t)
	paths := document["paths"].(map[string]any)
	for _, test := range []struct {
		path, method, success, schema string
		statuses                      []string
	}{
		{"/api/v1/disks", "get", "200", "DiskPage", []string{"400", "405", "409", "500", "503"}},
		{"/api/v1/disks", "post", "201", "DiskDetail",
			[]string{"400", "405", "409", "413", "415", "500", "503"}},
		{"/api/v1/disks/{id}", "get", "200", "DiskDetail",
			[]string{"400", "404", "405", "500", "503"}},
		{"/api/v1/disks/{id}", "patch", "200", "DiskDetail",
			[]string{"400", "404", "405", "409", "413", "415", "500", "503"}},
	} {
		operation := paths[test.path].(map[string]any)[test.method].(map[string]any)
		responses := operation["responses"].(map[string]any)
		for _, status := range append(test.statuses, test.success) {
			if _, ok := responses[status]; !ok {
				t.Fatalf("%s %s missing %s", test.method, test.path, status)
			}
		}
		response := responses[test.success].(map[string]any)
		media := response["content"].(map[string]any)["application/json"].(map[string]any)
		if media["schema"].(map[string]any)["$ref"] != "#/components/schemas/"+test.schema {
			t.Fatalf("response schema %s %s", test.method, test.path)
		}
		if test.method == "post" || test.method == "patch" {
			body := operation["requestBody"].(map[string]any)
			if body["required"] != true {
				t.Fatalf("%s body must be required", test.method)
			}
		}
	}
}
