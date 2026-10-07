package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func testDB(t *testing.T) (*database.DB, *sql.DB, base.DiskId) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	assertNoErr(t, err)
	t.Cleanup(func() { db.Close() })
	raw, err := sql.Open("sqlite", path)
	assertNoErr(t, err)
	t.Cleanup(func() { raw.Close() })
	id, err := db.CreateDisk("disk", "", "", 100)
	assertNoErr(t, err)
	return db, raw, base.DiskId(id)
}

func request(disk base.DiskId) Request {
	return Request{DiskID: disk, Path: "fixture.cshd", CapturedAt: time.Unix(1673815645, 0)}
}

func count(t *testing.T, raw *sql.DB, table string) int {
	t.Helper()

	var n int
	assertNoErr(t, raw.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n))
	return n
}

func manyFiles(n int, known bool) string {
	var input strings.Builder
	if known {
		input.WriteString("# version 1\n")
	}

	for i := range n {
		if known {
			fmt.Fprintf(&input, ",4,sha256,%064x tree/file-%d\n", i, i)
		} else {
			fmt.Fprintf(&input, ",sha256,%064x tree/file-%d\n", i, i)
		}
	}

	return input.String()
}

func TestImportPublishesOnlyAfterSuccess(t *testing.T) {
	db, raw, disk := testDB(t)
	result, err := ImportReader(
		t.Context(),
		db,
		request(disk),
		strings.NewReader(manyFiles(2005, true)),
	)
	assertNoErr(t, err)
	assertEqual(t, result.FileCount, int64(2005))
	assertEqual(t, result.ContentCount, int64(2005))
	if result.ImportedAt.IsZero() {
		t.Fatal("missing import time")
	}

	got, err := db.LatestCompleteSnapshot(t.Context(), disk)
	assertNoErr(t, err)
	assertEqual(t, got.Id, result.Id)
	assertEqual(t, count(t, raw, "pending_size"), 0)
	assertEqual(t, count(t, raw, "import_content"), 0)

	var unknown int
	assertNoErr(t, raw.QueryRow("SELECT COUNT(*) FROM content WHERE size IS NULL").Scan(&unknown))
	assertEqual(t, unknown, 0)
}

func TestImportFailuresCleanEveryCommittedBatch(t *testing.T) {
	tests := map[string]string{
		"late parse failure": manyFiles(1001, true) + "broken\n",
		"duplicate across batches": manyFiles(
			1001,
			true,
		) + ",4,sha256," + fixtureSHA25600000000 + " tree/file-0\n",
		"conflicting size across batches": manyFiles(1001, true) +
			",9,sha256," + fixtureSHA25600000000 + " other\n",
		"conflicting size within batch": "# version 1\n,4,sha256," + fixtureSHA256AB +
			" a\n,5,sha256," + fixtureSHA256AB + " b\n",
		"unsupported version": "# version 2\n",
		"absolute path":       ",sha256," + fixtureSHA256AB + " /absolute\n",
		"parent traversal":    ",sha256," + fixtureSHA256AB + " foo/../bar\n",
		"overflow":            "# version 1\n,9223372036854775808,sha256," + fixtureSHA256AB + " a\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			db, raw, disk := testDB(t)
			_, err := ImportReader(t.Context(), db, request(disk), strings.NewReader(input))
			assertErr(t, err)
			for _, table := range []string{
				"snapshot", "observation", "content", "pending_size", "import_content",
				"search_path", "search_trigram",
			} {
				assertEqual(t, count(t, raw, table), 0)
			}
		})
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestReaderErrorAndCancellationCleanCommittedBatches(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			db, raw, disk := testDB(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("reader failed")

			var reader io.Reader = io.MultiReader(strings.NewReader(manyFiles(1001, true)), errorReader{failure})
			if cancelled {
				reader = io.MultiReader(
					strings.NewReader(manyFiles(1001, true)),
					cancelReader{cancel: cancel},
				)
			}

			_, err := ImportReader(ctx, db, request(disk), reader)
			if cancelled {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatal(err)
			}

			for _, table := range []string{
				"snapshot", "observation", "content", "pending_size", "import_content",
				"search_path", "search_trigram",
			} {
				assertEqual(t, count(t, raw, table), 0)
			}
		})
	}
}

type cancelReader struct{ cancel context.CancelFunc }

func (r cancelReader) Read([]byte) (int, error) { r.cancel(); return 0, context.Canceled }

func TestFailedEnrichmentPreservesCompletedContent(t *testing.T) {
	db, raw, disk := testDB(t)
	first, err := ImportReader(
		t.Context(),
		db,
		request(disk),
		strings.NewReader(",sha256,"+fixtureSHA25600000000+" original\n"),
	)
	assertNoErr(t, err)
	_, err = ImportReader(
		t.Context(),
		db,
		request(disk),
		strings.NewReader(manyFiles(1001, true)+"broken\n"),
	)
	assertErr(t, err)
	assertEqual(t, count(t, raw, "snapshot"), 1)
	assertEqual(t, count(t, raw, "content"), 1)

	var size sql.NullInt64
	assertNoErr(t, raw.QueryRow("SELECT size FROM content").Scan(&size))
	if size.Valid {
		t.Fatal("failed import enriched shared content")
	}

	_, err = db.GetCompleteSnapshot(t.Context(), first.Id)
	assertNoErr(t, err)
	_, err = ImportReader(
		t.Context(),
		db,
		request(disk),
		strings.NewReader("# version 1\n,0,sha256,"+fixtureSHA25600000000+" empty\n"),
	)
	assertNoErr(t, err)
	assertNoErr(t, raw.QueryRow("SELECT size FROM content").Scan(&size))
	if !size.Valid || size.Int64 != 0 {
		t.Fatalf("empty file: %v", size)
	}

	_, err = ImportReader(
		t.Context(),
		db,
		request(disk),
		strings.NewReader("# version 1\n,1,sha256,"+fixtureSHA25600000000+" conflict\n"),
	)
	if !errors.Is(err, database.ErrConflict) {
		t.Fatal(err)
	}

	assertNoErr(t, raw.QueryRow("SELECT size FROM content").Scan(&size))
	if !size.Valid || size.Int64 != 0 {
		t.Fatal("known zero changed")
	}
}

func TestCleanupFailurePoisonsQueriesUntilRecovery(t *testing.T) {
	db, raw, disk := testDB(t)
	assertNoErr(
		t,
		execSQL(
			raw,
			`CREATE TRIGGER fail_cleanup BEFORE DELETE ON snapshot BEGIN SELECT RAISE(ABORT,'cleanup blocked'); END;`,
		),
	)
	_, err := ImportReader(
		t.Context(),
		db,
		request(disk),
		strings.NewReader(manyFiles(1001, true)+"broken\n"),
	)
	if err == nil || !strings.Contains(err.Error(), "cleanup blocked") ||
		!strings.Contains(err.Error(), "line 1003") {
		t.Fatal(err)
	}

	if _, err := db.LatestCompleteSnapshot(t.Context(), disk); err == nil ||
		errors.Is(err, database.ErrNotFound) {
		t.Fatalf("query not blocked: %v", err)
	}

	assertEqual(t, count(t, raw, "snapshot"), 1)
	assertNoErr(t, execSQL(raw, "DROP TRIGGER fail_cleanup"))
	_, err = ImportReader(t.Context(), db, request(disk), strings.NewReader(",sha256,"+fixtureSHA256AB+" valid\n"))
	assertNoErr(t, err)
	assertEqual(t, count(t, raw, "snapshot"), 1)
	assertEqual(t, count(t, raw, "content"), 1)
}

func execSQL(raw *sql.DB, statement string) error { _, err := raw.Exec(statement); return err }

type probeReader struct {
	probe  func()
	reader io.Reader
	done   bool
}

func (r *probeReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		r.probe()
	}

	return r.reader.Read(p)
}

func TestImportBlocksCatalogQueriesAndSecondImport(t *testing.T) {
	db, _, disk := testDB(t)
	reader := &probeReader{reader: strings.NewReader(",sha256," + fixtureSHA256AB + " a\n"), probe: func() {
		if _, err := db.LatestCompleteSnapshot(t.Context(), disk); !errors.Is(
			err,
			database.ErrBusy,
		) {
			t.Fatalf("query during import: %v", err)
		}

		if _, err := db.CreateDisk("other", "", "", 1); !errors.Is(err, database.ErrBusy) {
			t.Fatalf("disk creation during import: %v", err)
		}
		if _, err := db.Search(t.Context(), base.SearchFilters{
			Query: "a", Field: "name", Match: "exact",
			ContentFilters: base.ContentFilters{
				Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks,
			},
		}, 50, base.SearchAnchor{}, nil); !errors.Is(err, database.ErrBusy) {
			t.Fatalf("search during import: %v", err)
		}

		if _, err := db.ListDiskSnapshots(t.Context(), disk, 50, 0, time.Time{}, nil); !errors.Is(err, database.ErrBusy) {
			t.Fatalf("snapshot listing during import: %v", err)
		}

		if _, err := ImportReader(t.Context(), db, request(disk), strings.NewReader("")); !errors.Is(
			err,
			database.ErrBusy,
		) {
			t.Fatalf("second import: %v", err)
		}
	}}
	_, err := ImportReader(t.Context(), db, request(disk), reader)
	assertNoErr(t, err)
}

func TestImportMetadataAndHashIdentity(t *testing.T) {
	db, raw, disk := testDB(t)
	_, err := ImportReader(
		t.Context(),
		db,
		request(disk),
		strings.NewReader(
			"# version 1\n,,sha256,"+fixtureSHA256AB+" unknown\n"+
				",0,sha256,"+fixtureSHA256CD+" empty\n"+
				"1,4,md5,"+fixtureMD5AB+" known\n,4,md5,"+fixtureMD5AB+" copy\n",
		),
	)
	assertNoErr(t, err)
	assertEqual(t, count(t, raw, "content"), 3)
	assertEqual(t, count(t, raw, "observation"), 4)

	var size sql.NullInt64
	var mtime sql.NullString
	assertNoErr(
		t,
		raw.QueryRow("SELECT size,mtime FROM observation o JOIN content c ON c.id=o.content_id WHERE path='unknown'").
			Scan(&size, &mtime),
	)
	if size.Valid || mtime.Valid {
		t.Fatal("missing metadata was not null")
	}

	assertNoErr(
		t,
		raw.QueryRow("SELECT size,mtime FROM observation o JOIN content c ON c.id=o.content_id WHERE path='empty'").
			Scan(&size, &mtime),
	)
	if !size.Valid || size.Int64 != 0 || mtime.Valid {
		t.Fatal("empty file metadata incorrect")
	}
}

func TestImportPathAndCaptureValidation(t *testing.T) {
	db, _, disk := testDB(t)
	path := filepath.Join(t.TempDir(), "checksums.cshd")
	assertNoErr(t, os.WriteFile(path, []byte(",sha256,"+fixtureSHA256AB+" foo/bar\n"), 0o600))
	_, err := Import(t.Context(), db, Request{DiskID: disk, Path: path})
	if !errors.Is(err, database.ErrValidation) {
		t.Fatal(err)
	}

	result, err := Import(t.Context(), db, Request{DiskID: disk, Path: path, UseSourceMTime: true})
	assertNoErr(t, err)
	assertEqual(t, result.CaptureProvenance, "source_mtime")
	st, err := os.Stat(path)
	assertNoErr(t, err)
	if !result.CapturedAt.Equal(st.ModTime()) {
		t.Fatal("capture time differs from source mtime")
	}

	_, err = Import(
		t.Context(),
		db,
		Request{DiskID: disk, Path: path + ".txt", CapturedAt: time.Now()},
	)
	assertErr(t, err)
	_, err = ImportReader(t.Context(), db, request(404), strings.NewReader(""))
	if !errors.Is(err, database.ErrNotFound) {
		t.Fatal(err)
	}
}
