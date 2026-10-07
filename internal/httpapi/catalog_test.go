package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestCatalogHTTPContract(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := app.New(db)
	server := httptest.NewServer(New(a))
	t.Cleanup(server.Close)
	document := openAPIDocument(t)
	operation := document["paths"].(map[string]any)["/api/v1/catalog"].(map[string]any)["get"].(map[string]any)
	responses := operation["responses"].(map[string]any)
	for _, status := range []string{"200", "400", "405", "500", "503"} {
		if _, ok := responses[status]; !ok {
			t.Fatalf("missing OpenAPI response %s", status)
		}
	}
	request := func(method, suffix string, status int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+"/api/v1/catalog"+suffix, nil)
		if err != nil {
			t.Fatal(err)
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
			t.Fatalf("response: %d %s", resp.StatusCode, data)
		}
		if status == 405 && resp.Header.Get("Allow") != "GET, HEAD" {
			t.Fatal("missing Allow header")
		}
		if method != "HEAD" {
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			schema := map[string]any{"$ref": "#/components/schemas/Error"}
			if status == 200 {
				schema["$ref"] = "#/components/schemas/CatalogSummary"
			}
			assertResponseSchema(t, document, schema, value)
		}
		return data
	}
	check := func(disks string) {
		t.Helper()
		data := request("GET", "", 200)
		var got catalogDTO
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		want := catalogDTO{
			Revision: "0", Scope: base.ScopeCurrent,
			DiskCount: disks, FileCount: "0", ContentCount: "0",
		}
		if got != want {
			t.Fatalf("summary: %+v, want %+v", got, want)
		}
	}
	check("0")
	if _, err := a.CreateDisk(t.Context(), "empty", "", "", 0); err != nil {
		t.Fatal(err)
	}
	check("1")
	file := filepath.Join(t.TempDir(), "fixture.cshd")
	input := "# version 1\n,0,sha256," + fixtureSHA256AB + " empty\n" +
		",0,sha256," + fixtureSHA256AB + " copy\n" +
		",,md5," + fixtureMD5AB + " unknown\n"
	if err := os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := a.Import(t.Context(), app.ImportRequest{
		DiskID: 1, Path: file, CapturedAt: time.Unix(10, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	var populated catalogDTO
	if err := json.Unmarshal(request("GET", "", 200), &populated); err != nil {
		t.Fatal(err)
	}
	if populated.Revision != decimal(snapshot.Id) || populated.DiskCount != "1" ||
		populated.FileCount != "3" || populated.ContentCount != "2" ||
		populated.Scope != base.ScopeCurrent {
		t.Fatalf("populated summary: %+v", populated)
	}
	if data := request("HEAD", "", 200); len(data) != 0 {
		t.Fatalf("HEAD body: %s", data)
	}
	for _, suffix := range []string{"?scope=history", "?q=", "?q=a&q=b", "?q=%ZZ"} {
		request("GET", suffix, 400)
	}
	request("POST", "", 405)
	release, err := db.AcquireImport(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	request("GET", "", 503)
	release()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	request("GET", "", 500)
}

func TestCatalogDecimalEncoding(t *testing.T) {
	const large int64 = 9007199254740993
	got := catalogResponse(base.CatalogSummary{
		Catalog:   base.CatalogState{Revision: base.SnapshotId(large)},
		DiskCount: large, FileCount: large, ContentCount: large,
	})
	if got.Revision != "9007199254740993" || got.DiskCount != got.Revision ||
		got.FileCount != got.Revision || got.ContentCount != got.Revision {
		t.Fatalf("unsafe integer conversion: %+v", got)
	}
}
