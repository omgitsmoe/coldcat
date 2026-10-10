package database

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestSearchReplicaQueryPlan(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchReplicaHistory(t, db, 24, 3, 3)
	for _, metric := range []base.ReplicaMetric{base.ReplicaDisks, base.ReplicaLocations} {
		f := base.SearchFilters{Query: "report", Field: "name", Match: "substring",
			ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
				ReplicaMetric: metric, OtherReplicas: new(int64(1))}}
		query, args := searchWindow(f, 50, base.SearchAnchor{})
		plan := searchWindowPlan(t, db, query, args)
		for _, required := range []string{
			"MATERIALIZE search_current_snapshot", "CO-ROUTINE o",
			"observation_content_snapshot (content_id=?)",
		} {
			if !strings.Contains(plan, required) {
				t.Fatalf("%s: missing %q:\n%s", metric, required, plan)
			}
		}
		if strings.Count(plan, "SCAN s USING COVERING INDEX snapshot_current") != 1 {
			t.Fatalf("%s: current snapshots are repeatedly selected:\n%s", metric, plan)
		}
	}
}

func seedSearchReplicaHistory(tb testing.TB, db *DB, current, disks, generations int) {
	tb.Helper()
	err := db.TransactionContext(tb.Context(), func(tx *Tx) error {
		for disk := 1; disk <= disks; disk++ {
			if _, err := tx.Exec(`INSERT INTO disk(id,label,capacity) VALUES(?,'replicas-'||?,0)`,
				disk, disk); err != nil {
				return err
			}
			files := current / disks
			if disk <= current%disks {
				files++
			}
			for generation := 1; generation <= generations; generation++ {
				snapshot := (disk-1)*generations + generation
				captured := fmt.Sprintf("2023-01-%02dT00:00:00.000000000Z", generation)
				if _, err := tx.Exec(`INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(?,?,'importing',?,?,'explicit')`, snapshot, disk, captured, captured); err != nil {
					return err
				}
				content := `CASE WHEN n<=2 THEN 1 ELSE 2+?*?+n+
 CASE WHEN n=4 AND ?<? THEN ?*?*(?+1)
 WHEN n%5=0 THEN ?*?*? ELSE 0 END END`
				contentArgs := []any{disk - 1, current, generation, generations,
					current, disks, generations, current, disks, generation}
				if _, err := tx.Exec(`WITH RECURSIVE ids(n) AS
 (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?), contents(id) AS
 (SELECT `+content+` FROM ids)
 INSERT INTO content(id,hash_type,hash) SELECT id,'sha256',CAST(id AS BLOB)
 FROM contents WHERE true ON CONFLICT(id) DO NOTHING`,
					append([]any{files}, contentArgs...)...); err != nil {
					return err
				}
				args := append([]any{files, snapshot}, contentArgs...)
				args = append(args, disk, generation, generations)
				if _, err := tx.Exec(`WITH RECURSIVE ids(n) AS
 (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO observation(snapshot_id,content_id,path) SELECT ?,`+content+`,
 printf('disk-%02d/',?) || CASE WHEN n<=2 THEN
 'copy-'||n||'/keepsake-report.txt' WHEN n=4 AND ?<? THEN
 'archive/retired-report.txt' ELSE 'archive/report-'||printf('%07d',n)||'.txt' END
 FROM ids`, args...); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE snapshot SET state='complete',
 source_digest=zeroblob(32),file_count=?,content_count=? WHERE id=?`,
					files, files-1, snapshot); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
}

func TestSearchReplicaHistorySemantics(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchReplicaHistory(t, db, 24, 3, 3)
	_, err = db.db.Exec(`INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(10,1,'importing','2024-01-01T00:00:00.000000000Z',
 '2024-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO observation(snapshot_id,content_id,path)
 VALUES(10,1,'unfinished/keepsake-report.txt');`)
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range []base.ReplicaMetric{base.ReplicaDisks, base.ReplicaLocations} {
		bound := int64(2)
		if metric == base.ReplicaLocations {
			bound = 5
		}
		f := base.SearchFilters{Query: "KEEPsake-report.txt", Field: "name", Match: "exact",
			ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
				ReplicaMetric: metric, OtherReplicas: &bound}}
		for _, match := range []string{"exact", "substring"} {
			f.Match = match
			verifySearchWindowPages(t, db, f)
			page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
			if err != nil || len(page.Items) != 6 {
				t.Fatalf("%s/%s: got %d items: %v", metric, match, len(page.Items), err)
			}
			for _, item := range page.Items {
				c := item.Content
				if !item.IsCurrent || c.CurrentDiskCount != 3 || c.CurrentLocationCount != 6 ||
					c.DiskCount != 3 || c.LocationCount != 6 || c.ObservationCount != 18 {
					t.Fatalf("incorrect current summary: %+v", item)
				}
			}
			f.OtherReplicas, f.MinOtherReplicas, f.MaxOtherReplicas = nil, &bound, &bound
			verifySearchWindowPages(t, db, f)
			f.DiskID, f.Directory = 1, "disk-01/copy-1"
			page, err = db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
			if err != nil || len(page.Items) != 1 || page.Items[0].Content.CurrentDiskCount != 3 ||
				page.Items[0].Content.CurrentLocationCount != 6 {
				t.Fatalf("membership changed replica counts: %+v %v", page, err)
			}
			f.DiskID, f.Directory, f.SnapshotID = 0, "", 3
			verifySearchWindowPages(t, db, f)
			f.SnapshotID = 1
			page, err = db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
			if err != nil || len(page.Items) != 0 {
				t.Fatalf("historical snapshot selected current matches: %+v %v", page, err)
			}
			f.SnapshotID = 0
			f.OtherReplicas, f.MinOtherReplicas, f.MaxOtherReplicas = &bound, nil, nil
		}
	}
	for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
		f := base.SearchFilters{Query: "retired-report.txt", Field: "name", Match: "exact",
			ContentFilters: base.ContentFilters{Scope: scope, ReplicaMetric: base.ReplicaDisks}}
		page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
		want := 0
		if scope == base.ScopeHistory {
			want = 6
		}
		if err != nil || len(page.Items) != want {
			t.Fatalf("retired %s: got %d: %v", scope, len(page.Items), err)
		}
		for _, item := range page.Items {
			c := item.Content
			if item.IsCurrent || c.CurrentDiskCount != 0 || c.CurrentLocationCount != 0 ||
				c.DiskCount != 1 || c.LocationCount != 1 || c.ObservationCount != 2 {
				t.Fatalf("incorrect retired summary: %+v", item)
			}
		}
	}
}

func TestSearchImpossibleDiskBoundsPreserveValidation(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchReplicaHistory(t, db, 24, 3, 3)
	f := base.SearchFilters{Query: "report", Field: "name", Match: "substring",
		ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
			ReplicaMetric: base.ReplicaDisks, OtherReplicas: new(int64(3))}}
	for _, minimum := range []bool{false, true} {
		if minimum {
			f.MinOtherReplicas, f.OtherReplicas = f.OtherReplicas, nil
		}
		page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
		if err != nil || len(page.Items) != 0 {
			t.Fatalf("impossible bound: %+v %v", page, err)
		}
		for _, after := range []base.SearchAnchor{
			{ID: 1, Relevance: "substring"},
			{ID: 1, Relevance: "substring", Path: "disk-01/copy-1/keepsake-report.txt"},
		} {
			if _, err := db.Search(t.Context(), f, 50, after, nil); !errors.Is(err, ErrValidation) {
				t.Fatalf("impossible bound accepted cursor: %v", err)
			}
		}
		f.DiskID = 99
		if _, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("impossible bound hid missing disk: %v", err)
		}
		f.DiskID, f.SnapshotID = 0, 99
		if _, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("impossible bound hid missing snapshot: %v", err)
		}
		f.DiskID, f.SnapshotID = 1, 6
		if _, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil); !errors.Is(err, ErrValidation) {
			t.Fatalf("impossible bound hid snapshot ownership mismatch: %v", err)
		}
		f.DiskID, f.SnapshotID = 0, 0
		if _, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, &base.CatalogState{}); !errors.Is(err, ErrStaleCursor) {
			t.Fatalf("impossible bound hid stale revision: %v", err)
		}
	}
	f.MinOtherReplicas, f.MaxOtherReplicas = nil, new(int64(0))
	page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
	if err != nil || len(page.Items) != 18 {
		t.Fatalf("maximum bound incorrectly short-circuited: %+v %v", page, err)
	}
	for _, metric := range []base.ReplicaMetric{base.ReplicaDisks, base.ReplicaLocations} {
		f.ReplicaMetric = metric
		f.MinOtherReplicas, f.MaxOtherReplicas = new(int64(0)), new(int64(1))
		page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
		if err != nil || len(page.Items) != 18 {
			t.Fatalf("non-equal bounds changed membership: %+v %v", page, err)
		}
		f.MinOtherReplicas, f.MaxOtherReplicas = new(int64(1)), new(int64(99))
		page, err = db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
		if err != nil || len(page.Items) != 6 {
			t.Fatalf("inclusive bounds changed membership: %+v %v", page, err)
		}
	}
}

func BenchmarkSearchReplicaHistory(b *testing.B) {
	for _, disks := range []int{3, 12} {
		for _, generations := range []int{1, 5} {
			b.Run(fmt.Sprintf("50000/disks%d/snapshots%d", disks, generations), func(b *testing.B) {
				db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
				if err != nil {
					b.Fatal(err)
				}
				defer db.Close()
				seedSearchReplicaHistory(b, db, 50000, disks, generations)
				for _, metric := range []base.ReplicaMetric{base.ReplicaDisks, base.ReplicaLocations} {
					for _, test := range []struct {
						name  string
						bound int64
						want  int
					}{
						{"possible_no_match", 1, 0},
						{"high_no_match", int64(2 * disks), 0},
						{"matching", int64(disks - 1), 2 * disks},
					} {
						if metric == base.ReplicaLocations && test.name == "matching" {
							test.bound = int64(2*disks - 1)
						}
						b.Run(string(metric)+"/"+test.name, func(b *testing.B) {
							f := base.SearchFilters{Query: "report", Field: "name", Match: "substring",
								ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
									ReplicaMetric: metric, OtherReplicas: &test.bound}}
							b.ReportAllocs()
							for b.Loop() {
								page, err := db.Search(b.Context(), f, 50, base.SearchAnchor{}, nil)
								if err != nil || len(page.Items) != test.want {
									b.Fatalf("got %d items want %d: %v", len(page.Items), test.want, err)
								}
							}
						})
					}
				}
			})
		}
	}
}
