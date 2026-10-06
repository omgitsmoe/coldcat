package importer

import (
	"strings"
	"testing"
)

func TestDirectoryBuildAndPublicationFailuresPreserveCompletedMetadata(t *testing.T) {
	for _, phase := range []string{"build", "publish", "enrich"} {
		t.Run(phase, func(t *testing.T) {
			db, raw, disk := testDB(t)
			shared := strings.Repeat("ab", 32)
			first, err := ImportReader(t.Context(), db, request(disk),
				strings.NewReader("# version 1\n,,sha256,"+shared+" old/a\n"+
					",,sha256,"+shared+" old/b\n"))
			assertNoErr(t, err)
			trigger := `CREATE TRIGGER fail_directory BEFORE INSERT ON directory
 WHEN NEW.path='tree' BEGIN SELECT RAISE(ABORT,'directory failure'); END;`
			if phase == "publish" {
				trigger = `CREATE TRIGGER fail_directory BEFORE UPDATE ON snapshot
 WHEN NEW.state='complete' BEGIN SELECT RAISE(ABORT,'directory failure'); END;`
			}
			if phase == "enrich" {
				trigger = `CREATE TRIGGER fail_directory BEFORE UPDATE ON directory
 WHEN OLD.snapshot_id=1 BEGIN SELECT RAISE(ABORT,'directory failure'); END;`
			}
			assertNoErr(t, execSQL(raw, trigger))
			_, err = ImportReader(t.Context(), db, request(disk), strings.NewReader(
				"# version 1\n,7,sha256,"+shared+" tree/shared\n"+
					strings.TrimPrefix(manyFiles(2005, true), "# version 1\n")))
			if err == nil || !strings.Contains(err.Error(), "directory failure") {
				t.Fatalf("failure: %v", err)
			}
			for _, table := range []string{
				"directory", "directory_content", "directory_file", "directory_build",
			} {
				var count int
				assertNoErr(t, raw.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE snapshot_id!=?",
					first.Id).Scan(&count))
				assertEqual(t, count, 0)
			}
			detail, err := db.GetDirectory(t.Context(), first.Id, "old")
			assertNoErr(t, err)
			assertEqual(t, detail.KnownBytes, int64(0))
			assertEqual(t, detail.UnknownSizeFileCount, int64(2))
			assertEqual(t, detail.UnknownSizeContentCount, int64(1))
			var unknown bool
			assertNoErr(t, raw.QueryRow(`SELECT size IS NULL FROM content WHERE id=(
 SELECT content_id FROM observation WHERE snapshot_id=? LIMIT 1)`, first.Id).Scan(&unknown))
			assertEqual(t, unknown, true)
			assertNoErr(t, execSQL(raw, "DROP TRIGGER fail_directory"))
			_, err = ImportReader(t.Context(), db, request(disk), strings.NewReader(
				"# version 1\n,7,sha256,"+shared+" tree/shared\n"))
			assertNoErr(t, err)
			detail, err = db.GetDirectory(t.Context(), first.Id, "old")
			assertNoErr(t, err)
			assertEqual(t, detail.KnownBytes, int64(14))
			assertEqual(t, detail.UniqueContentKnownBytes, int64(7))
			assertEqual(t, detail.UnknownSizeFileCount, int64(0))
		})
	}
}

func TestDirectorySizeOverflowRejectsImport(t *testing.T) {
	db, raw, disk := testDB(t)
	_, err := ImportReader(t.Context(), db, request(disk), strings.NewReader(
		"# version 1\n,9223372036854775807,sha256,"+fixtureSHA256AB+" tree/a\n"+
			",9223372036854775807,sha256,"+fixtureSHA256AB+" tree/b\n"))
	if err == nil || !strings.Contains(err.Error(), "build directories") {
		t.Fatalf("overflowing repeated-content size: %v", err)
	}
	for _, table := range []string{"snapshot", "observation", "content", "directory",
		"directory_content", "directory_file", "directory_build"} {
		assertEqual(t, count(t, raw, table), 0)
	}
	state, err := db.GetCatalogState(t.Context())
	assertNoErr(t, err)
	assertEqual(t, int64(state.Revision), int64(0))
}

func TestDirectoryEnrichmentOverflowRollsBack(t *testing.T) {
	db, raw, disk := testDB(t)
	first, err := ImportReader(t.Context(), db, request(disk), strings.NewReader(
		"# version 1\n,4,sha256,"+fixtureSHA256AB+" old/known\n"+
			",,sha256,"+fixtureSHA256CD+" old/unknown\n"))
	assertNoErr(t, err)
	_, err = ImportReader(t.Context(), db, request(disk), strings.NewReader(
		"# version 1\n,9223372036854775807,sha256,"+fixtureSHA256CD+" new/known\n"))
	if err == nil || !strings.Contains(err.Error(), "enrich directories") {
		t.Fatalf("overflow during enrichment: %v", err)
	}
	detail, err := db.GetDirectory(t.Context(), first.Id, "old")
	assertNoErr(t, err)
	assertEqual(t, detail.KnownBytes, int64(4))
	assertEqual(t, detail.UnknownSizeFileCount, int64(1))
	assertEqual(t, detail.UniqueContentKnownBytes, int64(4))
	var unknown bool
	assertNoErr(t, raw.QueryRow(`SELECT size IS NULL FROM content WHERE id=(
 SELECT content_id FROM observation WHERE path='old/unknown')`).Scan(&unknown))
	assertEqual(t, unknown, true)
	assertEqual(t, count(t, raw, "snapshot"), 1)
}
