package database

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"unicode/utf8"

	"modernc.org/sqlite"
)

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("coldcat_basename", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			path, ok := args[0].(string)
			if !ok || !utf8.ValidString(path) || strings.ContainsRune(path, 0) {
				return nil, fmt.Errorf("invalid UTF-8 search path")
			}
			return path[strings.LastIndexByte(path, '/')+1:], nil
		})
}

const searchSchema = `
CREATE TABLE search_path (
 id INTEGER PRIMARY KEY, path TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
 sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0,1)),
 name_fold TEXT GENERATED ALWAYS AS (coldcat_fold_v1(name)) STORED,
 path_fold TEXT GENERATED ALWAYS AS (coldcat_fold_v1(path)) STORED
);
CREATE INDEX search_name ON search_path(name_fold,path);
CREATE INDEX search_path_fold ON search_path(path_fold);
CREATE VIRTUAL TABLE search_trigram USING fts5(
 name_fold, path_fold, content='search_path', content_rowid='id',
 tokenize='trigram case_sensitive 1'
);
CREATE TRIGGER search_path_insert AFTER INSERT ON search_path BEGIN
 INSERT INTO search_trigram(rowid,name_fold,path_fold)
 VALUES(NEW.id,NEW.name_fold,NEW.path_fold);
END;
CREATE TRIGGER search_path_delete AFTER DELETE ON search_path BEGIN
 INSERT INTO search_trigram(search_trigram,rowid,name_fold,path_fold)
 VALUES('delete',OLD.id,OLD.name_fold,OLD.path_fold);
END;
CREATE TRIGGER observation_search_insert AFTER INSERT ON observation BEGIN
 INSERT INTO search_path(path,name) VALUES(NEW.path,coldcat_basename(NEW.path))
 ON CONFLICT(path) DO NOTHING;
END;
CREATE TRIGGER observation_search_delete AFTER DELETE ON observation BEGIN
 DELETE FROM search_path WHERE path=OLD.path
 AND NOT EXISTS(SELECT 1 FROM observation WHERE path=OLD.path);
END;
CREATE TRIGGER observation_search_update AFTER UPDATE OF path ON observation BEGIN
 INSERT INTO search_path(path,name) VALUES(NEW.path,coldcat_basename(NEW.path))
 ON CONFLICT(path) DO NOTHING;
 DELETE FROM search_path WHERE path=OLD.path
 AND NOT EXISTS(SELECT 1 FROM observation WHERE path=OLD.path);
END;
CREATE TRIGGER immutable_search_path_update BEFORE UPDATE ON search_path
 WHEN NEW.id!=OLD.id OR NEW.path!=OLD.path OR NEW.name!=OLD.name
 OR OLD.sealed=1 OR NEW.sealed!=1 OR NOT EXISTS(
 SELECT 1 FROM observation o JOIN snapshot s ON s.id=o.snapshot_id
 WHERE o.path=OLD.path AND s.state='complete')
 BEGIN SELECT RAISE(ABORT,'search paths are immutable'); END;
CREATE TRIGGER immutable_completed_search_path_insert BEFORE INSERT ON search_path
 WHEN NOT EXISTS(SELECT 1 FROM search_path WHERE path=NEW.path)
 AND EXISTS(SELECT 1 FROM observation o JOIN snapshot s ON s.id=o.snapshot_id
 WHERE o.path=NEW.path AND s.state='complete')
 BEGIN SELECT RAISE(ABORT,'complete snapshot search index is immutable'); END;
CREATE TRIGGER immutable_search_path_delete BEFORE DELETE ON search_path
 WHEN OLD.sealed=1
 BEGIN SELECT RAISE(ABORT,'complete snapshot search index is immutable'); END;
CREATE TRIGGER seal_completed_search_paths AFTER UPDATE OF state ON snapshot
 WHEN NEW.state='complete' BEGIN
 UPDATE search_path SET sealed=1 WHERE sealed=0
 AND path IN (SELECT path FROM observation WHERE snapshot_id=NEW.id);
END;
`
