package importer

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestObservationBatchBoundaries(t *testing.T) {
	for _, files := range []int{
		observationBatchSize - 1, observationBatchSize, observationBatchSize + 1,
		defaultBatchSize + 1,
	} {
		t.Run(fmt.Sprint(files), func(t *testing.T) {
			db, raw, disk := testDB(t)
			result, err := ImportReader(t.Context(), db, request(disk),
				strings.NewReader(manyFiles(files, true)))
			assertNoErr(t, err)
			assertEqual(t, result.FileCount, int64(files))
			assertEqual(t, result.ContentCount, int64(files))
			assertEqual(t, count(t, raw, "observation"), files)
			assertEqual(t, count(t, raw, "search_path"), files)
			assertEqual(t, count(t, raw, "directory_file"), files)
			assertNoErr(t, execSQL(raw,
				`INSERT INTO search_trigram(search_trigram,rank) VALUES('integrity-check',1)`))
		})
	}
}

func TestObservationSQLBatchesStayWithinOneLargerTransaction(t *testing.T) {
	db, raw, disk := testDB(t)
	const files = observationBatchSize + 1
	req := request(disk)
	commits := 0
	req.Progress = func(p Progress) error {
		if p.Phase == ProgressImporting {
			commits++
			assertEqual(t, p.CommittedFiles, int64(files))
			assertEqual(t, count(t, raw, "observation"), files)
		}
		return nil
	}
	result, err := importReader(t.Context(), db, req,
		strings.NewReader(manyFiles(files, true)), 2*defaultBatchSize)
	assertNoErr(t, err)
	assertEqual(t, commits, 1)
	assertEqual(t, result.FileCount, int64(files))
	assertNoErr(t, execSQL(raw,
		`INSERT INTO search_trigram(search_trigram,rank) VALUES('integrity-check',1)`))
}

func TestObservationBatchFailureRollsBackAndCleansEarlierCommits(t *testing.T) {
	for _, duplicateAt := range []int{
		observationBatchSize - 1, observationBatchSize, defaultBatchSize + 1,
	} {
		t.Run(fmt.Sprint(duplicateAt), func(t *testing.T) {
			db, raw, disk := testDB(t)
			input := manyFiles(duplicateAt, true) +
				",4,sha256," + fixtureSHA25600000000 + " tree/file-0\n"
			_, err := ImportReader(t.Context(), db, request(disk), strings.NewReader(input))
			if !errors.Is(err, database.ErrConflict) ||
				!strings.Contains(err.Error(), "observe ") {
				t.Fatalf("observation conflict: %v", err)
			}
			for _, table := range []string{
				"snapshot", "observation", "content", "pending_size", "import_content",
				"search_path", "search_trigram", "directory", "directory_file",
			} {
				assertEqual(t, count(t, raw, table), 0)
			}
		})
	}
}

func TestObservationBatchPreservesNullableMetadata(t *testing.T) {
	db, raw, disk := testDB(t)
	const files = observationBatchSize + 1
	var input strings.Builder
	input.WriteString("# version 1\n")
	for i := range files {
		mtime, size := "", ""
		if i%2 != 0 {
			mtime = "1700000000"
		}
		if i%3 != 0 {
			size = fmt.Sprint(i)
		}
		fmt.Fprintf(&input, "%s,%s,sha256,%064x tree/file-%d\n", mtime, size, i, i)
	}
	result, err := ImportReader(t.Context(), db, request(disk), strings.NewReader(input.String()))
	assertNoErr(t, err)

	rows, err := raw.Query(`SELECT o.path,o.mtime,c.size FROM observation o
 JOIN content c ON c.id=o.content_id WHERE o.snapshot_id=? ORDER BY o.id`, result.Id)
	assertNoErr(t, err)
	defer rows.Close()
	i := 0
	for rows.Next() {
		var path string
		var mtime sql.NullString
		var size sql.NullInt64
		assertNoErr(t, rows.Scan(&path, &mtime, &size))
		assertEqual(t, path, fmt.Sprintf("tree/file-%d", i))
		assertEqual(t, mtime.Valid, i%2 != 0)
		if mtime.Valid {
			assertEqual(t, mtime.String, database.FormatTime(time.Unix(1700000000, 0)))
		}
		assertEqual(t, size.Valid, i%3 != 0)
		if size.Valid {
			assertEqual(t, size.Int64, int64(i))
		}
		i++
	}
	assertNoErr(t, rows.Err())
	assertEqual(t, i, files)
}
