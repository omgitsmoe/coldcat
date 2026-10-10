package database

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceDigestConstraints(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateDisk("disk", "", "", 0); err != nil {
		t.Fatal(err)
	}

	for _, digest := range []string{"NULL", "zeroblob(31)", "zeroblob(33)", "printf('%032d', 0)"} {
		err := db.Transaction(func(tx *Tx) error {
			_, err := tx.Exec(
				`INSERT INTO snapshot(disk_id,state,captured_at,imported_at,capture_provenance,file_count,content_count,source_digest)
 VALUES(1,'complete','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit',0,0,` + digest + `)`,
			)
			return err
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("accepted invalid digest %s: %v", digest, err)
		}
	}
}

func TestImportCleanupReferenceIndexes(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	queries := []struct {
		name  string
		query string
		index string
	}{
		{"owned content",
			"SELECT snapshot_id FROM import_content WHERE content_id=? AND snapshot_id!=?",
			"import_content_content"},
		{"staged size", "SELECT snapshot_id FROM pending_size WHERE content_id=? AND snapshot_id!=?",
			"pending_size_content"},
		{"observations", "DELETE FROM observation WHERE snapshot_id=?",
			"sqlite_autoindex_observation_1"},
		{"shared path", "SELECT 1 FROM observation WHERE path=? AND snapshot_id!=?",
			"observation_path_snapshot"},
		{"search path", "DELETE FROM search_path WHERE path=? AND sealed=?",
			"sqlite_autoindex_search_path_1"},
		{"directory file cascade",
			"SELECT 1 FROM directory_file WHERE observation_id=? AND snapshot_id=?",
			"INTEGER PRIMARY KEY"},
	}
	for _, query := range queries {
		args := make([]any, strings.Count(query.query, "?"))
		for i := range args {
			args[i] = 1
		}
		rows, err := db.db.Query("EXPLAIN QUERY PLAN "+query.query, args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		t.Logf("%s: %v", query.name, plan)
		if !strings.Contains(strings.Join(plan, "\n"), query.index) {
			t.Fatalf("unindexed cleanup reference %s: %v", query.name, plan)
		}
	}
}

func TestImportCleanupErrorStages(t *testing.T) {
	for _, stage := range []struct {
		table string
		label string
	}{
		{"directory", "directories"},
		{"observation", "observations"},
		{"pending_size", "staged sizes"},
		{"content", "owned content"},
		{"snapshot", "snapshot"},
	} {
		t.Run(stage.table, func(t *testing.T) {
			db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1,1,'importing','2023-01-01','2023-01-01','explicit');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',X'AB');
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'file');
 INSERT INTO pending_size(snapshot_id,content_id,size) VALUES(1,1,5);
 INSERT INTO import_content(snapshot_id,content_id) VALUES(1,1);`)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.BuildDirectories(t.Context(), 1); err != nil {
				t.Fatal(err)
			}
			_, err = db.db.Exec("CREATE TRIGGER fail_cleanup BEFORE DELETE ON " + stage.table +
				" BEGIN SELECT RAISE(ABORT,'cleanup blocked'); END;")
			if err != nil {
				t.Fatal(err)
			}

			err = db.CleanupImport(t.Context(), 1)
			if err == nil || !strings.Contains(err.Error(), stage.label) ||
				!strings.Contains(err.Error(), "cleanup blocked") {
				t.Fatalf("cleanup stage %s: %v", stage.table, err)
			}
			for _, table := range []string{
				"snapshot", "observation", "content", "pending_size", "import_content",
				"directory", "directory_file", "directory_content", "directory_build", "search_path",
			} {
				var count int
				if err := db.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 1 {
					t.Fatalf("failed cleanup changed %s: %d, want 1", table, count)
				}
			}
		})
	}
}

func TestPublicationFailureRollsBackDigestAndEnrichment(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance,input_format) VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit','cshd');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',X'AB');
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'file');
 INSERT INTO pending_size(snapshot_id,content_id,size) VALUES(1,1,5);
 INSERT INTO import_content(snapshot_id,content_id) VALUES(1,1);
 CREATE TRIGGER fail_publish BEFORE UPDATE ON snapshot WHEN NEW.state='complete' BEGIN SELECT RAISE(ABORT,'publication failed'); END;`)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.BuildDirectories(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	_, err = db.PublishImport(t.Context(), PublishImportRequest{
		SnapshotID: 1, SourceDigest: [32]byte{1},
	})
	if err == nil || !strings.Contains(err.Error(), "publication failed") {
		t.Fatalf("publication failure: %v", err)
	}

	var untouched bool
	if err := db.db.QueryRow(`SELECT s.state='importing' AND s.source_digest IS NULL AND c.size IS NULL
 AND EXISTS(SELECT 1 FROM pending_size WHERE snapshot_id=1)
 AND EXISTS(SELECT 1 FROM import_content WHERE snapshot_id=1)
 FROM snapshot s JOIN content c ON c.id=1 WHERE s.id=1`).Scan(&untouched); err != nil ||
		!untouched {
		t.Fatalf("publication did not roll back: %v %v", untouched, err)
	}
}
