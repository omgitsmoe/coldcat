package importer

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestDuplicateImportCleanupAndExplicitRepeat(t *testing.T) {
	db, raw, disk := testDB(t)
	input := manyFiles(2005, true)
	req := request(disk)
	first, err := ImportReader(t.Context(), db, req, iotest.OneByteReader(strings.NewReader(input)))
	assertNoErr(t, err)
	digest := newInventoryDigest()
	assertNoErr(t, ParseCshd(strings.NewReader(input), digest.add))
	var stored []byte
	assertNoErr(t, raw.QueryRow("SELECT source_digest FROM snapshot WHERE id=?", first.Id).Scan(&stored))
	expected := digest.sum()
	if !bytes.Equal(stored, expected[:]) {
		t.Fatalf("stored digest: %x, expected %x", stored, expected)
	}
	req.Path = "renamed.cshd"
	req.CapturedAt = req.CapturedAt.In(time.FixedZone("offset", 3600))
	_, err = ImportReader(t.Context(), db, req, strings.NewReader("# comment\n"+input))
	var duplicate *database.DuplicateImportError
	if !errors.As(err, &duplicate) || !errors.Is(err, database.ErrConflict) || duplicate.SnapshotID != first.Id || duplicate.DiskID != disk {
		t.Fatalf("duplicate result: %v", err)
	}
	assertEqual(t, count(t, raw, "snapshot"), 1)
	assertEqual(t, count(t, raw, "observation"), 2005)
	assertEqual(t, count(t, raw, "content"), 2005)
	assertEqual(t, count(t, raw, "pending_size"), 0)
	assertEqual(t, count(t, raw, "import_content"), 0)
	var known int
	assertNoErr(t, raw.QueryRow("SELECT COUNT(*) FROM content WHERE size=4").Scan(&known))
	assertEqual(t, known, 2005)
	req.AllowRepeat = true
	repeated, err := ImportReader(t.Context(), db, req, strings.NewReader(input))
	assertNoErr(t, err)
	if repeated.Id <= first.Id {
		t.Fatal("explicit repeat did not create a new snapshot")
	}
	latest, err := db.LatestCompleteSnapshot(t.Context(), disk)
	assertNoErr(t, err)
	assertEqual(t, latest.Id, repeated.Id)
	req.AllowRepeat = false
	_, err = ImportReader(t.Context(), db, req, strings.NewReader(input))
	if !errors.As(err, &duplicate) || duplicate.SnapshotID != first.Id {
		t.Fatalf("deterministic duplicate after repeat: %v", err)
	}
}

func TestDuplicateIdentityBoundaries(t *testing.T) {
	for _, change := range []string{"disk", "capture time", "metadata", "path", "order"} {
		t.Run(change, func(t *testing.T) {
			db, _, disk := testDB(t)
			req := request(disk)
			input := ",sha256,ab first\n,sha256,cd second\n"
			_, err := ImportReader(t.Context(), db, req, strings.NewReader(input))
			assertNoErr(t, err)
			switch change {
			case "disk":
				id, err := db.CreateDisk("other", "", "", 100)
				assertNoErr(t, err)
				req.DiskID = base.DiskId(id)
			case "capture time":
				req.CapturedAt = req.CapturedAt.Add(time.Nanosecond)
			case "metadata":
				input = "# version 1\n,0,sha256,ab first\n,,sha256,cd second\n"
			case "path":
				input = ",sha256,ab renamed\n,sha256,cd second\n"
			case "order":
				input = ",sha256,cd second\n,sha256,ab first\n"
			}
			_, err = ImportReader(t.Context(), db, req, strings.NewReader(input))
			assertNoErr(t, err)
		})
	}
}

func TestDigestDoesNotDependOnSharedContentEnrichment(t *testing.T) {
	db, raw, disk := testDB(t)
	req := request(disk)
	input := ",sha256,ab file\n"
	first, err := ImportReader(t.Context(), db, req, strings.NewReader(input))
	assertNoErr(t, err)
	later := req
	later.CapturedAt = later.CapturedAt.Add(time.Second)
	_, err = ImportReader(t.Context(), db, later, strings.NewReader("# version 1\n,5,sha256,ab file\n"))
	assertNoErr(t, err)
	_, err = ImportReader(t.Context(), db, req, strings.NewReader(input))
	var duplicate *database.DuplicateImportError
	if !errors.As(err, &duplicate) || duplicate.SnapshotID != first.Id {
		t.Fatalf("enrichment changed duplicate identity: %v", err)
	}
	var size int
	assertNoErr(t, raw.QueryRow("SELECT size FROM content").Scan(&size))
	assertEqual(t, size, 5)
}

func TestIncompleteDigestDoesNotBlockRetry(t *testing.T) {
	db, raw, disk := testDB(t)
	req := request(disk)
	input := manyFiles(1001, true)
	_, err := ImportReader(t.Context(), db, req, strings.NewReader(input+"broken\n"))
	assertErr(t, err)
	assertEqual(t, count(t, raw, "snapshot"), 0)
	digest := newInventoryDigest()
	assertNoErr(t, ParseCshd(strings.NewReader(input), digest.add))
	sum := digest.sum()
	_, err = raw.Exec(`INSERT INTO snapshot(disk_id,state,captured_at,imported_at,capture_provenance,input_format,source_digest)
 VALUES(?,'importing',?,?,'explicit','cshd',?)`, disk, database.FormatTime(req.CapturedAt), database.FormatTime(time.Now()), sum[:])
	assertNoErr(t, err)
	_, err = ImportReader(t.Context(), db, req, strings.NewReader(input))
	assertNoErr(t, err)
	assertEqual(t, count(t, raw, "snapshot"), 1)
	assertEqual(t, count(t, raw, "observation"), 1001)
}
