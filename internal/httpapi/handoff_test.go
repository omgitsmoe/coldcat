package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestOpenAPIEndpointHandoff(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := app.New(db)
	var diskID, snapshotID string
	for _, label := range []string{"source", "replica"} {
		disk, err := a.CreateDisk(t.Context(), label, "", "", 100)
		if err != nil {
			t.Fatal(err)
		}

		input := filepath.Join(t.TempDir(), "inventory.cshd")
		records := "# version 1\n,0,sha256," + fixtureSHA256AB +
			" docs/report.txt\n,,md5," + fixtureMD5AB + " docs/unknown.txt\n"
		if err := os.WriteFile(input, []byte(records), 0600); err != nil {
			t.Fatal(err)
		}

		snapshot, err := a.Import(t.Context(), app.ImportRequest{
			DiskID: disk, Path: input, CapturedAt: time.Unix(10, 0),
		})
		if err != nil {
			t.Fatal(err)
		}
		if label == "source" {
			diskID, snapshotID = decimal(disk), decimal(snapshot.Id)
		}
	}

	server := httptest.NewServer(New(a))
	t.Cleanup(server.Close)
	document := openAPIDocument(t)
	paths := document["paths"].(map[string]any)
	covered := map[string]bool{}
	request := func(t *testing.T, method, route, path, body string, status int) any {
		t.Helper()
		operation := paths[route].(map[string]any)[strings.ToLower(method)].(map[string]any)
		response, ok := operation["responses"].(map[string]any)[strconv.Itoa(status)].(map[string]any)
		if !ok {
			t.Fatalf("%s %s: undocumented status %d", method, route, status)
		}
		if ref, ok := response["$ref"].(string); ok {
			response = resolveReference(t, document, ref)
		}

		wireMethod := method
		if status == http.StatusMethodNotAllowed {
			wireMethod = http.MethodDelete
		}
		req, err := http.NewRequestWithContext(
			t.Context(), wireMethod, server.URL+path, strings.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status || resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("%s %s: status %d, body %s", method, path, resp.StatusCode, data)
		}
		if status == http.StatusMethodNotAllowed {
			allow := "GET, HEAD"
			switch route {
			case "/api/v1/disks":
				allow = "GET, HEAD, POST"
			case "/api/v1/disks/{id}":
				allow = "GET, HEAD, PATCH"
			}
			if resp.Header.Get("Allow") != allow {
				t.Fatalf("%s: Allow = %q, want %q", path, resp.Header.Get("Allow"), allow)
			}
		}

		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		media := response["content"].(map[string]any)["application/json"].(map[string]any)
		assertResponseSchema(t, document, media["schema"].(map[string]any), value)
		if status < 300 {
			covered[strings.ToLower(method)+" "+route] = true
		}
		return value
	}

	result := request(t, "GET", "/api/v1/search", "/api/v1/search?q=REPORT", "", 200)
	items := result.(map[string]any)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("search returned %d items, want both disks", len(items))
	}
	first := items[0].(map[string]any)
	contentID := first["content"].(map[string]any)["id"].(string)
	observationID := first["observation"].(map[string]any)["id"].(string)

	for _, test := range []struct {
		method, route, path, body string
		status                    int
	}{
		{"GET", "/healthz", "/healthz", "", 200},
		{"GET", "/api/v1/catalog", "/api/v1/catalog", "", 200},
		{"GET", "/api/v1/disks", "/api/v1/disks", "", 200},
		{"POST", "/api/v1/disks", "/api/v1/disks", `{"label":"empty","capacity":"0"}`, 201},
		{"GET", "/api/v1/disks/{id}", "/api/v1/disks/" + diskID, "", 200},
		{"PATCH", "/api/v1/disks/{id}", "/api/v1/disks/" + diskID, `{"notes":"offline"}`, 200},
		{"GET", "/api/v1/disks/{id}/snapshots", "/api/v1/disks/" + diskID + "/snapshots", "", 200},
		{"GET", "/api/v1/snapshots/{id}", "/api/v1/snapshots/" + snapshotID, "", 200},
		{"GET", "/api/v1/contents", "/api/v1/contents", "", 200},
		{"GET", "/api/v1/contents/lookup",
			"/api/v1/contents/lookup?hash_type=sha256&hash=" + fixtureSHA256AB, "", 200},
		{"GET", "/api/v1/contents/{id}", "/api/v1/contents/" + contentID, "", 200},
		{"GET", "/api/v1/contents/{id}/observations",
			"/api/v1/contents/" + contentID + "/observations", "", 200},
		{"GET", "/api/v1/observations/{id}", "/api/v1/observations/" + observationID, "", 200},
		{"GET", "/api/v1/search", "/api/v1/search?q=REPORT", "", 200},
		{"GET", "/api/v1/snapshots/{id}/directories",
			"/api/v1/snapshots/" + snapshotID + "/directories", "", 200},
		{"GET", "/api/v1/snapshots/{id}/directory",
			"/api/v1/snapshots/" + snapshotID + "/directory?path=docs", "", 200},
		{"GET", "/api/v1/snapshots/{id}/directory/entries",
			"/api/v1/snapshots/" + snapshotID + "/directory/entries?path=docs", "", 200},
		{"GET", "/api/v1/snapshots/{id}/directory/replicas",
			"/api/v1/snapshots/" + snapshotID + "/directory/replicas?path=docs", "", 200},
		{"GET", "/api/v1/snapshots/{id}/directory/coverage",
			"/api/v1/snapshots/" + snapshotID + "/directory/coverage?path=docs", "", 200},
	} {
		t.Run(test.method+" "+test.route, func(t *testing.T) {
			request(t, test.method, test.route, test.path, test.body, test.status)
			invalidPath := test.path + "?unexpected=1"
			if strings.Contains(test.path, "?") {
				invalidPath = test.path + "&unexpected=1"
			}
			request(t, test.method, test.route, invalidPath, test.body, 400)
			request(t, test.method, test.route, test.path, "", 405)
		})
	}

	for route, item := range paths {
		for method := range item.(map[string]any) {
			if !covered[method+" "+route] {
				t.Errorf("missing endpoint contract case: %s %s", method, route)
			}
		}
	}
}

func TestOpenAPISuccessExamples(t *testing.T) {
	document := openAPIDocument(t)
	for route, item := range document["paths"].(map[string]any) {
		for method, value := range item.(map[string]any) {
			for status, value := range value.(map[string]any)["responses"].(map[string]any) {
				if !strings.HasPrefix(status, "2") {
					continue
				}
				response := value.(map[string]any)
				if ref, ok := response["$ref"].(string); ok {
					response = resolveReference(t, document, ref)
				}
				media := response["content"].(map[string]any)["application/json"].(map[string]any)
				_, example := media["example"]
				examples, _ := media["examples"].(map[string]any)
				if !example && len(examples) == 0 {
					t.Errorf("%s %s %s: missing success example", method, route, status)
				}
			}
		}
	}
}
