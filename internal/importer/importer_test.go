package importer

import (
	"crypto"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type contentRow struct {
	Id       base.ContentId
	Size     int64
	HashType string
	Hash     []byte
}

type observationRow struct {
	Path      string
	MTime     string
	ContentId base.ContentId
}

// testDB opens a database in a temporary directory and returns it together
// with the path, so a second connection can read back what was written. The
// sqlite driver is already registered by the database package.
func testDB(t *testing.T) (*database.DB, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "coldcat.sqlite")
	db, err := database.Open(path)
	assertNoErr(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db, path
}

// snapshotId inserts a disk and a snapshot to hang observations off, and
// returns the snapshot id.
func snapshotId(t *testing.T, db *database.DB) int64 {
	t.Helper()

	diskId := addDisk(t, db)

	var id int64
	err := db.Transaction(func(tx *database.Tx) error {
		result, err := tx.Exec(
			"INSERT INTO snapshot(disk_id, created_at) VALUES ($1, $2)",
			diskId, database.FormatTime(time.Unix(1673815645, 0)))
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	assertNoErr(t, err)

	return id
}

func addDisk(t *testing.T, db *database.DB) base.DiskId {
	t.Helper()

	var id base.DiskId
	err := db.Transaction(func(tx *database.Tx) error {
		result, err := tx.Exec(
			"INSERT INTO disk(label, capacity) VALUES ($1, $2)",
			fmt.Sprintf("test disk %d", time.Now().UnixNano()), int64(1<<40))
		if err != nil {
			return err
		}
		rowId, err := result.LastInsertId()
		id = base.DiskId(rowId)
		return err
	})
	assertNoErr(t, err)

	return id
}

func importBatches(t *testing.T, db *database.DB, id int64, batches ...[]File) {
	t.Helper()

	for _, batch := range batches {
		err := db.Transaction(func(tx *database.Tx) error {
			return importBatch(tx, id, batch)
		})
		assertNoErr(t, err)
	}
}

func queryContents(t *testing.T, path string) []contentRow {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	assertNoErr(t, err)
	defer db.Close()

	rows, err := db.Query(
		"SELECT id, size, hash_type, hash FROM content ORDER BY id")
	assertNoErr(t, err)
	defer rows.Close()

	var contents []contentRow
	for rows.Next() {
		var c contentRow
		assertNoErr(t, rows.Scan(&c.Id, &c.Size, &c.HashType, &c.Hash))
		contents = append(contents, c)
	}
	assertNoErr(t, rows.Err())

	return contents
}

func queryObservations(t *testing.T, path string) []observationRow {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	assertNoErr(t, err)
	defer db.Close()

	rows, err := db.Query(
		"SELECT o.path, o.mtime, o.content_id FROM observation o ORDER BY o.id")
	assertNoErr(t, err)
	defer rows.Close()

	var observations []observationRow
	for rows.Next() {
		var o observationRow
		assertNoErr(t, rows.Scan(&o.Path, &o.MTime, &o.ContentId))
		observations = append(observations, o)
	}
	assertNoErr(t, rows.Err())

	return observations
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()

	b, err := hex.DecodeString(s)
	assertNoErr(t, err)
	return b
}

func sha256File(t *testing.T, path string, size uint64, hashHex string, mtime time.Time) File {
	t.Helper()

	return File{
		Name:               filepath.Base(path),
		PathRelativeToRoot: path[:len(path)-len(filepath.Base(path))],
		MTime:              mtime,
		SizeInBytes:        size,
		HashType:           base.HashType{Hash: crypto.SHA256},
		Hash:               mustDecodeHex(t, hashHex),
	}
}

func TestImportBatch(t *testing.T) {
	mtime := time.Unix(1673815645, 797977209)

	tests := []struct {
		name string
		// batches are imported in order, each in its own transaction.
		batches [][]File

		wantContents []contentRow
		// wantContentIds, when set, are compared against the content id
		// of the observation at the same index.
		wantObservations []observationRow
	}{
		{
			name:    "empty batch is a no-op",
			batches: [][]File{{}},
		},
		{
			name: "single file",
			batches: [][]File{{
				sha256File(t, "foo/bar.txt", 1337, "deadbeef", mtime),
			}},
			wantContents: []contentRow{
				{Id: 1, Size: 1337, HashType: "sha256", Hash: mustDecodeHex(t, "deadbeef")},
			},
			wantObservations: []observationRow{
				{
					Path:      "foo/bar.txt",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
			},
		},
		{
			name: "path is rejoined from directory and name",
			batches: [][]File{{
				sha256File(t, "bar foo/bar/baz xer/file.txt", 0, "deadbeef", time.Time{}),
			}},
			wantContents: []contentRow{
				{Id: 1, Size: 0, HashType: "sha256", Hash: mustDecodeHex(t, "deadbeef")},
			},
			wantObservations: []observationRow{
				{Path: "bar foo/bar/baz xer/file.txt", MTime: "0001-01-01T00:00:00Z", ContentId: 1},
			},
		},
		{
			name: "distinct contents get distinct rows and observations",
			batches: [][]File{{
				sha256File(t, "foo/a", 1, "deadbeef", mtime),
				sha256File(t, "foo/b", 2, "beefdead", mtime),
			}},
			wantContents: []contentRow{
				{Id: 1, Size: 1, HashType: "sha256", Hash: mustDecodeHex(t, "deadbeef")},
				{Id: 2, Size: 2, HashType: "sha256", Hash: mustDecodeHex(t, "beefdead")},
			},
			wantObservations: []observationRow{
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
				{
					Path:      "foo/b",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 2,
				},
			},
		},
		{
			// A repeated hash in one batch must not conflict with the
			// batch's own multi-row insert, and both files are still
			// observed.
			name: "duplicate hash within a batch",
			batches: [][]File{{
				sha256File(t, "foo/a", 1, "deadbeef", mtime),
				sha256File(t, "foo/b", 2, "deadbeef", mtime),
			}},
			wantContents: []contentRow{
				{Id: 1, Size: 1, HashType: "sha256", Hash: mustDecodeHex(t, "deadbeef")},
			},
			wantObservations: []observationRow{
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
				{
					Path:      "foo/b",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
			},
		},
		{
			// Deduplication is per content, not per file: a file
			// repeated several times still gets one observation per
			// occurrence.
			name: "a file repeated within a batch",
			batches: [][]File{{
				sha256File(t, "foo/a", 1, "deadbeef", mtime),
				sha256File(t, "foo/a", 1, "deadbeef", mtime),
				sha256File(t, "foo/a", 1, "deadbeef", mtime),
			}},
			wantContents: []contentRow{
				{Id: 1, Size: 1, HashType: "sha256", Hash: mustDecodeHex(t, "deadbeef")},
			},
			wantObservations: []observationRow{
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
			},
		},
		{
			// The same content in a later batch must reuse the row
			// created by the earlier batch.
			name: "content is shared across batches",
			batches: [][]File{
				{sha256File(t, "foo/a", 1, "deadbeef", mtime)},
				{
					sha256File(t, "foo/b", 2, "beefdead", mtime),
					sha256File(t, "foo/c", 1, "deadbeef", mtime),
				},
			},
			wantContents: []contentRow{
				{Id: 1, Size: 1, HashType: "sha256", Hash: mustDecodeHex(t, "deadbeef")},
				{Id: 2, Size: 2, HashType: "sha256", Hash: mustDecodeHex(t, "beefdead")},
			},
			wantObservations: []observationRow{
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
				{
					Path:      "foo/b",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 2,
				},
				{
					Path:      "foo/c",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
			},
		},
		{
			// A v0 line reports no size, so the content is stored with
			// size 0 until a v1 import fills it in.
			name: "a known size is filled in by a later import",
			batches: [][]File{
				{sha256File(t, "foo/a", 0, "deadbeef", mtime)},
				{sha256File(t, "foo/a", 4096, "deadbeef", mtime)},
			},
			wantContents: []contentRow{
				{Id: 1, Size: 4096, HashType: "sha256", Hash: mustDecodeHex(t, "deadbeef")},
			},
			wantObservations: []observationRow{
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
			},
		},
		{
			name: "a size-less import does not blank out a known size",
			batches: [][]File{
				{sha256File(t, "foo/a", 4096, "deadbeef", mtime)},
				{sha256File(t, "foo/a", 0, "deadbeef", mtime)},
			},
			wantContents: []contentRow{
				{Id: 1, Size: 4096, HashType: "sha256", Hash: mustDecodeHex(t, "deadbeef")},
			},
			wantObservations: []observationRow{
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
				{
					Path:      "foo/a",
					MTime:     "2023-01-15T20:47:25.797977209Z",
					ContentId: 1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, path := testDB(t)
			id := snapshotId(t, db)

			importBatches(t, db, id, tt.batches...)

			contents := queryContents(t, path)
			if len(contents) != len(tt.wantContents) {
				t.Fatalf("got %d contents, want %d", len(contents), len(tt.wantContents))
			}
			for i, want := range tt.wantContents {
				got := contents[i]
				assertEqual(t, got.Id, want.Id)
				assertEqual(t, got.Size, want.Size)
				assertEqual(t, got.HashType, want.HashType)
				assertSliceEqual(t, got.Hash, want.Hash)
			}

			observations := queryObservations(t, path)
			if len(observations) != len(tt.wantObservations) {
				t.Fatalf("got %d observations, want %d",
					len(observations), len(tt.wantObservations))
			}
			for i, want := range tt.wantObservations {
				got := observations[i]
				assertEqual(t, got.Path, want.Path)
				assertEqual(t, got.MTime, want.MTime)
				assertEqual(t, got.ContentId, want.ContentId)
			}
		})
	}
}

func TestImportBatchIdentifiesContentByHashTypeAndHash(t *testing.T) {
	hash := mustDecodeHex(t, "deadbeef")
	mtime := time.Unix(1673815645, 797977209)

	// The same bytes under two hash types are two contents, so the second
	// file must not be folded into the first one's row.
	sha256 := File{
		Name: "a", MTime: mtime, SizeInBytes: 10,
		HashType: base.HashType{Hash: crypto.SHA256}, Hash: hash,
	}
	md5 := File{
		Name: "b", MTime: mtime, SizeInBytes: 20,
		HashType: base.HashType{Hash: crypto.MD5}, Hash: hash,
	}

	tests := []struct {
		name  string
		files []File
	}{
		{name: "distinct hash types", files: []File{sha256, md5}},
		{name: "same hash type", files: []File{sha256, sha256}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, path := testDB(t)
			id := snapshotId(t, db)
			importBatches(t, db, id, tt.files)

			contents := queryContents(t, path)
			observations := queryObservations(t, path)
			assertEqual(t, len(observations), len(tt.files))

			// every file must resolve to a content row, and distinct
			// hash types must not share one
			byType := map[string]base.ContentId{}
			for _, c := range contents {
				if other, dup := byType[c.HashType]; dup {
					t.Fatalf("hash type %q reused content id %d and %d",
						c.HashType, other, c.Id)
				}
				byType[c.HashType] = c.Id
			}

			for i, o := range observations {
				want := byType[hashTypeOf(t, tt.files[i])]
				assertEqual(t, o.ContentId, want)
			}
		})
	}
}

func hashTypeOf(t *testing.T, f File) string {
	t.Helper()

	id, err := f.HashType.ToIdentifier()
	assertNoErr(t, err)
	return id
}

func TestImportChecksExtension(t *testing.T) {
	// filepath.Ext keeps the leading dot, so the lookup must not: without
	// trimming it, Import rejects every path it supports.
	write := func(name string) string {
		path := filepath.Join(t.TempDir(), name)
		assertNoErr(t, os.WriteFile(path, []byte(",sha256,deadbeef foo/bar\n"), 0o644))
		return path
	}

	db, _ := testDB(t)
	diskId := addDisk(t, db)
	assertNoErr(t, Import(db, diskId, write("checksums.cshd")))

	assertErr(t, Import(db, diskId, write("checksums.txt")))
	assertErr(t, Import(db, diskId, write("checksums")))
}

func TestImportRejectsUnknownDisk(t *testing.T) {
	// Foreign keys are enforced, so importing for a disk that does not
	// exist must fail instead of writing a dangling snapshot.
	db, _ := testDB(t)

	path := filepath.Join(t.TempDir(), "checksums.cshd")
	assertNoErr(t, os.WriteFile(
		path, []byte(",sha256,deadbeef foo/bar\n"), 0o644))

	assertErr(t, Import(db, 404, path))
}

func TestImportBatchRollsBackOnError(t *testing.T) {
	db, path := testDB(t)
	id := snapshotId(t, db)

	unsupported := File{
		Name:               "foo",
		PathRelativeToRoot: "",
		HashType:           base.HashType{Hash: crypto.SHA224},
		Hash:               mustDecodeHex(t, "deadbeef"),
	}

	err := db.Transaction(func(tx *database.Tx) error {
		return importBatch(tx, id, []File{
			sha256File(t, "ok", 1, "cafebabe", time.Unix(1, 0)),
			unsupported,
		})
	})
	assertErr(t, err)

	// The whole batch, including the file that would have imported fine, is
	// rolled back.
	if contents := queryContents(t, path); len(contents) != 0 {
		t.Fatalf("got %d contents, want 0", len(contents))
	}
	if observations := queryObservations(t, path); len(observations) != 0 {
		t.Fatalf("got %d observations, want 0", len(observations))
	}
}
