package database

import (
	"database/sql/driver"
	"encoding/json"
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
	sqlite.MustRegisterDeterministicScalarFunction("coldcat_short_grams", 2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			type posting struct {
				Field string `json:"field"`
				Gram  string `json:"gram"`
			}
			var postings []posting
			for i, field := range []string{"name", "path"} {
				text, ok := args[i].(string)
				if !ok || !utf8.ValidString(text) {
					return nil, fmt.Errorf("invalid UTF-8 search text")
				}
				runes := []rune(text)
				seen := map[string]bool{}
				for start := range runes {
					for width := 1; width <= 2 && start+width <= len(runes); width++ {
						gram := string(runes[start : start+width])
						if !seen[gram] {
							postings = append(postings, posting{field, gram})
							seen[gram] = true
						}
					}
				}
			}
			data, err := json.Marshal(postings)
			return string(data), err
		})
}

const searchSchema = `
CREATE TABLE search_path (
 id INTEGER PRIMARY KEY, path TEXT NOT NULL UNIQUE, name TEXT NOT NULL
);
CREATE INDEX search_name ON search_path(name,path);
CREATE VIRTUAL TABLE search_trigram USING fts5(
 name, path, content='search_path', content_rowid='id', tokenize='trigram case_sensitive 1'
);
CREATE TABLE search_short (
 field TEXT NOT NULL CHECK(field IN ('name','path')), gram TEXT NOT NULL,
 path_id INTEGER NOT NULL REFERENCES search_path(id) ON DELETE CASCADE,
 PRIMARY KEY(field,gram,path_id)
) WITHOUT ROWID;
CREATE INDEX search_short_path ON search_short(path_id);
CREATE TRIGGER search_path_insert AFTER INSERT ON search_path BEGIN
 INSERT INTO search_trigram(rowid,name,path) VALUES(NEW.id,NEW.name,NEW.path);
 INSERT INTO search_short(field,gram,path_id)
 SELECT json_extract(value,'$.field'),json_extract(value,'$.gram'),NEW.id
 FROM json_each(coldcat_short_grams(NEW.name,NEW.path));
END;
CREATE TRIGGER search_path_delete AFTER DELETE ON search_path BEGIN
 INSERT INTO search_trigram(search_trigram,rowid,name,path)
 VALUES('delete',OLD.id,OLD.name,OLD.path);
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
 BEGIN SELECT RAISE(ABORT,'search paths are immutable'); END;
CREATE TRIGGER immutable_search_path_delete BEFORE DELETE ON search_path
 WHEN EXISTS(SELECT 1 FROM observation o JOIN snapshot s ON s.id=o.snapshot_id
 WHERE o.path=OLD.path AND s.state='complete')
 BEGIN SELECT RAISE(ABORT,'complete snapshot search index is immutable'); END;
CREATE TRIGGER immutable_search_short_delete BEFORE DELETE ON search_short
 WHEN EXISTS(SELECT 1 FROM search_path WHERE id=OLD.path_id)
 BEGIN SELECT RAISE(ABORT,'delete search postings through their owning path'); END;
CREATE TRIGGER immutable_search_short_update BEFORE UPDATE ON search_short
 BEGIN SELECT RAISE(ABORT,'search postings are immutable'); END;
CREATE TRIGGER immutable_search_short_insert BEFORE INSERT ON search_short
 WHEN EXISTS(SELECT 1 FROM observation o JOIN search_path p ON p.path=o.path
 JOIN snapshot s ON s.id=o.snapshot_id WHERE p.id=NEW.path_id AND s.state='complete')
 BEGIN SELECT RAISE(ABORT,'complete snapshot search index is immutable'); END;
`
