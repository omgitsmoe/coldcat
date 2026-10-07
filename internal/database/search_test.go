package database

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestSearchIndexesCleanupAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'foo/report');
 UPDATE snapshot SET state='complete',source_digest=zeroblob(32),file_count=1,content_count=1;
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(2,1,'importing','2024-01-01T00:00:00.000000000Z',
 '2024-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO observation(snapshot_id,content_id,path)
 VALUES(2,1,'foo/report'),(2,1,'unfinished/REPORT');`)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path   string
		sealed int
	}{{"foo/report", 1}, {"unfinished/REPORT", 0}} {
		var sealed int
		if err := db.db.QueryRow(`SELECT sealed FROM search_path WHERE path=?`, test.path).
			Scan(&sealed); err != nil || sealed != test.sealed {
			t.Fatalf("seal %q: got %d want %d: %v", test.path, sealed, test.sealed, err)
		}
	}
	f := base.SearchFilters{Query: "report", Field: "name", Match: "substring",
		ContentFilters: base.ContentFilters{Scope: base.ScopeHistory, ReplicaMetric: base.ReplicaDisks}}
	page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("incomplete visibility: %+v %v", page, err)
	}
	for _, statement := range []string{
		`DELETE FROM search_path WHERE path='foo/report'`,
		`UPDATE search_path SET name='changed' WHERE path='foo/report'`,
		`UPDATE search_path SET sealed=0 WHERE path='foo/report'`,
		`UPDATE search_path SET sealed=1 WHERE path='unfinished/REPORT'`,
	} {
		if _, err := db.db.Exec(statement); err == nil {
			t.Fatalf("accepted mutation: %s", statement)
		}
	}
	for _, test := range []struct {
		field, match, query, index string
	}{
		{"name", "exact", "report", "search_name"},
		{"path", "exact", "foo/report", "search_path_fold"},
		{"name", "substring", "report", "VIRTUAL TABLE INDEX"},
		{"path", "substring", "foo/report", "VIRTUAL TABLE INDEX"},
	} {
		f.Field, f.Match, f.Query = test.field, test.match, test.query
		for _, after := range []int{0, 10000} {
			query, args := searchWindow(f, 50, base.SearchAnchor{
				Relevance: "exact", ID: base.FileObservationId(after),
			})
			rows, err := db.db.Query("EXPLAIN QUERY PLAN "+query+
				`SELECT * FROM eligible WHERE (rank,path,id)>(0,'',?) ORDER BY rank,path,id LIMIT 51`,
				append(args, after)...)
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
			for _, index := range []string{test.index, "observation_path_snapshot", "snapshot_current"} {
				if !strings.Contains(text, index) {
					t.Fatalf("missing %s:\n%s", index, text)
				}
			}
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM search_path`,
		`SELECT COUNT(*) FROM search_trigram WHERE search_trigram MATCH 'name_fold:"report"'`,
	} {
		var count int
		if err := db.db.QueryRow(query).Scan(&count); err != nil || count != 1 {
			t.Fatalf("recovery: %s: %d %v", query, count, err)
		}
	}
	if _, err := db.db.Exec(`INSERT INTO search_trigram(search_trigram) VALUES('integrity-check')`); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationRequiresSearchPaths(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit','cshd');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'Report');`)
	if err != nil {
		t.Fatal(err)
	}
	mutate := `DELETE FROM search_path`
	if _, err := db.db.Exec(mutate); err != nil {
		t.Fatal(err)
	}
	_, err = db.PublishImport(t.Context(), PublishImportRequest{SnapshotID: 1})
	if err == nil || !strings.Contains(err.Error(), "search index is incomplete") {
		t.Fatalf("published missing search path: %v", err)
	}
	var state string
	if err := db.db.QueryRow(`SELECT state FROM snapshot WHERE id=1`).Scan(&state); err != nil ||
		state != "importing" {
		t.Fatalf("publication marker: %s %v", state, err)
	}
}

func BenchmarkSearchQueries(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "catalog.sqlite")
			started := time.Now()
			db, err := Open(path)
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			err = db.Transaction(func(tx *Tx) error {
				_, err := tx.Exec(`WITH RECURSIVE ids(n) AS
 (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<20)
 INSERT INTO disk(id,label,capacity) SELECT n,'disk-'||n,0 FROM ids;
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 SELECT id,id,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit' FROM disk;
 WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<50000)
 INSERT INTO content(id,hash_type,hash) SELECT n,'sha256',CAST(n AS BLOB) FROM ids;`)
				if err != nil {
					return err
				}
				_, err = tx.Exec(`WITH RECURSIVE ids(n) AS
 (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO observation(id,snapshot_id,content_id,path)
 SELECT n,1+((n-1)/50000),1+((n-1)%50000),
 CASE WHEN n%3=0 THEN 'photos/é猫/' ELSE 'archive/' END ||
 CASE WHEN n%5=0 THEN 'report-' ELSE 'item-' END ||
 printf('%05d',1+((n-1)%50000))||'.txt' FROM ids`, count)
				if err != nil {
					return err
				}
				_, err = tx.Exec(`UPDATE snapshot SET state='complete',source_digest=zeroblob(32),
 file_count=(SELECT COUNT(*) FROM observation WHERE snapshot_id=snapshot.id),
 content_count=(SELECT COUNT(DISTINCT content_id) FROM observation WHERE snapshot_id=snapshot.id)`)
				return err
			})
			if err != nil {
				b.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			b.Logf("fixture: %d observations, %d bytes, built in %s",
				count, info.Size(), time.Since(started))
			highBound := int64(99)
			for _, test := range []struct {
				name string
				f    base.SearchFilters
			}{
				{"exact_name", base.SearchFilters{Query: "report-00005.txt", Match: "exact"}},
				{"exact_path", base.SearchFilters{Query: "archive/report-00005.txt",
					Field: "path", Match: "exact"}},
				{"rare", base.SearchFilters{Query: "00005"}},
				{"common", base.SearchFilters{Query: "report"}},
				{"unicode_three", base.SearchFilters{Query: "é猫/", Field: "path"}},
				{"no_match", base.SearchFilters{Query: "missing-path"}},
				{"history", base.SearchFilters{Query: "00005",
					ContentFilters: base.ContentFilters{Scope: base.ScopeHistory}}},
				{"directory", base.SearchFilters{Query: "report", ContentFilters: base.ContentFilters{
					DiskID: 1, Directory: "archive"}}},
				{"disk_bounds", base.SearchFilters{Query: "00005", ContentFilters: base.ContentFilters{
					MinOtherReplicas: new(int64)}}},
				{"location_bounds", base.SearchFilters{Query: "00005", ContentFilters: base.ContentFilters{
					ReplicaMetric: base.ReplicaLocations, MinOtherReplicas: new(int64)}}},
				{"common_disk_bounds", base.SearchFilters{Query: "report", ContentFilters: base.ContentFilters{
					MinOtherReplicas: new(int64)}}},
				{"common_location_bounds", base.SearchFilters{Query: "report",
					ContentFilters: base.ContentFilters{ReplicaMetric: base.ReplicaLocations,
						MinOtherReplicas: new(int64)}}},
				{"no_match_disk_bounds", base.SearchFilters{Query: "report", ContentFilters: base.ContentFilters{
					OtherReplicas: &highBound}}},
				{"deep_common", base.SearchFilters{Query: "report"}},
				{"connection_cold_rare", base.SearchFilters{Query: "00005"}},
			} {
				f := test.f
				if f.Field == "" {
					f.Field = "name"
				}
				if f.Match == "" {
					f.Match = "substring"
				}
				if f.Scope == "" {
					f.Scope = base.ScopeCurrent
				}
				if f.ReplicaMetric == "" {
					f.ReplicaMetric = base.ReplicaDisks
				}
				b.Run(test.name, func(b *testing.B) {
					var after base.SearchAnchor
					if test.name == "deep_common" {
						after = base.SearchAnchor{Relevance: "prefix",
							Path: "archive/report-25000.txt", ID: 25000}
					}
					if test.name == "connection_cold_rare" {
						db.db.SetMaxIdleConns(0)
						defer db.db.SetMaxIdleConns(2)
					}
					b.ReportAllocs()
					var samples []int64
					for b.Loop() {
						started := time.Now()
						if _, err := db.Search(b.Context(), f, 50, after, nil); err != nil {
							b.Fatal(err)
						}
						samples = append(samples, time.Since(started).Nanoseconds())
					}
					slices.Sort(samples)
					b.ReportMetric(float64(samples[(len(samples)*95-1)/100]), "p95-ns/op")
				})
			}
		})
	}
}
