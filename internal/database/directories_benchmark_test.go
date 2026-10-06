package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func BenchmarkDirectoryQueries(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		for _, shape := range []string{"wide", "deep", "duplicate"} {
			b.Run(fmt.Sprintf("%d/%s", count, shape), func(b *testing.B) {
				path := filepath.Join(b.TempDir(), "catalog.sqlite")
				db, err := Open(path)
				if err != nil {
					b.Fatal(err)
				}
				defer db.Close()
				if _, err := db.db.Exec(`DROP TRIGGER observation_search_insert;
 DROP TRIGGER observation_search_delete; DROP TRIGGER observation_search_update;`); err != nil {
					b.Fatal(err)
				}
				contents := 10000
				if shape == "duplicate" {
					contents = 100
				}
				prefix := "tree/"
				if shape == "deep" {
					prefix += strings.Repeat("nested/", 8)
				}
				err = db.TransactionContext(b.Context(), func(tx *Tx) error {
					_, err := tx.ExecContext(b.Context(), `INSERT INTO disk(id,label,capacity)
 VALUES(1,'source',0); INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit','cshd');
 WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO content(id,size,hash_type,hash)
 SELECT n,4,'sha256',CAST(printf('%032d',n) AS BLOB) FROM ids;`, contents)
					if err != nil {
						return err
					}
					_, err = tx.ExecContext(b.Context(), `WITH RECURSIVE ids(n) AS
 (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT 1,1+((n-1)%?),?||printf('%03d/file-%07d',n%100,n) FROM ids`,
						count, contents, prefix)
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
				before, err := os.Stat(path)
				if err != nil {
					b.Fatal(err)
				}
				started := time.Now()
				if err := db.BuildDirectories(b.Context(), 1); err != nil {
					b.Fatal(err)
				}
				build := time.Since(started)
				if _, err := db.db.Exec(`UPDATE snapshot SET state='complete',
 source_digest=zeroblob(32),file_count=?,content_count=? WHERE id=1`, count, contents); err != nil {
					b.Fatal(err)
				}
				stat, err := os.Stat(path)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(build.Milliseconds()), "build-ms")
				b.ReportMetric(float64(stat.Size()), "catalog-bytes")
				b.Logf("directory-only build %s; catalog %d bytes; added %d bytes",
					build, stat.Size(), stat.Size()-before.Size())
				filters := base.DirectoryFilters{SnapshotID: 1, ReplicaMetric: base.ReplicaDisks}
				for _, name := range []string{"detail_root", "detail_nested", "children_first",
					"children_deep", "entries_first", "recursive_first", "recursive_deep", "replica_nested"} {
					b.Run(name, func(b *testing.B) {
						f := filters
						var after base.DirectoryAnchor
						var expected *base.CatalogState
						if name == "detail_nested" || name == "entries_first" || name == "replica_nested" {
							f.Path = strings.TrimSuffix(prefix, "/") + "/050"
							if name == "replica_nested" {
								zero := int64(0)
								f.OtherReplicas = &zero
							}
						}
						if strings.HasPrefix(name, "children") {
							f.Path = strings.TrimSuffix(prefix, "/")
							f.DirectoriesOnly = true
						}
						if strings.HasPrefix(name, "recursive") {
							f.Recursive = true
						}
						if strings.HasSuffix(name, "deep") {
							afterPath := prefix + "050"
							after.Kind = "directory"
							table := "directory"
							if f.Recursive {
								afterPath += fmt.Sprintf("/file-%07d", count/2+50)
								after.Kind = "file"
								table = "observation"
							}
							if err := db.db.QueryRow("SELECT id FROM "+table+
								" WHERE snapshot_id=1 AND path=?", afterPath).Scan(&after.ID); err != nil {
								b.Fatal(err)
							}
							expected = &base.CatalogState{Revision: 1}
						}
						b.ReportAllocs()
						for b.Loop() {
							if strings.HasPrefix(name, "detail") {
								if _, err := db.GetDirectory(b.Context(), 1, f.Path); err != nil {
									b.Fatal(err)
								}
							} else if _, err := db.ListDirectoryEntries(b.Context(), f, 50,
								after, expected); err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			})
		}
	}
}

func BenchmarkDirectoryRecovery(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				path := filepath.Join(b.TempDir(), "catalog.sqlite")
				db, err := Open(path)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := db.db.Exec(`DROP TRIGGER observation_search_insert;
 DROP TRIGGER observation_search_delete; DROP TRIGGER observation_search_update;`); err != nil {
					b.Fatal(err)
				}
				_, err = db.db.ExecContext(b.Context(), `INSERT INTO disk(id,label,capacity)
 VALUES(1,'source',0); INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit','cshd');
 WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO content(id,hash_type,hash)
 SELECT n,'sha256',CAST(printf('%032d',n) AS BLOB) FROM ids;
 INSERT INTO import_content SELECT 1,id FROM content;
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT 1,id,'tree/'||printf('%03d/file-%07d',id%100,id) FROM content;`, count)
				if err != nil {
					b.Fatal(err)
				}
				if err := db.BuildDirectories(b.Context(), 1); err != nil {
					b.Fatal(err)
				}
				if err := db.Close(); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				db, err = Open(path)
				if err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				var remaining int
				if err := db.db.QueryRow(`SELECT (SELECT COUNT(*) FROM snapshot)+
 (SELECT COUNT(*) FROM directory)+(SELECT COUNT(*) FROM directory_content)+
 (SELECT COUNT(*) FROM directory_file)+(SELECT COUNT(*) FROM directory_build)+
 (SELECT COUNT(*) FROM content)+(SELECT COUNT(*) FROM search_path)`).Scan(&remaining); err != nil ||
					remaining != 0 {
					b.Fatalf("incomplete recovery: %d, %v", remaining, err)
				}
				if err := db.Close(); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}

func BenchmarkDirectoryEnrichment(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
				if err != nil {
					b.Fatal(err)
				}
				_, err = db.db.ExecContext(b.Context(), `DROP TRIGGER observation_search_insert;
 DROP TRIGGER observation_search_delete; DROP TRIGGER observation_search_update;
 INSERT INTO disk(id,label,capacity) VALUES(1,'source',0);
 INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit','cshd');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));
 WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT 1,1,'tree/'||printf('%03d/file-%07d',n%100,n) FROM ids;`, count)
				if err != nil {
					b.Fatal(err)
				}
				if err := db.BuildDirectories(b.Context(), 1); err != nil {
					b.Fatal(err)
				}
				_, err = db.db.ExecContext(b.Context(), `UPDATE snapshot SET state='complete',
 source_digest=zeroblob(32),file_count=?,content_count=1 WHERE id=1;
 INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(2,1,'importing','2024-01-01T00:00:00.000000000Z',
 '2024-01-01T00:00:00.000000000Z','explicit','cshd');
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(2,1,'new/known');
 INSERT INTO search_path(path,name) VALUES('new/known','known');
 INSERT INTO pending_size(snapshot_id,content_id,size) VALUES(2,1,7);`, count)
				if err != nil {
					b.Fatal(err)
				}
				if err := db.BuildDirectories(b.Context(), 2); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				_, err = db.PublishImport(b.Context(), PublishImportRequest{
					SnapshotID: 2, SourceDigest: [32]byte{2},
				})
				if err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				detail, err := db.GetDirectory(b.Context(), 1, "")
				if err != nil || detail.KnownBytes != int64(count)*7 ||
					detail.UnknownSizeFileCount != 0 || detail.UniqueContentKnownBytes != 7 {
					b.Fatalf("incorrect enrichment: %+v, %v", detail, err)
				}
				if err := db.Close(); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}
