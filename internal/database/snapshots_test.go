package database

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestSnapshotPageVisibilityAndAnchors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, label := range []string{"source", "other"} {
		if _, err := db.CreateDisk(label, "", "", 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		disk  int
		state string
	}{
		{1, "complete"}, {2, "complete"}, {1, "importing"},
	} {
		_, err := db.db.Exec(`INSERT INTO snapshot(
			disk_id,state,captured_at,imported_at,capture_provenance,
			file_count,content_count,source_digest)
			VALUES(?,?,?,?,'explicit',0,0,zeroblob(32))`,
			item.disk, item.state, FormatTime(time.Unix(10, 123)), FormatTime(time.Unix(20, 0)))
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := db.ListDiskSnapshots(t.Context(), 1, 50, 0, time.Time{}, nil)
	if err != nil || len(page.Items) != 1 || page.Items[0].Id != 1 || page.Catalog.Revision != 2 {
		t.Fatalf("visibility: %+v %v", page, err)
	}
	for _, test := range []struct {
		id   base.SnapshotId
		time time.Time
	}{
		{2, time.Unix(10, 123)}, {3, time.Unix(10, 123)},
		{999, time.Unix(10, 123)}, {1, time.Unix(10, 124)},
	} {
		if _, err := db.ListDiskSnapshots(t.Context(), 1, 1, test.id, test.time, nil); !errors.Is(err, ErrValidation) {
			t.Fatalf("invalid anchor %+v: %v", test, err)
		}
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	continued, err := db.ListDiskSnapshots(t.Context(), 1, 1, 1, time.Unix(10, 123), &page.Catalog)
	if err != nil || len(continued.Items) != 0 || continued.Catalog != page.Catalog {
		t.Fatalf("recovery changed cursor: %+v %v", continued, err)
	}
	var abandoned int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM snapshot WHERE state='importing'").
		Scan(&abandoned); err != nil || abandoned != 0 {
		t.Fatalf("recovery: count %d, error %v", abandoned, err)
	}
}

func TestSnapshotPageQueryPlans(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, test := range []struct {
		query string
		args  []any
	}{
		{diskSnapshotsQuery, []any{1, 51}},
		{diskSnapshotsAfterQuery, []any{1, FormatTime(time.Unix(10, 123)), 1, 51}},
	} {
		rows, err := db.db.Query("EXPLAIN QUERY PLAN "+test.query, test.args...)
		if err != nil {
			t.Fatal(err)
		}
		var indexed bool
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(detail, "SEARCH snapshot USING INDEX snapshot_current") {
				indexed = true
			}
			if strings.Contains(detail, "USE TEMP B-TREE") {
				t.Fatalf("unexpected sort: %s", detail)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if !indexed {
			t.Fatalf("missing indexed snapshot lookup: %s", test.query)
		}
	}
}
