package database

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkDirectoryEnrichmentDistinct(b *testing.B) {
	for _, count := range []int{20000, 100000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				db := enrichmentDistinctFixture(b, count, false)
				b.StartTimer()
				err := db.TransactionContext(b.Context(), func(tx *Tx) error {
					return enrichDirectories(b.Context(), tx, 3)
				})
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				checkDistinctEnrichment(b, db, count)
				if err := db.Close(); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}

func enrichmentDistinctFixture(t testing.TB, count int, legacy bool) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if legacy {
		_, err := db.db.Exec(`DROP TRIGGER directory_content_insert;
 DROP VIEW directory_content;
 DROP INDEX directory_content_reverse;` + directoryContentSchema)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = db.Transaction(func(tx *Tx) error {
		_, err := tx.Exec(`INSERT INTO disk(id,label,capacity)
 VALUES(1,'first',0),(2,'second',0),(3,'incoming',0);
 INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 SELECT id,id,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit','cshd' FROM disk;
 WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?1)
 INSERT INTO content(id,hash_type,hash) SELECT n,'sha256',CAST(printf('%064d',n) AS BLOB)
 FROM ids;
 INSERT INTO directory(snapshot_id,path,parent_path,file_count,content_count,
 unknown_size_file_count,unknown_size_content_count)
 SELECT id,'',NULL,?1,?1,?1,?1 FROM snapshot WHERE id IN(1,2);
 WITH RECURSIVE dirs(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM dirs WHERE n<?2)
 INSERT INTO directory(snapshot_id,path,parent_path,file_count,content_count,
 unknown_size_file_count,unknown_size_content_count)
 SELECT s.id,printf('leaf-%04d',n),'',50,50,50,50 FROM dirs
 CROSS JOIN snapshot s WHERE s.id IN(1,2);
 INSERT INTO directory_content(snapshot_id,directory_path,content_id,occurrences)
 SELECT s.id,'',c.id,1 FROM content c CROSS JOIN snapshot s WHERE s.id IN(1,2);
 INSERT INTO directory_content(snapshot_id,directory_path,content_id,occurrences)
 SELECT s.id,printf('leaf-%04d',(c.id-1)/50+1),c.id,1
 FROM content c CROSS JOIN snapshot s WHERE s.id IN(1,2);
 UPDATE snapshot SET state='complete',source_digest=zeroblob(32),file_count=?1,
 content_count=?1 WHERE id IN(1,2);
 INSERT INTO pending_size(snapshot_id,content_id,size) SELECT 3,id,7 FROM content;`,
			count, count/50)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func checkDistinctEnrichment(t testing.TB, db *DB, count int) {
	t.Helper()
	var wrong int
	err := db.db.QueryRow(`SELECT COUNT(*) FROM directory WHERE
 known_bytes!=file_count*7 OR unique_content_known_bytes!=content_count*7
 OR unknown_size_file_count!=0 OR unknown_size_content_count!=0`).Scan(&wrong)
	if err != nil || wrong != 0 {
		t.Fatalf("incorrect directory enrichment: %d rows, %v", wrong, err)
	}
	var directories int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM directory").Scan(&directories); err != nil {
		t.Fatal(err)
	}
	if directories != 2*(count/50+1) {
		t.Fatalf("got %d directories", directories)
	}
}
