package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"hash"
	"strings"
	"unicode/utf8"

	"modernc.org/sqlite"
)

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("coldcat_dirname", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			path, ok := args[0].(string)
			if !ok || !utf8.ValidString(path) {
				return nil, fmt.Errorf("invalid UTF-8 directory path")
			}
			if at := strings.LastIndexByte(path, '/'); at >= 0 {
				return path[:at], nil
			}
			return "", nil
		})
}

const directorySchema = `
CREATE TABLE directory (
	 id INTEGER PRIMARY KEY,
 snapshot_id INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
 path TEXT NOT NULL, parent_path TEXT,
 file_count INTEGER NOT NULL DEFAULT 0 CHECK(file_count>=0),
 content_count INTEGER NOT NULL DEFAULT 0 CHECK(content_count>=0),
 known_bytes INTEGER NOT NULL DEFAULT 0 CHECK(typeof(known_bytes)='integer' AND known_bytes>=0),
 unknown_size_file_count INTEGER NOT NULL DEFAULT 0 CHECK(unknown_size_file_count>=0),
 unique_content_known_bytes INTEGER NOT NULL DEFAULT 0
  CHECK(typeof(unique_content_known_bytes)='integer' AND unique_content_known_bytes>=0),
 unknown_size_content_count INTEGER NOT NULL DEFAULT 0 CHECK(unknown_size_content_count>=0),
 max_known_mtime TEXT,
 fingerprint_version INTEGER, fingerprint BLOB,
 UNIQUE(snapshot_id,path),
 CHECK((path='' AND parent_path IS NULL) OR (path!='' AND parent_path IS NOT NULL)),
 CHECK(fingerprint IS NULL OR (typeof(fingerprint)='blob' AND length(fingerprint)=32))
);
CREATE INDEX directory_parent ON directory(snapshot_id,parent_path,path);
CREATE INDEX directory_fingerprint ON directory(fingerprint_version,fingerprint,snapshot_id,path);
CREATE TABLE directory_content (
 snapshot_id INTEGER NOT NULL, directory_path TEXT NOT NULL,
 content_id INTEGER NOT NULL REFERENCES content(id),
 occurrences INTEGER NOT NULL CHECK(occurrences>0),
 PRIMARY KEY(snapshot_id,directory_path,content_id),
 FOREIGN KEY(snapshot_id,directory_path) REFERENCES directory(snapshot_id,path) ON DELETE CASCADE
);
CREATE INDEX directory_content_reverse ON directory_content(content_id,snapshot_id,directory_path);
CREATE TABLE directory_file (
 observation_id INTEGER PRIMARY KEY REFERENCES observation(id) ON DELETE CASCADE,
 snapshot_id INTEGER NOT NULL, directory_path TEXT NOT NULL, path TEXT NOT NULL,
 FOREIGN KEY(snapshot_id,directory_path) REFERENCES directory(snapshot_id,path) ON DELETE CASCADE
);
CREATE INDEX directory_file_parent
 ON directory_file(snapshot_id,directory_path,path,observation_id);
CREATE TABLE directory_build (
 snapshot_id INTEGER PRIMARY KEY REFERENCES snapshot(id) ON DELETE CASCADE
);
CREATE TRIGGER directory_build_observation_insert AFTER INSERT ON observation
 BEGIN DELETE FROM directory_build WHERE snapshot_id=NEW.snapshot_id; END;
CREATE TRIGGER directory_build_observation_update AFTER UPDATE ON observation
 BEGIN DELETE FROM directory_build WHERE snapshot_id IN(OLD.snapshot_id,NEW.snapshot_id); END;
CREATE TRIGGER directory_build_observation_delete AFTER DELETE ON observation
 BEGIN DELETE FROM directory_build WHERE snapshot_id=OLD.snapshot_id; END;
CREATE TRIGGER directory_build_size_insert AFTER INSERT ON pending_size
 BEGIN DELETE FROM directory_build WHERE snapshot_id=NEW.snapshot_id; END;
CREATE TRIGGER directory_build_size_update AFTER UPDATE ON pending_size
 BEGIN DELETE FROM directory_build WHERE snapshot_id IN(OLD.snapshot_id,NEW.snapshot_id); END;
CREATE TRIGGER directory_build_size_delete AFTER DELETE ON pending_size
 WHEN (SELECT state FROM snapshot WHERE id=OLD.snapshot_id)='importing'
 BEGIN DELETE FROM directory_build WHERE snapshot_id=OLD.snapshot_id; END;
CREATE TRIGGER immutable_directory_insert BEFORE INSERT ON directory
 WHEN (SELECT state FROM snapshot WHERE id=NEW.snapshot_id)='complete'
 BEGIN SELECT RAISE(ABORT,'complete directory tree is immutable'); END;
CREATE TRIGGER immutable_directory_delete BEFORE DELETE ON directory
 WHEN (SELECT state FROM snapshot WHERE id=OLD.snapshot_id)='complete'
 BEGIN SELECT RAISE(ABORT,'complete directory tree is immutable'); END;
CREATE TRIGGER immutable_directory_identity BEFORE UPDATE ON directory
 WHEN (SELECT state FROM snapshot WHERE id=OLD.snapshot_id)='complete' AND (
 NEW.id!=OLD.id OR NEW.snapshot_id!=OLD.snapshot_id OR NEW.path!=OLD.path
 OR NEW.parent_path IS NOT OLD.parent_path
 OR NEW.file_count!=OLD.file_count OR NEW.content_count!=OLD.content_count
 OR NEW.max_known_mtime IS NOT OLD.max_known_mtime
 OR NEW.fingerprint_version IS NOT OLD.fingerprint_version
 OR NEW.fingerprint IS NOT OLD.fingerprint)
 BEGIN SELECT RAISE(ABORT,'complete directory tree is immutable'); END;
CREATE TRIGGER directory_build_directory_delete AFTER DELETE ON directory
 BEGIN DELETE FROM directory_build WHERE snapshot_id=OLD.snapshot_id; END;
`

const directoryAncestors = `WITH RECURSIVE ancestors(observation_id,content_id,path,rest) AS (
 SELECT id,content_id,'',path FROM observation WHERE snapshot_id=?
 UNION ALL
 SELECT observation_id,content_id,
 CASE WHEN path='' THEN '' ELSE path||'/' END||substr(rest,1,instr(rest,'/')-1),
 substr(rest,instr(rest,'/')+1) FROM ancestors WHERE instr(rest,'/')>0
) `

func (db *DB) BuildDirectories(ctx context.Context, snapshotID int64) error {
	return db.TransactionContext(ctx, func(tx *Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM snapshot WHERE id=?", snapshotID).
			Scan(&state); err != nil {
			return err
		}
		if state != "importing" {
			return fmt.Errorf("%w: directory build requires importing snapshot", ErrConflict)
		}

		if _, err := tx.ExecContext(ctx, `WITH RECURSIVE ancestors(path) AS (
 VALUES('')
 UNION SELECT coldcat_dirname(path) FROM observation WHERE snapshot_id=?
 UNION SELECT coldcat_dirname(path) FROM ancestors WHERE path!=''
 ) INSERT INTO directory(snapshot_id,path,parent_path)
 SELECT ?,path,CASE WHEN path='' THEN NULL ELSE coldcat_dirname(path) END FROM ancestors`,
			snapshotID, snapshotID); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, directoryAncestors+`INSERT INTO directory_content
 SELECT ?,path,content_id,COUNT(*) FROM ancestors GROUP BY path,content_id`,
			snapshotID, snapshotID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO directory_file
 SELECT id,?,coldcat_dirname(path),path FROM observation WHERE snapshot_id=?`,
			snapshotID, snapshotID); err != nil {
			return err
		}

		if err := buildDirectorySummaries(ctx, tx, snapshotID); err != nil {
			return err
		}

		rows, err := tx.QueryContext(ctx, `SELECT path FROM directory WHERE snapshot_id=?
 ORDER BY length(path) DESC,path`, snapshotID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				return err
			}
			if err := buildDirectoryFingerprint(ctx, tx, snapshotID, path); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, "INSERT INTO directory_build VALUES(?)", snapshotID)
		return err
	})
}

func buildDirectorySummaries(ctx context.Context, tx *Tx, snapshotID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE directory AS d SET
 (file_count,content_count,known_bytes,unknown_size_file_count,
 unique_content_known_bytes,unknown_size_content_count)=(
 SELECT COALESCE(SUM(occurrences),0),COUNT(*),COALESCE(SUM(size*occurrences),0),
 COALESCE(SUM(CASE WHEN size IS NULL THEN occurrences ELSE 0 END),0),
 COALESCE(SUM(size),0),COUNT(*) FILTER(WHERE size IS NULL) FROM (
 SELECT dc.occurrences,COALESCE(c.size,p.size) AS size FROM directory_content dc
 JOIN content c ON c.id=dc.content_id
 LEFT JOIN pending_size p ON p.content_id=c.id AND p.snapshot_id=d.snapshot_id
 WHERE dc.snapshot_id=d.snapshot_id AND dc.directory_path=d.path))
 WHERE d.snapshot_id=?`, snapshotID)
	return err
}

func directoryPrefix(path string) string {
	if path == "" {
		return ""
	}
	return path + "/"
}

func directoryUpper(path string) string {
	if path == "" {
		// Valid UTF-8 paths sort below this byte under SQLite's BINARY collation.
		return "\xff"
	}
	// '/' immediately precedes '0', so this bounds descendants without matching siblings.
	return path + "0"
}

func fingerprintField(h hash.Hash, data []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(data)))
	h.Write(size[:])
	h.Write(data)
}

func buildDirectoryFingerprint(ctx context.Context, tx *Tx, snapshotID int64, path string) error {
	rows, err := tx.QueryContext(ctx, `SELECT path,kind,algorithm,digest,mtime FROM (
  SELECT f.path,'file' AS kind,c.hash_type AS algorithm,c.hash AS digest,o.mtime AS mtime
 FROM directory_file f JOIN observation o ON o.id=f.observation_id
 JOIN content c ON c.id=o.content_id WHERE f.snapshot_id=? AND f.directory_path=?
  UNION ALL SELECT path,'directory','directory-v1',fingerprint,max_known_mtime FROM directory
 WHERE snapshot_id=? AND parent_path=?) ORDER BY path,kind`,
		snapshotID, path, snapshotID, path)
	if err != nil {
		return err
	}
	defer rows.Close()
	h := sha256.New()
	fingerprintField(h, []byte("coldcat-directory-v1"))
	var maxMTime sql.NullString
	for rows.Next() {
		var entryPath, kind, algorithm string
		var digest []byte
		var mtime sql.NullString
		if err := rows.Scan(&entryPath, &kind, &algorithm, &digest, &mtime); err != nil {
			return err
		}
		if digest == nil {
			return fmt.Errorf("directory %q has unfinished child fingerprint", path)
		}
		if mtime.Valid && (!maxMTime.Valid || mtime.String > maxMTime.String) {
			maxMTime = mtime
		}
		for _, data := range [][]byte{
			[]byte(kind), []byte(strings.TrimPrefix(entryPath, directoryPrefix(path))),
			[]byte(algorithm), digest,
		} {
			fingerprintField(h, data)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `UPDATE directory
 SET fingerprint_version=1,fingerprint=?,max_known_mtime=?
  WHERE snapshot_id=? AND path=?`, h.Sum(nil), maxMTime, snapshotID, path)
	return err
}

func enrichDirectories(ctx context.Context, tx *Tx, snapshotID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT dc.snapshot_id,dc.directory_path
 FROM pending_size p JOIN content c ON c.id=p.content_id AND c.size IS NULL
 JOIN directory_content dc ON dc.content_id=p.content_id
 WHERE p.snapshot_id=? AND dc.snapshot_id!=?`, snapshotID, snapshotID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE directory SET
 known_bytes=known_bytes+(SELECT SUM(dc.occurrences*p.size)
 FROM directory_content dc JOIN pending_size p ON p.content_id=dc.content_id
 JOIN content c ON c.id=p.content_id AND c.size IS NULL
 WHERE dc.snapshot_id=? AND dc.directory_path=? AND p.snapshot_id=?),
 unknown_size_file_count=unknown_size_file_count-(SELECT SUM(dc.occurrences)
 FROM directory_content dc JOIN pending_size p ON p.content_id=dc.content_id
 JOIN content c ON c.id=p.content_id AND c.size IS NULL
 WHERE dc.snapshot_id=? AND dc.directory_path=? AND p.snapshot_id=?),
 unique_content_known_bytes=unique_content_known_bytes+(SELECT SUM(p.size)
 FROM directory_content dc JOIN pending_size p ON p.content_id=dc.content_id
 JOIN content c ON c.id=p.content_id AND c.size IS NULL
 WHERE dc.snapshot_id=? AND dc.directory_path=? AND p.snapshot_id=?),
 unknown_size_content_count=unknown_size_content_count-(SELECT COUNT(*)
 FROM directory_content dc JOIN pending_size p ON p.content_id=dc.content_id
 JOIN content c ON c.id=p.content_id AND c.size IS NULL
 WHERE dc.snapshot_id=? AND dc.directory_path=? AND p.snapshot_id=?)
 WHERE snapshot_id=? AND path=?`,
			id, path, snapshotID, id, path, snapshotID,
			id, path, snapshotID, id, path, snapshotID, id, path)
		if err != nil {
			return err
		}
	}
	return rows.Err()
}
