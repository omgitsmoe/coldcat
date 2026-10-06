package httpapi

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestDuplicateImportPreservesCursorAcrossReopen(t *testing.T) {
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
	file := filepath.Join(t.TempDir(), "inventory.cshd")
	if err := os.WriteFile(file, []byte(",sha256,ab a\n,sha256,ab b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := app.ImportRequest{DiskID: disk, Path: file, CapturedAt: time.Unix(10, 0)}
	first, err := a.Import(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	get := func(route string, status int, result any) {
		t.Helper()
		response := httptest.NewRecorder()
		New(a).ServeHTTP(response, httptest.NewRequest("GET", route, nil))
		if response.Code != status {
			t.Fatalf("%s: %d %s", route, response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), result); err != nil {
			t.Fatal(err)
		}
	}
	var page observationPageDTO
	get("/api/v1/contents/1/observations?limit=1", 200, &page)
	if page.NextCursor == nil {
		t.Fatal("missing cursor")
	}
	route := "/api/v1/contents/1/observations?limit=1&cursor=" + *page.NextCursor
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = app.New(db)
	_, err = a.Import(t.Context(), req)
	var duplicate *database.DuplicateImportError
	if !errors.As(err, &duplicate) || duplicate.SnapshotID != first.Id {
		t.Fatalf("duplicate after reopen: %v", err)
	}
	var next observationPageDTO
	get(route, 200, &next)
	if len(next.Items) != 1 || next.Items[0].Observation.ID == page.Items[0].Observation.ID {
		t.Fatalf("cursor after duplicate: %+v", next)
	}
	req.AllowRepeat = true
	if _, err := a.Import(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	var stale errorDTO
	get(route, 409, &stale)
	if stale.Error.Code != "stale_cursor" {
		t.Fatalf("repeat did not invalidate cursor: %+v", stale)
	}
}
