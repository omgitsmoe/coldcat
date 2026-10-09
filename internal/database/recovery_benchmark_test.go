package database

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func seedFullIndexRecovery(tb testing.TB, path string, count int, built bool) {
	tb.Helper()
	db, err := OpenContext(tb.Context(), path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })

	_, err = db.db.ExecContext(tb.Context(), `INSERT INTO disk(id,label,capacity)
 VALUES(1,'source',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit','cshd');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'kept/shared.txt');`)
	if err != nil {
		tb.Fatal(err)
	}
	if err := db.BuildDirectories(tb.Context(), 1); err != nil {
		tb.Fatal(err)
	}
	if _, err := db.PublishImport(tb.Context(), PublishImportRequest{
		SnapshotID: 1, SourceDigest: [32]byte{1},
	}); err != nil {
		tb.Fatal(err)
	}

	_, err = db.db.ExecContext(tb.Context(), `INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(2,1,'importing','2024-01-01T00:00:00.000000000Z',
 '2024-01-01T00:00:00.000000000Z','explicit','cshd');
 INSERT INTO pending_size(snapshot_id,content_id,size) VALUES(2,1,7);`)
	if err != nil {
		tb.Fatal(err)
	}
	for first := 1; first <= count; first += 5000 {
		last := min(first+4999, count)
		err := db.TransactionContext(tb.Context(), func(tx *Tx) error {
			_, err := tx.ExecContext(tb.Context(), `WITH RECURSIVE ids(n) AS
 (VALUES(?) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO content(id,hash_type,hash)
 SELECT n+1,'sha256',CAST(printf('%032d',n) AS BLOB) FROM ids
 WHERE n!=1 AND n%10!=0;`, first, last)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(tb.Context(), `WITH RECURSIVE ids(n) AS
 (VALUES(?) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO import_content(snapshot_id,content_id)
 SELECT 2,n+1 FROM ids WHERE n!=1 AND n%10!=0;`, first, last)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(tb.Context(), `WITH RECURSIVE ids(n) AS
 (VALUES(?) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT 2,CASE WHEN n=1 OR n%10=0 THEN 1 ELSE n+1 END,
 CASE WHEN n=1 THEN 'kept/shared.txt'
 ELSE 'abandoned/'||printf('%03d/file-%07d.txt',n%100,n) END FROM ids;`, first, last)
			return err
		})
		if err != nil {
			tb.Fatal(err)
		}
	}
	if built {
		if err := db.BuildDirectories(tb.Context(), 2); err != nil {
			tb.Fatal(err)
		}
	}
	var observations, paths, buildMarkers int
	err = db.db.QueryRowContext(tb.Context(), `SELECT
 (SELECT COUNT(*) FROM observation WHERE snapshot_id=2),
 (SELECT COUNT(*) FROM search_path),
 (SELECT COUNT(*) FROM directory_build WHERE snapshot_id=2)`).
		Scan(&observations, &paths, &buildMarkers)
	wantMarkers := 0
	if built {
		wantMarkers = 1
	}
	if err != nil || observations != count || paths != count || buildMarkers != wantMarkers {
		tb.Fatalf("fixture: observations=%d paths=%d build markers=%d: %v",
			observations, paths, buildMarkers, err)
	}
	if err := db.Close(); err != nil {
		tb.Fatal(err)
	}
}

func assertFullIndexRecovery(tb testing.TB, db *DB) {
	tb.Helper()
	digest := [32]byte{1}
	for _, check := range []struct {
		query string
		want  int
		args  []any
	}{
		{`SELECT COUNT(*) FROM snapshot WHERE id=1 AND state='complete'
 AND file_count=1 AND content_count=1 AND source_digest=?`, 1, []any{digest[:]}},
		{`SELECT COUNT(*) FROM snapshot WHERE id!=1`, 0, nil},
		{`SELECT COUNT(*) FROM observation`, 1, nil},
		{`SELECT COUNT(*) FROM observation
 WHERE snapshot_id=1 AND content_id=1 AND path='kept/shared.txt'`, 1, nil},
		{`SELECT COUNT(*) FROM content`, 1, nil},
		{`SELECT COUNT(*) FROM content WHERE id=1 AND size IS NULL`, 1, nil},
		{`SELECT COUNT(*) FROM pending_size`, 0, nil},
		{`SELECT COUNT(*) FROM import_content`, 0, nil},
		{`SELECT COUNT(*) FROM directory`, 2, nil},
		{`SELECT COUNT(*) FROM directory WHERE snapshot_id!=1`, 0, nil},
		{`SELECT COUNT(*) FROM directory_content`, 2, nil},
		{`SELECT COUNT(*) FROM directory_content WHERE snapshot_id!=1`, 0, nil},
		{`SELECT COUNT(*) FROM directory_file`, 1, nil},
		{`SELECT COUNT(*) FROM directory_file WHERE snapshot_id!=1`, 0, nil},
		{`SELECT COUNT(*) FROM directory_build`, 1, nil},
		{`SELECT COUNT(*) FROM directory_build WHERE snapshot_id!=1`, 0, nil},
		{`SELECT COUNT(*) FROM search_path`, 1, nil},
		{`SELECT COUNT(*) FROM search_path WHERE path='kept/shared.txt' AND sealed=1`, 1, nil},
	} {
		var got int
		err := db.db.QueryRowContext(tb.Context(), check.query, check.args...).Scan(&got)
		if err != nil || got != check.want {
			tb.Fatalf("%s: got %d, want %d: %v", check.query, got, check.want, err)
		}
	}
	state, err := db.GetCatalogState(tb.Context())
	if err != nil || state.Revision != 1 {
		tb.Fatalf("inventory revision: %+v: %v", state, err)
	}
	for _, path := range []string{"", "kept"} {
		directory, err := db.GetDirectory(tb.Context(), 1, path)
		if err != nil {
			tb.Fatal(err)
		}
		if directory.FileCount != 1 || directory.ContentCount != 1 ||
			directory.UnknownSizeFileCount != 1 || directory.UnknownSizeContentCount != 1 ||
			directory.KnownBytes != 0 || directory.UniqueContentKnownBytes != 0 {
			tb.Fatalf("completed directory changed: %+v", directory)
		}
	}
	for _, check := range []struct {
		query, match string
		want         int
	}{{"shared.txt", "exact", 1}, {"SHARED", "substring", 1}, {"file-", "substring", 0}} {
		filters := base.SearchFilters{
			Query: check.query, Field: "name", Match: check.match,
			ContentFilters: base.ContentFilters{
				Scope: base.ScopeHistory, ReplicaMetric: base.ReplicaDisks,
			},
		}
		page, err := db.Search(tb.Context(), filters, 50, base.SearchAnchor{}, nil)
		if err != nil || len(page.Items) != check.want {
			tb.Fatalf("search %q: %+v: %v", check.query, page, err)
		}
	}
	if err := db.TransactionContext(tb.Context(), func(tx *Tx) error {
		return checkForeignKeys(tb.Context(), tx)
	}); err != nil {
		tb.Fatal(err)
	}
	if _, err := db.db.ExecContext(tb.Context(),
		`INSERT INTO search_trigram(search_trigram,rank) VALUES('integrity-check',1)`); err != nil {
		tb.Fatal(err)
	}
}

func TestFullIndexRecovery(t *testing.T) {
	for _, built := range []bool{false, true} {
		t.Run(fmt.Sprintf("directories_built_%t", built), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.sqlite")
			seedFullIndexRecovery(t, path, 23, built)
			db, err := OpenContext(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			assertFullIndexRecovery(t, db)
		})
	}
}

func BenchmarkFullIndexRecovery(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		for _, built := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/directories_built_%t", count, built), func(b *testing.B) {
				b.ReportAllocs()
				var elapsed time.Duration
				var beforeBytes, afterBytes int64
				for b.Loop() {
					b.StopTimer()
					path := filepath.Join(b.TempDir(), "catalog.sqlite")
					seedFullIndexRecovery(b, path, count, built)
					before, err := os.Stat(path)
					if err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
					started := time.Now()
					db, err := OpenContext(b.Context(), path)
					elapsed += time.Since(started)
					b.StopTimer()
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(func() { db.Close() })
					assertFullIndexRecovery(b, db)
					if err := db.Close(); err != nil {
						b.Fatal(err)
					}
					after, err := os.Stat(path)
					if err != nil {
						b.Fatal(err)
					}
					beforeBytes += before.Size()
					afterBytes += after.Size()
					b.StartTimer()
				}
				b.ReportMetric(float64(count)*float64(b.N)/elapsed.Seconds(), "observations/s")
				b.ReportMetric(float64(beforeBytes)/float64(b.N), "before-bytes")
				b.ReportMetric(float64(afterBytes)/float64(b.N), "after-bytes")
			})
		}
	}
}
