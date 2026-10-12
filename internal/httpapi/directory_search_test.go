package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"io"
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

func TestDirectorySearchHTTPWorkflow(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := app.New(db)
	disk, err := a.CreateDisk(t.Context(), "source", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.CreateDisk(t.Context(), "second", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := a.CreateDisk(t.Context(), "empty", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	importSnapshot := func(diskID base.DiskId, captured int64, paths ...string) string {
		t.Helper()
		input := "# version 1\n"
		for _, path := range paths {
			if strings.HasSuffix(path, "unknown.dat") {
				input += ",,md5," + fixtureMD5AB + " " + path + "\n"
			} else {
				input += "0,5,sha256," + fixtureSHA256AB + " " + path + "\n"
			}
		}
		file := filepath.Join(t.TempDir(), "input.cshd")
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := a.Import(t.Context(), app.ImportRequest{
			DiskID: diskID, Path: file, CapturedAt: time.Unix(captured, 0),
		})
		if err != nil {
			t.Fatal(err)
		}
		return decimal(snapshot.Id)
	}
	old := importSnapshot(disk, 10, "Old/Photos/file.jpg", "docs/report.txt")
	current := importSnapshot(disk, 20, "Photos/Trips/one.jpg", "Photos/Trips/two.jpg",
		"Photos/unknown.dat", "Straße/Ωmega/file.txt", "literal*/file.txt",
		"a"+strings.Repeat("界", 6000)+"/Needle/file.txt", "z/Needle/file.txt",
		"non\ufffe/file.txt", "non\ufffd/file.txt")
	importSnapshot(second, 20, "PHOTOS/another.jpg")
	importSnapshot(empty, 20)
	server := httptest.NewServer(New(a))
	t.Cleanup(server.Close)
	document := openAPIDocument(t)
	operation := document["paths"].(map[string]any)["/api/v1/search/directories"].(map[string]any)["get"].(map[string]any)
	response := operation["responses"].(map[string]any)["200"].(map[string]any)
	schema := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	get := func(query string, status int) directorySearchPageDTO {
		t.Helper()
		resp, err := server.Client().Get(server.URL + "/api/v1/search/directories?" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("%s: %d %s", query, resp.StatusCode, data)
		}
		var page directorySearchPageDTO
		if status == 200 {
			if err := json.Unmarshal(data, &page); err != nil {
				t.Fatal(err)
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			assertResponseSchema(t, document, schema, value)
		}
		return page
	}
	for _, mode := range []string{"syntax", "whole", "substring", "exact"} {
		page := get("q=photos&match="+mode, 200)
		if len(page.Items) != 2 {
			t.Fatalf("%s name matches: %+v", mode, page.Items)
		}
		for _, item := range page.Items {
			if !item.IsCurrent || !strings.EqualFold(item.Basename, "photos") {
				t.Fatalf("unexpected directory: %+v", item)
			}
			if item.Snapshot.ID == current && (item.Directory.FileCount != "3" ||
				item.Directory.KnownBytes != "10" || item.Directory.SizeComplete ||
				item.Directory.UnknownSizeFileCount != "1") {
				t.Fatalf("recursive partial totals: %+v", item.Directory)
			}
		}
	}
	for _, query := range []string{
		"q=photos&field=path", "q=photos&field=path&match=substring",
		"q=photos%20trips&field=path", "q=pho*tri&field=path",
		"q=PHOTOS%2FTRIPS&field=path&match=exact", "q=photos*trips&field=path&match=whole",
		"q=trips%20OR%20absent", "q=%22Trips%22", "q=STRASSE&match=exact",
		"q=%CE%A9MEGA&match=exact", "q=%22literal*%22", "q=*&match=exact",
	} {
		page := get(query, 200)
		want := 1
		if query == "q=photos&field=path" || query == "q=photos&field=path&match=substring" {
			want = 3
		}
		if query == "q=STRASSE&match=exact" || query == "q=*&match=exact" {
			want = 0
		}
		if len(page.Items) != want {
			t.Fatalf("%s: got %d want %d", query, len(page.Items), want)
		}
	}
	if page := get("q=report", 200); len(page.Items) != 0 {
		t.Fatalf("filename must not match a directory: %+v", page)
	}
	if page := get("q=photos&snapshot_id="+old, 200); len(page.Items) != 0 {
		t.Fatal("older snapshot returned current results")
	}
	page := get("q=photos&scope=history&snapshot_id="+old, 200)
	if len(page.Items) != 1 || page.Items[0].IsCurrent {
		t.Fatalf("historical snapshot: %+v", page)
	}
	page = get("q=photos&disk_id="+decimal(disk), 200)
	if len(page.Items) != 1 || page.Items[0].Snapshot.ID != current {
		t.Fatalf("disk filter: %+v", page)
	}
	page = get("q=photos&scope=history", 200)
	if len(page.Items) != 3 {
		t.Fatalf("history repetitions: %+v", page)
	}
	for _, query := range []string{
		"q=ab", "q=ab&match=substring", "q=**", "q=photos&match=wildcard",
		"q=photos&other_replicas=0", "q=photos&directory=Photos", "q=photos&replica_metric=disks",
		"q=photos&q=trips", "q=", "q=photos&scope=", "q=photos&limit=0",
		"q=photos&snapshot_id=" + current + "&disk_id=" + decimal(second),
		"q=photos&cursor=bad", "q=photos&field=bad", "q=" + strings.Repeat("a", 1025),
		"q=%00", "q=%FF", "q=photos%20OR",
	} {
		get(query, 400)
	}
	get("q=photos&disk_id=999", 404)
	get("q=photos&snapshot_id=999", 404)
	longPage := get("q=needle&limit=1", 200)
	if len(longPage.Items) != 1 || len(longPage.Items[0].Directory.Path) < 16384 ||
		longPage.NextCursor == nil || len(*longPage.NextCursor) > 1024 {
		t.Fatalf("long path cursor: %+v", longPage)
	}
	longPage = get("q=needle&limit=1&cursor="+url.QueryEscape(*longPage.NextCursor), 200)
	if len(longPage.Items) != 1 || longPage.Items[0].Directory.Path != "z/Needle" ||
		longPage.NextCursor != nil {
		t.Fatalf("long path continuation: %+v", longPage)
	}
	for _, name := range []string{"non\ufffe", "non\ufffd"} {
		for _, match := range []string{"syntax", "whole", "substring", "exact"} {
			page := get("q="+url.QueryEscape(name)+"&match="+match, 200)
			if len(page.Items) != 1 || page.Items[0].Basename != name {
				t.Fatalf("Unicode noncharacter %q %s: %+v", name, match, page)
			}
		}
	}
	page = get("q=*&disk_id="+decimal(empty), 200)
	if len(page.Items) != 1 || page.Items[0].Directory.Path != "" ||
		page.Items[0].Directory.FileCount != "0" {
		t.Fatalf("empty inventory root: %+v", page)
	}
	all := get("q=*&scope=history", 200)
	var ids []string
	query := "q=*&scope=history&limit=1"
	page = get(query, 200)
	firstCursor := *page.NextCursor
	for {
		if len(page.Items) != 1 {
			t.Fatalf("pagination: %+v", page)
		}
		ids = append(ids, page.Items[0].ID)
		if page.NextCursor == nil {
			break
		}
		if len(ids) > len(all.Items) {
			t.Fatal("pagination did not terminate")
		}
		page = get(query+"&cursor="+url.QueryEscape(*page.NextCursor), 200)
	}
	if len(ids) != len(all.Items) {
		t.Fatalf("paged %d of %d", len(ids), len(all.Items))
	}
	for i, item := range all.Items {
		if ids[i] != item.ID {
			t.Fatalf("page order at %d: %s != %s", i, ids[i], item.ID)
		}
	}
	get("q=*&limit=1&cursor="+url.QueryEscape(firstCursor), 400)
	get("q=*&scope=history&limit=2&cursor="+url.QueryEscape(firstCursor), 400)
	data, err := base64.RawURLEncoding.DecodeString(firstCursor)
	if err != nil {
		t.Fatal(err)
	}
	var altered map[string]any
	if err := json.Unmarshal(data, &altered); err != nil {
		t.Fatal(err)
	}
	altered["after_id"] = 999999
	data, err = json.Marshal(altered)
	if err != nil {
		t.Fatal(err)
	}
	get(query+"&cursor="+base64.RawURLEncoding.EncodeToString(data), 400)
	importSnapshot(second, 30, "New/file.txt")
	get(query+"&cursor="+url.QueryEscape(firstCursor), 409)
}
