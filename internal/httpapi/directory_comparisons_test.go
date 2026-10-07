package httpapi

import (
	"encoding/json"
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

func TestDirectoryComparisonHTTPContract(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	a := app.New(db)
	importFiles := func(label, input string) {
		t.Helper()

		disk, err := a.CreateDisk(t.Context(), label, "", "", 100)
		if err != nil {
			t.Fatal(err)
		}

		file := filepath.Join(t.TempDir(), label+".cshd")
		if err := os.WriteFile(file, []byte("# version 1\n"+input), 0600); err != nil {
			t.Fatal(err)
		}

		if _, err := a.Import(t.Context(), app.ImportRequest{
			DiskID: disk, Path: file, CapturedAt: time.Unix(1, 0),
		}); err != nil {
			t.Fatal(err)
		}
	}

	importFiles("source",
		",4,sha256,"+fixtureSHA256AB+" source/a.txt\n"+
			",2,sha256,"+fixtureSHA256CD+" source/debug.log\n")
	importFiles("copy",
		",4,sha256,"+fixtureSHA256AB+" renamed/a.txt\n"+
			",2,sha256,"+fixtureSHA256EF+" renamed/debug.log\n")

	handler := New(a)
	document := openAPIDocument(t)
	get := func(path, schemaName string, status int, target any) {
		t.Helper()

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != status {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
			t.Fatal(err)
		}

		if status == http.StatusOK {
			var value any
			if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}

			assertResponseSchema(t, document, map[string]any{
				"$ref": "#/components/schemas/" + schemaName,
			}, value)
		}
	}

	var replicas directoryReplicaPageDTO
	get("/api/v1/snapshots/1/directory/replicas?path=source&block="+
		url.QueryEscape("**/*.log"), "DirectoryReplicaPage", http.StatusOK, &replicas)
	if len(replicas.Items) != 1 || replicas.Items[0].Path != "renamed" ||
		replicas.Items[0].WholeTreeEqual || replicas.Selection.RetainedFileCount != "1" {
		t.Fatalf("replicas: %+v", replicas)
	}

	var coverage directoryCoveragePageDTO
	get("/api/v1/snapshots/1/directory/coverage?path=source&allow=*.txt&allow="+
		url.QueryEscape("**/*.txt"), "DirectoryCoveragePage", http.StatusOK, &coverage)
	if len(coverage.Items) != 1 || !coverage.Items[0].Complete ||
		len(coverage.Filters.Allow) != 2 {
		t.Fatalf("coverage: %+v", coverage)
	}

	var failure errorDTO
	for _, query := range []string{
		"path=source&allow=[", "path=source&allow=", "path=source&path=other",
		"path=source&allow=a&allow=a&limit=0", "path=source&unknown=a",
	} {
		get("/api/v1/snapshots/1/directory/replicas?"+query, "", http.StatusBadRequest,
			&failure)
		if failure.Error.Code != "invalid_request" {
			t.Fatalf("%s: %+v", query, failure)
		}
	}

	for _, route := range []string{"replicas", "coverage"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
			"/api/v1/snapshots/1/directory/"+route, nil))
		if response.Code != http.StatusMethodNotAllowed ||
			response.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("%s method contract: %d %v", route, response.Code, response.Header())
		}
	}
}
