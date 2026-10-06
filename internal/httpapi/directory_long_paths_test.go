package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestDirectoryLongPathsRemainBrowsableWithCompactCursors(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := app.New(db)
	disk, err := a.CreateDisk(t.Context(), "long paths", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	longFile := strings.Repeat("a", 13000)
	longDirectory := strings.Repeat("b", 13000)
	nestedDirectory := strings.Repeat("segment/", 150) + "leaf"
	paths := []string{longFile, longDirectory + "/a", longDirectory + "/z",
		nestedDirectory + "/file", "z"}
	var input strings.Builder
	for _, path := range paths {
		input.WriteString(",sha256," + fixtureSHA256AB + " " + path + "\n")
	}
	file := filepath.Join(t.TempDir(), "files.cshd")
	if err := os.WriteFile(file, []byte(input.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(t.Context(), app.ImportRequest{
		DiskID: disk, Path: file, CapturedAt: time.Unix(1, 0),
	}); err != nil {
		t.Fatal(err)
	}
	handler := New(a)
	get := func(path string, target any) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != 200 {
			t.Fatalf("response: %d %s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
			t.Fatal(err)
		}
	}
	var page directoryPageDTO
	request := "/api/v1/snapshots/1/directory/entries?limit=1"
	var entries []string
	for {
		get(request, &page)
		entries = append(entries, page.Items[0].Path)
		if page.NextCursor == nil {
			break
		}
		if len(*page.NextCursor) > 1024 {
			t.Fatalf("cursor includes a long path: %d bytes", len(*page.NextCursor))
		}
		request = "/api/v1/snapshots/1/directory/entries?limit=1&cursor=" + *page.NextCursor
	}
	if len(entries) != 4 || entries[0] != longFile || entries[1] != longDirectory ||
		entries[2] != "segment" || entries[3] != "z" {
		t.Fatalf("failed to traverse root: %d entries", len(entries))
	}
	for _, path := range []string{longDirectory, nestedDirectory} {
		var detail directoryDetailDTO
		get("/api/v1/snapshots/1/directory?path="+url.QueryEscape(path), &detail)
		if detail.Directory.Path != path {
			t.Fatal("directory spelling changed")
		}
		get("/api/v1/snapshots/1/directories?parent="+url.QueryEscape(path), &page)
		if len(page.Items) != 0 {
			t.Fatalf("unexpected children: %+v", page.Items)
		}
	}
	request = "/api/v1/snapshots/1/directory/entries?limit=1&path=" + url.QueryEscape(longDirectory)
	get(request, &page)
	if page.Items[0].Path != longDirectory+"/a" || page.NextCursor == nil ||
		len(*page.NextCursor) > 1024 {
		t.Fatal("long directory cannot produce a compact next cursor")
	}
	get(request+"&cursor="+*page.NextCursor, &page)
	if page.Items[0].Path != longDirectory+"/z" || page.NextCursor != nil {
		t.Fatal("long directory cursor cannot reach its final file")
	}
	get("/api/v1/snapshots/1/directories?limit=1", &page)
	if page.Items[0].Path != longDirectory || page.NextCursor == nil {
		t.Fatal("long child-directory anchor missing")
	}
	get("/api/v1/snapshots/1/directories?limit=1&cursor="+*page.NextCursor, &page)
	if page.Items[0].Path != "segment" || page.NextCursor != nil {
		t.Fatal("long child-directory cursor failed")
	}
}
