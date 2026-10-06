package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitialSchemaReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	for pass := range 2 {
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}

		var version int
		if err := db.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			t.Fatal(err)
		}

		if version != len(migrations) {
			t.Fatalf("schema version: %d", version)
		}

		var tables int
		if err := db.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'",
		).Scan(&tables); err != nil {
			t.Fatal(err)
		}

		if tables != 6 {
			t.Fatalf("initial schema has %d tables", tables)
		}

		if pass == 0 {
			if _, err := db.CreateDisk("preserved", "", "", 100); err != nil {
				t.Fatal(err)
			}
		} else if id, err := db.DiskIDByLabel("preserved"); err != nil || id != 1 {
			t.Fatalf("reopen changed disk: %d %v", id, err)
		}

		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInitialSchemaRollbackAndRetry(t *testing.T) {
	raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	db := &DB{db: raw}
	failure := errors.New("injected migration failure")
	err = db.applyMigrations(t.Context(), []migration{func(ctx context.Context, tx *Tx) error {
		if err := migrateInitial(ctx, tx); err != nil {
			return err
		}

		return failure
	}})
	if !errors.Is(err, failure) {
		t.Fatalf("migration error: %v", err)
	}

	var version, objects int
	if err := raw.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}

	if err := raw.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
		t.Fatal(err)
	}

	if version != 0 || objects != 0 {
		t.Fatalf("failed migration retained version %d and %d objects", version, objects)
	}

	if err := db.migrate(t.Context()); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestOrderedMigrationsRollbackAndResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { raw.Close() })
	db := &DB{db: raw}
	failure := errors.New("injected upgrade failure")
	fail := true
	secondRuns := 0
	steps := []migration{
		migrateInitial,
		func(ctx context.Context, tx *Tx) error {
			secondRuns++
			_, err := tx.ExecContext(
				ctx,
				"CREATE TABLE migration_probe(id INTEGER PRIMARY KEY); INSERT INTO migration_probe VALUES(1);",
			)
			return err
		},
		func(ctx context.Context, tx *Tx) error {
			if _, err := tx.ExecContext(ctx, "CREATE TABLE migration_scratch(id INTEGER); INSERT INTO migration_probe VALUES(2);"); err != nil {
				return err
			}

			if fail {
				return failure
			}

			return nil
		},
	}
	if err := db.applyMigrations(t.Context(), steps); !errors.Is(err, failure) {
		t.Fatalf("upgrade: %v", err)
	}

	var version, count, scratch int
	if err := raw.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}

	if err := raw.QueryRow("SELECT COUNT(*) FROM migration_probe").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if err := raw.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name='migration_scratch'").Scan(&scratch); err != nil {
		t.Fatal(err)
	}

	if version != 2 || count != 1 || scratch != 0 {
		t.Fatalf("rollback: version %d count %d scratch %d", version, count, scratch)
	}

	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	db = &DB{db: raw}
	fail = false
	for range 2 {
		if err := db.applyMigrations(t.Context(), steps); err != nil {
			t.Fatalf("resume: %v", err)
		}
	}

	if err := raw.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}

	if err := raw.QueryRow("SELECT COUNT(*) FROM migration_probe").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if version != 3 || count != 2 || secondRuns != 1 {
		t.Fatalf("resume: version %d count %d second migration runs %d", version, count, secondRuns)
	}
}

func TestRejectUnexpectedSchema(t *testing.T) {
	for _, schema := range []string{
		"CREATE TABLE disk(id INTEGER);",
		"PRAGMA user_version=999;",
		"CREATE TABLE unrelated(id INTEGER);",
	} {
		path := filepath.Join(t.TempDir(), "bad.sqlite")
		raw, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := raw.Exec(schema); err != nil {
			t.Fatal(err)
		}

		raw.Close()
		if db, err := Open(path); err == nil {
			db.Close()
			t.Fatalf("accepted %s", schema)
		}
	}
}

func TestRecoveryIsRequiredBeforeUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',1);
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance) VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit');
INSERT INTO content(id,size,hash_type,hash) VALUES(1,NULL,'sha256',X'AB');
INSERT INTO import_content VALUES(1,1);
INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'file');
CREATE TRIGGER fail_cleanup BEFORE DELETE ON snapshot BEGIN SELECT RAISE(ABORT,'cleanup blocked'); END;`)
	if err != nil {
		t.Fatal(err)
	}

	db.Close()
	if db, err := Open(path); err == nil {
		db.Close()
		t.Fatal("startup succeeded despite failed recovery")
	} else if !strings.Contains(err.Error(), "cleanup blocked") {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := raw.Exec("DROP TRIGGER fail_cleanup"); err != nil {
		t.Fatal(err)
	}

	raw.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"snapshot", "content", "observation", "import_content", "pending_size"} {
		var count int
		if err := db.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}

		if count != 0 {
			t.Fatalf("%s retained %d rows", table, count)
		}
	}
}

func TestCatalogLockAcrossProcesses(t *testing.T) {
	if path := os.Getenv("COLDCAT_LOCK_TEST"); path != "" {
		db, err := Open(path)
		if os.Getenv("COLDCAT_LOCK_EXPECT_BUSY") == "1" {
			if !errors.Is(err, ErrBusy) {
				t.Fatalf("expected lock conflict: %v", err)
			}

			return
		}

		if err != nil {
			t.Fatal(err)
		}

		_ = db
		os.Exit(0)
	}

	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0], "-test.run=^TestCatalogLockAcrossProcesses$")
	child.Env = append(os.Environ(), "COLDCAT_LOCK_TEST="+path, "COLDCAT_LOCK_EXPECT_BUSY=1")
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}

	db.Close()
	child = exec.Command(os.Args[0], "-test.run=^TestCatalogLockAcrossProcesses$")
	child.Env = append(os.Environ(), "COLDCAT_LOCK_TEST="+path)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}

	db.Close()
}
