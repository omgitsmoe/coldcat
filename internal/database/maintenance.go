package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

type StorageObject struct {
	Name         string `json:"name"`
	Bytes        int64  `json:"bytes"`
	PayloadBytes int64  `json:"payload_bytes"`
	UnusedBytes  int64  `json:"unused_bytes"`
}

type StorageReport struct {
	PageSize        int64           `json:"page_size"`
	PageCount       int64           `json:"page_count"`
	FreePages       int64           `json:"free_pages"`
	Snapshots       int64           `json:"snapshots"`
	HistoricalFiles int64           `json:"historical_files"`
	Objects         []StorageObject `json:"objects"`
}

func (db *DB) Storage(ctx context.Context) (StorageReport, error) {
	report := StorageReport{Objects: []StorageObject{}}
	release, err := db.readAccess()
	if err != nil {
		return report, err
	}
	defer release()

	err = db.TransactionContext(ctx, func(tx *Tx) error {
		for _, query := range []struct {
			sql   string
			value *int64
		}{
			{"PRAGMA page_size", &report.PageSize},
			{"PRAGMA page_count", &report.PageCount},
			{"PRAGMA freelist_count", &report.FreePages},
			{"SELECT COUNT(*) FROM snapshot WHERE state='complete'", &report.Snapshots},
			{"SELECT COALESCE(SUM(file_count),0) FROM snapshot WHERE state='complete'",
				&report.HistoricalFiles},
		} {
			if err := tx.QueryRowContext(ctx, query.sql).Scan(query.value); err != nil {
				return err
			}
		}

		rows, err := tx.QueryContext(ctx, `SELECT name,SUM(pgsize),SUM(payload),SUM(unused)
 FROM dbstat GROUP BY name ORDER BY SUM(pgsize) DESC,name`)
		if err != nil {
			return fmt.Errorf("measure catalog objects: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var object StorageObject
			if err := rows.Scan(&object.Name, &object.Bytes, &object.PayloadBytes,
				&object.UnusedBytes); err != nil {
				return err
			}
			report.Objects = append(report.Objects, object)
		}
		return rows.Err()
	})
	return report, err
}

func (db *DB) Check(ctx context.Context) error {
	release, err := db.readAccess()
	if err != nil {
		return err
	}
	defer release()
	return db.TransactionContext(ctx, func(tx *Tx) error {
		var result string
		if err := tx.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
			return err
		}
		if result != "ok" {
			return fmt.Errorf("catalog integrity check: %s", result)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO search_trigram(search_trigram,rank)
VALUES('integrity-check',1)`); err != nil {
			return fmt.Errorf("search index integrity check: %w", err)
		}
		return checkForeignKeys(ctx, tx)
	})
}

// CompactInto keeps the logical schema compatible with existing catalogs; only
// the physical membership layout changes. The source is never rewritten here.
func (db *DB) CompactInto(ctx context.Context, output string) (result error) {
	if !db.access.TryLock() {
		return ErrBusy
	}
	defer db.access.Unlock()
	if db.recoveryErr != nil {
		return fmt.Errorf("catalog recovery required: %w", db.recoveryErr)
	}

	canonical, lock, err := acquireCatalogLock(output)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, lock.Close()) }()

	file, err := os.OpenFile(canonical, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create compacted catalog (output must not exist): %w", err)
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, os.Remove(canonical))
		}
	}()
	if err := file.Close(); err != nil {
		return err
	}

	if _, err := db.db.ExecContext(ctx, "VACUUM INTO ?", canonical); err != nil {
		return fmt.Errorf("copy catalog: %w", err)
	}

	copied, err := sql.Open("sqlite", databaseDSN(canonical))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, copied.Close()) }()
	target := &DB{db: copied}
	err = target.TransactionContext(ctx, func(tx *Tx) error {
		_, err := tx.ExecContext(ctx, `DROP INDEX directory_content_reverse;
ALTER TABLE directory_content RENAME TO directory_content_old;`+directoryContentSchema+`
INSERT INTO directory_content SELECT * FROM directory_content_old;
DROP TABLE directory_content_old;`)
		return err
	})
	if err != nil {
		return fmt.Errorf("compact directory memberships: %w", err)
	}
	err = target.TransactionContext(ctx, func(tx *Tx) error {
		_, err := tx.ExecContext(ctx, `DROP TABLE search_trigram;`+searchTrigramSchema+`
INSERT INTO search_trigram(search_trigram) VALUES('rebuild');`)
		return err
	})
	if err != nil {
		return fmt.Errorf("compact trigram search index: %w", err)
	}
	if _, err := copied.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("reclaim copied catalog pages: %w", err)
	}
	return target.Check(ctx)
}
