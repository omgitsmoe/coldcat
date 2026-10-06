package database

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestContentListIndexesAndIncompleteVisibility(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',X'AB');
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'foo/a');`); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
		for _, metric := range []base.ReplicaMetric{base.ReplicaDisks, base.ReplicaLocations} {
			f := base.ContentFilters{Scope: scope, ReplicaMetric: metric}
			page, err := db.ListContents(t.Context(), f, 50, 0, nil)
			if err != nil || len(page.Items) != 0 {
				t.Fatalf("incomplete: %+v %v", page, err)
			}
			for _, disk := range []base.DiskId{0, 1} {
				f.DiskID = disk
				if disk != 0 {
					f.Directory = "foo"
					if scope == base.ScopeCurrent {
						zero := int64(0)
						f.OtherReplicas = &zero
					}
				}
				for _, after := range []base.ContentId{0, 10000} {
					args := append(contentArguments(f, after), after, 51, scope, scope)
					rows, err := db.db.Query("EXPLAIN QUERY PLAN "+contentListQuery, args...)
					if err != nil {
						t.Fatal(err)
					}
					var plan []string
					for rows.Next() {
						var id, parent, unused int
						var detail string
						if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
							t.Fatal(err)
						}
						plan = append(plan, detail)
					}
					if err := rows.Err(); err != nil {
						t.Fatal(err)
					}
					rows.Close()
					text := strings.Join(plan, "\n")
					for _, index := range []string{"observation_content_snapshot", "snapshot_current",
						"SEARCH c USING INTEGER PRIMARY KEY"} {
						if !strings.Contains(text, index) {
							t.Fatalf("missing %s:\n%s", index, text)
						}
					}
				}
			}
		}
	}
}

func benchmarkContentLists(b *testing.B) {
	for _, observations := range []int{50000, 1000000} {
		b.Run(fmt.Sprintf("list_%d", observations), func(b *testing.B) {
			db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			contents := observations / 4
			err = db.Transaction(func(tx *Tx) error {
				if _, err := tx.Exec(`INSERT INTO disk(id,label,capacity)
 VALUES(1,'first',0),(2,'second',0),(3,'third',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 SELECT id,id,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit' FROM disk;`); err != nil {
					return err
				}
				if _, err := tx.Exec(`WITH RECURSIVE ids(n) AS
 (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO content(id,hash_type,hash) SELECT n,'sha256',CAST(n AS BLOB) FROM ids`,
					contents); err != nil {
					return err
				}
				if _, err := tx.Exec(`WITH RECURSIVE ids(n) AS
 (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO observation(id,snapshot_id,content_id,path)
 SELECT n, CASE WHEN n<=? OR (1+((n-1)%?))%3=0 THEN 1
 WHEN (1+((n-1)%?))%3=1 THEN 2 ELSE 2+((n/?)%2) END,
 CASE WHEN n<=? THEN n ELSE 1+((n-1)%?) END,
 CASE WHEN n%2=0 THEN 'photos/' ELSE 'archive/' END||n FROM ids`,
					observations, contents, contents, contents, contents, contents, contents); err != nil {
					return err
				}
				_, err := tx.Exec(`UPDATE snapshot SET state='complete',source_digest=zeroblob(32),
 file_count=(SELECT COUNT(*) FROM observation WHERE snapshot_id=snapshot.id),
 content_count=(SELECT COUNT(DISTINCT content_id) FROM observation
 WHERE snapshot_id=snapshot.id)`)
				return err
			})
			if err != nil {
				b.Fatal(err)
			}
			min, max := int64(0), int64(0)
			for _, test := range []struct {
				name  string
				f     base.ContentFilters
				after base.ContentId
			}{
				{"first", base.ContentFilters{}, 0},
				{"deep", base.ContentFilters{}, base.ContentId(contents - 100)},
				{"history", base.ContentFilters{Scope: base.ScopeHistory}, 0},
				{"directory", base.ContentFilters{DiskID: 1, Directory: "photos"}, 0},
				{"disk_bounds", base.ContentFilters{MinOtherReplicas: &min}, 0},
				{"location_no_matches", base.ContentFilters{
					ReplicaMetric: base.ReplicaLocations, MaxOtherReplicas: &max}, 0},
			} {
				b.Run(test.name, func(b *testing.B) {
					f := test.f
					if f.Scope == "" {
						f.Scope = base.ScopeCurrent
					}
					if f.ReplicaMetric == "" {
						f.ReplicaMetric = base.ReplicaDisks
					}
					b.ReportAllocs()
					for b.Loop() {
						if _, err := db.ListContents(b.Context(), f, 50, test.after, nil); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
