package httpapi

import (
	"encoding/json"
	"fmt"
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
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestHTTPSnapshotListing(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := app.New(db)
	disk, err := a.CreateDisk(ctx, "source", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.CreateDisk(ctx, "other", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "fixture.cshd")
	if err := os.WriteFile(file, []byte("# version 1\n,0,sha256,ab empty\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var imported []base.Snapshot
	for _, seconds := range []int64{30, 10, 30, 20} {
		s, err := a.Import(ctx, app.ImportRequest{
			DiskID: disk, Path: file, CapturedAt: time.Unix(seconds, 123), AllowRepeat: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		imported = append(imported, s)
	}
	server := httptest.NewServer(New(a))
	t.Cleanup(func() { server.Close() })
	document := openAPIDocument(t)
	request := func(route, method string, status int, dst any) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+route, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var value any
		if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("%s: status %d, want %d; body %#v", route, resp.StatusCode, status, value)
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type: %s", resp.Header.Get("Content-Type"))
		}
		if status == 405 && resp.Header.Get("Allow") != "GET, HEAD" {
			t.Fatalf("unexpected Allow: %s", resp.Header.Get("Allow"))
		}
		schemaName := "Error"
		if status == 200 {
			schemaName = "SnapshotPage"
			if strings.HasPrefix(route, "/api/v1/snapshots/") {
				schemaName = "Snapshot"
			}
		}
		assertResponseSchema(t, document,
			resolveReference(t, document, "#/components/schemas/"+schemaName), value)
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, dst); err != nil {
			t.Fatal(err)
		}
	}
	baseRoute := fmt.Sprintf("/api/v1/disks/%d/snapshots", disk)
	var first snapshotPageDTO
	request(baseRoute+"?limit=1", "GET", 200, &first)
	if first.NextCursor == nil || len(first.Items) != 1 || first.Items[0].ID != "3" ||
		first.Revision != "4" || first.DiskID != decimal(disk) {
		t.Fatalf("first: %+v", first)
	}
	continuation := baseRoute + "?limit=1&cursor=" + url.QueryEscape(*first.NextCursor)
	var got []snapshotDTO
	route := baseRoute + "?limit=1"
	for {
		var page snapshotPageDTO
		request(route, "GET", 200, &page)
		for _, item := range page.Items {
			var detail snapshotDTO
			request("/api/v1/snapshots/"+item.ID, "GET", 200, &detail)
			if !reflect.DeepEqual(item, detail) {
				t.Fatalf("list/detail mismatch: %+v %+v", item, detail)
			}
		}
		got = append(got, page.Items...)
		if page.NextCursor == nil {
			break
		}
		route = baseRoute + "?limit=1&cursor=" + url.QueryEscape(*page.NextCursor)
	}
	want := []snapshotDTO{
		snapshotResponse(imported[2]), snapshotResponse(imported[0]),
		snapshotResponse(imported[3]), snapshotResponse(imported[1]),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pages: %+v, want %+v", got, want)
	}
	var all snapshotPageDTO
	request(baseRoute, "GET", 200, &all)
	if !reflect.DeepEqual(all.Items, want) || all.NextCursor != nil {
		t.Fatalf("default page: %+v", all)
	}
	request(baseRoute+"?limit=200", "GET", 200, &all)
	var empty snapshotPageDTO
	request(fmt.Sprintf("/api/v1/disks/%d/snapshots", other), "GET", 200, &empty)
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatalf("empty: %+v", empty)
	}
	for _, suffix := range []string{
		"?limit=0", "?limit=-1", "?limit=201", "?limit=x", "?limit=",
		"?limit=1&limit=1", "?scope=history", "?cursor=", "?cursor=!", "?x=1",
		"?cursor=a&cursor=b", "?limit=%ZZ",
	} {
		var failure errorDTO
		request(baseRoute+suffix, "GET", 400, &failure)
		if failure.Error.Code != "invalid_request" {
			t.Fatalf("error: %+v", failure)
		}
	}
	for _, id := range []string{"0", "-1", "abc", "9223372036854775808"} {
		var failure errorDTO
		request("/api/v1/disks/"+id+"/snapshots", "GET", 400, &failure)
	}
	for _, mismatch := range []string{
		baseRoute + "?limit=2&cursor=" + url.QueryEscape(*first.NextCursor),
		fmt.Sprintf("/api/v1/disks/%d/snapshots?limit=1&cursor=%s",
			other, url.QueryEscape(*first.NextCursor)),
	} {
		var failure errorDTO
		request(mismatch, "GET", 400, &failure)
	}
	var failure errorDTO
	request("/api/v1/disks/999/snapshots", "GET", 404, &failure)
	if failure.Error.Code != "not_found" {
		t.Fatalf("missing disk: %+v", failure)
	}
	request(baseRoute, "POST", 405, &failure)
	if failure.Error.Code != "method_not_allowed" {
		t.Fatalf("method: %+v", failure)
	}

	server.Close()
	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = app.New(db)
	server = httptest.NewServer(New(a))
	var next snapshotPageDTO
	request(continuation, "GET", 200, &next)
	if len(next.Items) != 1 || next.Items[0].ID != "1" {
		t.Fatalf("restart: %+v", next)
	}
	server.Close()
	if err := os.WriteFile(file, []byte(",sha256,ab temporary\ninvalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(ctx, app.ImportRequest{
		DiskID: disk, Path: file, CapturedAt: time.Unix(40, 0),
	}); err == nil {
		t.Fatal("accepted failed import")
	}
	server = httptest.NewServer(New(a))
	request(continuation, "GET", 200, &next)
	server.Close()
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(ctx, app.ImportRequest{
		DiskID: other, Path: file, CapturedAt: time.Unix(5, 0),
	}); err != nil {
		t.Fatal(err)
	}
	server = httptest.NewServer(New(a))
	request(continuation, "GET", 409, &failure)
	if failure.Error.Code != "stale_cursor" {
		t.Fatalf("stale: %+v", failure)
	}
}
