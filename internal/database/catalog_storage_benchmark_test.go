package database

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

const catalogStoragePrefix = "archive/photographs-and-documents/collection-with-long-name/" +
	"year-2026/地域と資料/project-long-directory/"

// Keep this fixture identical across revisions: both derived indexes are built
// by production code, and historical observations remain in the measured file.
func buildCatalogStorageFixture(b *testing.B, initialize func(*DB) error) *DB {
	db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := db.Close(); err != nil {
			b.Error(err)
		}
	})
	if initialize != nil {
		if err := initialize(db); err != nil {
			b.Fatal(err)
		}
	}
	ctx := b.Context()
	started := time.Now()
	err = db.TransactionContext(ctx, func(tx *Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO disk(id,label,capacity)
 VALUES(1,'first',0),(2,'second',0);
 WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<25000)
 INSERT INTO content(id,size,hash_type,hash)
 SELECT i,1024+i,'sha256',CAST(printf('%032d',i) AS BLOB) FROM n;`)
		return err
	})
	if err != nil {
		b.Fatal(err)
	}
	for id := 1; id <= 4; id++ {
		disk, history := (id-1)%2, (id-1)/2
		err := db.TransactionContext(ctx, func(tx *Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(?,?,'importing',?,?,'explicit','cshd');`, id, disk+1,
				fmt.Sprintf("2026-01-0%dT00:00:00.000000000Z", history+1),
				"2026-01-03T00:00:00.000000000Z")
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `WITH RECURSIVE n(i) AS
 (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<12500)
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT ?,CASE WHEN ?=1 AND i%5=0 THEN 20000+?*2500+i/5
 ELSE 1+?*10000+(i-1)%10000 END,
 ?||printf('group-%03d/document-%07d',i%100,i)||
 CASE WHEN ?=1 AND i%5=0 THEN '-revised-猫.txt' ELSE '-猫.txt' END FROM n`,
				id, history, disk, disk, catalogStoragePrefix, history)
			return err
		})
		if err != nil {
			b.Fatal(err)
		}
		if err := db.BuildDirectories(ctx, int64(id)); err != nil {
			b.Fatal(err)
		}
		if _, err := db.db.ExecContext(ctx, `UPDATE snapshot SET state='complete',
 source_digest=zeroblob(32),file_count=12500,
 content_count=(SELECT COUNT(DISTINCT content_id) FROM observation WHERE snapshot_id=?)
 WHERE id=?`, id, id); err != nil {
			b.Fatal(err)
		}
	}
	b.Logf("full indexed fixture build: %s", time.Since(started))
	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM snapshot WHERE state='complete'": 4,
		"SELECT COUNT(*) FROM observation":                     50000,
		"SELECT COUNT(*) FROM content":                         25000,
		"SELECT COUNT(*) FROM search_path":                     15000,
		"SELECT COUNT(*) FROM directory_content":               328000,
	} {
		var got int64
		if err := db.db.QueryRowContext(ctx, query).Scan(&got); err != nil || got != want {
			b.Fatalf("fixture %s = %d, want %d: %v", query, got, want, err)
		}
	}
	return db
}

func BenchmarkCatalogStorage(b *testing.B) {
	db := buildCatalogStorageFixture(b, nil)
	ctx := b.Context()
	for _, compact := range []bool{false, true} {
		if compact {
			if _, err := db.db.ExecContext(ctx, "VACUUM"); err != nil {
				b.Fatal(err)
			}
		}
		var pages, pageSize, free int64
		for query, value := range map[string]*int64{
			"PRAGMA page_count": &pages, "PRAGMA page_size": &pageSize,
			"PRAGMA freelist_count": &free,
		} {
			if err := db.db.QueryRowContext(ctx, query).Scan(value); err != nil {
				b.Fatal(err)
			}
		}
		b.Logf("vacuum=%v total=%d free=%d", compact, pages*pageSize, free*pageSize)
	}
	rows, err := db.db.QueryContext(ctx, `SELECT name,SUM(pgsize) FROM dbstat
 GROUP BY name ORDER BY SUM(pgsize) DESC LIMIT 12`)
	if err != nil {
		b.Fatal(err)
	}
	for rows.Next() {
		var name string
		var size int64
		if err := rows.Scan(&name, &size); err != nil {
			b.Fatal(err)
		}
		b.Logf("object %s %d", name, size)
	}
	if err := rows.Err(); err != nil {
		b.Fatal(err)
	}
	rows.Close()
	for _, name := range []string{"detail_root", "detail_nested", "entries", "search"} {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				var err error
				switch name {
				case "detail_root":
					_, err = db.GetDirectory(ctx, 3, "")
				case "detail_nested":
					_, err = db.GetDirectory(ctx, 3, catalogStoragePrefix+"group-050")
				case "entries":
					_, err = db.ListDirectoryEntries(ctx, base.DirectoryFilters{
						SnapshotID: 3, Path: catalogStoragePrefix + "group-050",
						ReplicaMetric: base.ReplicaDisks,
					}, 50, base.DirectoryAnchor{}, nil)
				case "search":
					_, err = db.Search(ctx, base.SearchFilters{
						Query: "document-0000050", Field: "name", Match: "substring",
						ContentFilters: base.ContentFilters{
							Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks,
						},
					}, 50, base.SearchAnchor{}, nil)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
