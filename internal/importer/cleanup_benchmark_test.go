package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/database"
)

func BenchmarkDistributionCleanup(b *testing.B) {
	for _, files := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(files), func(b *testing.B) {
			d := importDistributions(files)[1]
			baseline := distributionBaseline(b, d)
			input := writeDistributionInput(b, d, d.history, true)
			for b.Loop() {
				b.StopTimer()
				c := openDistributionCatalog(b, d, baseline)
				b.Cleanup(func() {
					if err := closeCleanupCatalog(&c); err != nil {
						b.Error(err)
					}
				})
				_, err := c.raw.Exec(`CREATE TRIGGER hold_failed_import BEFORE DELETE ON observation
 BEGIN SELECT RAISE(ABORT,'profile fixture held'); END`)
				if err != nil {
					b.Fatal(err)
				}
				_, err = Import(b.Context(), c.db, Request{
					DiskID: c.disk, Path: input, CapturedAt: time.Unix(10, 0),
				})
				if err == nil || !strings.Contains(err.Error(), "profile fixture held") ||
					!strings.Contains(err.Error(), fmt.Sprintf("line %d", files+2)) {
					b.Fatalf("fixture did not retain failed import: %v", err)
				}
				if _, err := c.raw.Exec("DROP TRIGGER hold_failed_import"); err != nil {
					b.Fatal(err)
				}
				id := distributionCount(b, c.raw, "SELECT id FROM snapshot WHERE state='importing'")
				if got := distributionCount(b, c.raw,
					"SELECT COUNT(*) FROM observation WHERE snapshot_id=?", id); got != int64(files) {
					b.Fatalf("failed fixture observations: %d, want %d", got, files)
				}
				b.StartTimer()
				pprof.Do(b.Context(), pprof.Labels("phase", "cleanup"), func(ctx context.Context) {
					if err := c.db.CleanupImport(ctx, id); err != nil {
						b.Fatal(err)
					}
				})
				b.StopTimer()
				if err := closeCleanupCatalog(&c); err != nil {
					b.Fatal(err)
				}
				if err := checkCleanupCatalogBeforeRecovery(b.Context(), d, c); err != nil {
					b.Fatal(err)
				}
				c.db, err = database.OpenContext(b.Context(), c.path)
				if err != nil {
					b.Fatal(err)
				}
				c.raw, err = sql.Open("sqlite", c.path)
				if err != nil {
					b.Fatal(err)
				}
				assertDistributionImport(b, d, c, c.completed[0],
					fmt.Errorf("line %d", files+2), "parse")
				if err := closeCleanupCatalog(&c); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}

func closeCleanupCatalog(c *distributionCatalog) error {
	var err error
	if c.raw != nil {
		err = c.raw.Close()
		c.raw = nil
	}
	if c.db != nil {
		err = errors.Join(err, c.db.Close())
		c.db = nil
	}
	return err
}

func openCleanupCatalogReadOnly(path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro"}
	return sql.Open("sqlite", uri.String())
}

func checkCleanupCatalogBeforeRecovery(
	ctx context.Context, d importDistribution, c distributionCatalog,
) error {
	raw, err := openCleanupCatalogReadOnly(c.path)
	if err != nil {
		return err
	}
	defer raw.Close()
	for table, want := range c.counts {
		var count int64
		if err := raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			return err
		}
		if count != want {
			return fmt.Errorf("before recovery %s: %d, want %d", table, count, want)
		}
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM snapshot WHERE state!='complete'",
		"SELECT COUNT(*) FROM pending_size",
		"SELECT COUNT(*) FROM import_content",
		"SELECT COUNT(*) FROM pragma_foreign_key_check",
		"SELECT COUNT(*) FROM content WHERE size IS NULL OR size!=4096",
		`SELECT COUNT(*) FROM content c
 WHERE NOT EXISTS(SELECT 1 FROM observation o WHERE o.content_id=c.id)`,
		`SELECT COUNT(*) FROM observation o
 WHERE NOT EXISTS(SELECT 1 FROM search_path p WHERE p.path=o.path AND p.sealed=1)`,
		`SELECT COUNT(*) FROM search_path p
 WHERE NOT EXISTS(SELECT 1 FROM observation o WHERE o.path=p.path)`,
		`SELECT COUNT(*) FROM directory_file f JOIN observation o ON o.id=f.observation_id
 WHERE f.snapshot_id!=o.snapshot_id OR f.path!=o.path`,
		`SELECT COUNT(*) FROM search_path
 WHERE id NOT IN (SELECT rowid FROM search_trigram WHERE search_trigram MATCH 'report')`,
		`SELECT COUNT(*) FROM search_trigram WHERE search_trigram MATCH 'report'
 AND rowid NOT IN (SELECT id FROM search_path)`,
	} {
		var count int64
		if err := raw.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("before recovery invariant (%s): %d", query, count)
		}
	}
	for _, snapshot := range c.completed {
		var count int64
		err := raw.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot
 WHERE id=? AND disk_id=? AND state='complete' AND file_count=? AND content_count=?`,
			snapshot.Id, snapshot.DiskId, snapshot.FileCount, snapshot.ContentCount).Scan(&count)
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("before recovery completed snapshot %d changed", snapshot.Id)
		}
		var observedFiles, observedContents int64
		err = raw.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(DISTINCT content_id)
 FROM observation WHERE snapshot_id=?`, snapshot.Id).Scan(&observedFiles, &observedContents)
		if err != nil {
			return err
		}
		if observedFiles != snapshot.FileCount || observedContents != snapshot.ContentCount {
			return fmt.Errorf("before recovery snapshot %d observations changed", snapshot.Id)
		}
		var files, contents, bytes, unknownFiles, uniqueBytes, unknownContents int64
		err = raw.QueryRowContext(ctx, `SELECT file_count,content_count,known_bytes,
 unknown_size_file_count,unique_content_known_bytes,unknown_size_content_count
 FROM directory WHERE snapshot_id=? AND path=''`, snapshot.Id).
			Scan(&files, &contents, &bytes, &unknownFiles, &uniqueBytes, &unknownContents)
		if err != nil {
			return err
		}
		if files != int64(d.files) || contents != int64(d.contents) ||
			bytes != int64(d.files)*4096 || uniqueBytes != int64(d.contents)*4096 ||
			unknownFiles != 0 || unknownContents != 0 {
			return fmt.Errorf("before recovery snapshot %d directory changed", snapshot.Id)
		}
	}
	var integrity string
	if err := raw.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("before recovery integrity: %s", integrity)
	}
	return nil
}
