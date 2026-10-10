package database

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime/pprof"
	"testing"
)

func BenchmarkObservationCleanupRollback(b *testing.B) {
	for _, files := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(files), func(b *testing.B) {
			db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(1,1,'importing','2023-01-01T00:00:00Z','2023-01-01T00:00:00Z','explicit','cshd'),
 (2,1,'importing','2023-01-02T00:00:00Z','2023-01-02T00:00:00Z','explicit','cshd');
 WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<500)
 INSERT INTO content(id,hash_type,hash) SELECT i,'sha256',CAST(printf('%064d',i) AS BLOB) FROM n;`)
			if err != nil {
				b.Fatal(err)
			}
			for _, snapshot := range []int{1, 2} {
				for start := 1; start <= files; start += 5000 {
					_, err = db.db.Exec(`WITH RECURSIVE n(i) AS (
 VALUES(?) UNION ALL SELECT i+1 FROM n WHERE i<?)
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT ?,1+i%500,printf('generation-%d/report-%07d.txt',?,i) FROM n`,
						start, min(start+4999, files), snapshot, snapshot, snapshot)
					if err != nil {
						b.Fatal(err)
					}
				}
			}
			if err := db.BuildDirectories(b.Context(), 1); err != nil {
				b.Fatal(err)
			}
			if _, err := db.PublishImport(b.Context(), PublishImportRequest{
				SnapshotID: 1, SourceDigest: [32]byte{1},
			}); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for b.Loop() {
				pprof.Do(b.Context(), pprof.Labels("phase", "cleanup"), func(ctx context.Context) {
					tx, err := db.db.BeginTx(ctx, nil)
					if err != nil {
						b.Fatal(err)
					}
					defer tx.Rollback()
					if _, err := tx.ExecContext(ctx,
						"DELETE FROM observation WHERE snapshot_id=2"); err != nil {
						b.Fatal(err)
					}
					if err := tx.Rollback(); err != nil {
						b.Fatal(err)
					}
				})
			}
			b.StopTimer()
			for _, table := range []string{"observation", "search_path"} {
				var count int
				if err := db.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
					b.Fatal(err)
				}
				if count != 2*files {
					b.Fatalf("rollback changed %s: %d", table, count)
				}
			}
			if _, err := db.db.Exec(
				"INSERT INTO search_trigram(search_trigram,rank) VALUES('integrity-check',1)"); err != nil {
				b.Fatal(err)
			}
		})
	}
}
