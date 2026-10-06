package database

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestDiskVisibilityOverflowAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.CreateDisk("source", "", "", 0); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"complete", "importing"} {
		if _, err := db.db.Exec(`INSERT INTO snapshot(
			disk_id,state,captured_at,imported_at,capture_provenance,
			file_count,content_count,source_digest)
			VALUES(1,?,?,?,'explicit',0,0,zeroblob(32))`,
			state, FormatTime(time.Unix(10, 0)), FormatTime(time.Unix(20, 0))); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := db.GetDisk(t.Context(), 1)
	if err != nil || detail.LatestSnapshot == nil || detail.LatestSnapshot.Id != 1 ||
		detail.Cataloged == nil || !detail.Cataloged.SizeComplete {
		t.Fatalf("visibility: %+v %v", detail, err)
	}
	page, err := db.ListDisks(t.Context(), 1, 0, 0, nil)
	if err != nil || len(page.Items) != 1 || page.Items[0].LatestSnapshot.Id != 1 ||
		page.Catalog.Revision != 1 {
		t.Fatalf("list visibility: %+v %v", page, err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := db.GetDisk(t.Context(), 1)
	if err != nil || recovered.LatestSnapshot.Id != 1 {
		t.Fatalf("recovery: %+v %v", recovered, err)
	}
	if _, err := db.db.Exec(`INSERT INTO snapshot(
		disk_id,state,captured_at,imported_at,capture_provenance)
		VALUES(1,'importing',?,?,'explicit')`,
		FormatTime(time.Unix(30, 0)), FormatTime(time.Unix(40, 0))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO content(size,hash_type,hash)
		VALUES(9223372036854775807,'sha256',X'AB');
		INSERT INTO observation(snapshot_id,content_id,path) VALUES(2,1,'a'),(2,1,'copy');
		UPDATE snapshot SET state='complete',file_count=2,content_count=1,
		source_digest=zeroblob(32) WHERE id=2;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetDisk(t.Context(), 1); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("size overflow: %v", err)
	}
	page, err = db.ListDisks(t.Context(), 50, 0, 0, nil)
	if err != nil || page.Items[0].LatestSnapshot.Id != 2 {
		t.Fatalf("list must not aggregate sizes: %+v %v", page, err)
	}
	for _, test := range []struct{ after, max base.DiskId }{{1, 99}, {99, 100}} {
		if _, err := db.ListDisks(t.Context(), 1, test.after, test.max, &page.Catalog); !errors.Is(err, ErrValidation) {
			t.Fatalf("anchor %+v: %v", test, err)
		}
	}
}

func TestDiskQueryPlans(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, test := range []struct {
		query string
		args  []any
		index string
	}{
		{diskPageQuery, []any{0, 100, 51}, "SEARCH disk USING INTEGER PRIMARY KEY"},
		{diskPageQuery, []any{50, 100, 51}, "SEARCH disk USING INTEGER PRIMARY KEY"},
		{latestDiskSnapshotQuery, []any{1}, "SEARCH snapshot USING INDEX snapshot_current"},
		{diskSizeQuery, []any{1}, "SEARCH o USING INDEX sqlite_autoindex_observation_1"},
	} {
		rows, err := db.db.Query("EXPLAIN QUERY PLAN "+test.query, test.args...)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		plan := strings.Join(details, "\n")
		if !strings.Contains(plan, test.index) || strings.Contains(plan, "USE TEMP B-TREE") ||
			strings.Contains(plan, "SCAN o") {
			t.Fatalf("unexpected plan for %s:\n%s", test.query, plan)
		}
	}
}
