package importer

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestParserAlgorithmHashLengths(t *testing.T) {
	for _, algorithm := range []struct {
		name string
		size int
	}{
		{"md4", 16}, {"md5", 16}, {"sha1", 20}, {"sha256", 32}, {"sha384", 48},
		{"sha512", 64}, {"sha3_224", 28}, {"sha3_256", 32}, {"sha3_384", 48},
		{"sha3_512", 64},
	} {
		for _, version := range []int{0, 1} {
			for _, delta := range []int{-1, 0, 1} {
				t.Run(fmt.Sprintf("%s/v%d/delta%d", algorithm.name, version, delta), func(t *testing.T) {
					size := algorithm.size + delta
					hash := "00" + strings.Repeat("AB", size-1)
					prefix := ","
					if version == 1 {
						prefix = ",,"
					}
					input := fmt.Sprintf("# version %d\n%s%s,%s dir/file\n",
						version, prefix, algorithm.name, hash)
					called := false
					err := ParseCshd(strings.NewReader(input), func(file File) error {
						called = true
						if !bytes.Equal(file.Hash, fixtureHashBytes(t, strings.ToLower(hash))) {
							t.Fatalf("decoded hash changed: %x", file.Hash)
						}
						id, err := file.HashType.ToIdentifier()
						if err != nil || id != algorithm.name {
							t.Fatalf("algorithm: %s %v", id, err)
						}
						return nil
					})
					if delta == 0 {
						assertNoErr(t, err)
						if !called {
							t.Fatal("valid record did not reach callback")
						}
						return
					}
					if !errors.Is(err, database.ErrValidation) || called {
						t.Fatalf("invalid record: callback=%v error=%v", called, err)
					}
					h, errType := base.FromIdentifier(algorithm.name)
					assertNoErr(t, errType)
					for _, detail := range []string{
						"line 2", `path "dir/file"`, h.Hash.String(),
						fmt.Sprintf("must be %d bytes, got %d", algorithm.size, size),
					} {
						if !strings.Contains(err.Error(), detail) {
							t.Fatalf("missing %q: %v", detail, err)
						}
					}
				})
			}
		}
	}
}

func TestParserRejectsMalformedHashEncoding(t *testing.T) {
	for _, hash := range []string{"", "a", "xyz", " "} {
		t.Run(fmt.Sprintf("%q", hash), func(t *testing.T) {
			called := false
			err := ParseCshd(strings.NewReader(",sha256,"+hash+" file"), func(File) error {
				called = true
				return nil
			})
			if !errors.Is(err, database.ErrValidation) || called {
				t.Fatalf("callback=%v error=%v", called, err)
			}
		})
	}
}

func TestBatchRejectsInvalidHashIdentity(t *testing.T) {
	for _, hashType := range []crypto.Hash{crypto.SHA256, crypto.SHA224, 0, 999} {
		t.Run(fmt.Sprint(hashType), func(t *testing.T) {
			db, raw, _ := testDB(t)
			assertNoErr(t, execSQL(raw, `INSERT INTO snapshot(
				id,disk_id,state,captured_at,imported_at,capture_provenance,input_format
			) VALUES(1,1,'importing','2023-01-01','2023-01-01','explicit','cshd')`))
			err := db.TransactionContext(t.Context(), func(tx *database.Tx) error {
				return importBatch(t.Context(), tx, 1, []File{{
					Name: "file", HashType: base.HashType{Hash: hashType}, Hash: []byte{0xab},
				}})
			})
			if !errors.Is(err, database.ErrValidation) || !strings.Contains(err.Error(), `file "file"`) {
				t.Fatalf("batch error: %v", err)
			}
			for _, table := range []string{"content", "observation", "import_content", "pending_size"} {
				assertEqual(t, count(t, raw, table), 0)
			}
		})
	}
}

func TestInvalidHashLengthCleansImportAndPreservesMetadata(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late=%v", late), func(t *testing.T) {
			db, raw, disk := testDB(t)
			completed, err := ImportReader(t.Context(), db, request(disk),
				strings.NewReader(",sha256,"+fixtureSHA25600000000+" original\n"))
			assertNoErr(t, err)
			input := "# version 1\n"
			if late {
				input = manyFiles(1001, true)
			}
			input += ",4,sha256,ab invalid\n"
			committed := int64(0)
			req := request(disk)
			req.Progress = func(progress Progress) error {
				committed = progress.CommittedFiles
				assertEqual(t, count(t, raw, "observation"), 1001)
				assertEqual(t, count(t, raw, "pending_size"), 1000)
				return nil
			}
			_, err = ImportReader(t.Context(), db, req, strings.NewReader(input))
			if !errors.Is(err, database.ErrValidation) ||
				!strings.Contains(err.Error(), `path "invalid"`) {
				t.Fatalf("import error: %v", err)
			}
			wantCommitted := int64(0)
			if late {
				wantCommitted = 1000
			}
			assertEqual(t, committed, wantCommitted)
			for _, table := range []string{"snapshot", "content", "observation"} {
				assertEqual(t, count(t, raw, table), 1)
			}
			for _, table := range []string{"pending_size", "import_content"} {
				assertEqual(t, count(t, raw, table), 0)
			}
			var unknown int
			assertNoErr(t, raw.QueryRow("SELECT COUNT(*) FROM content WHERE size IS NULL").Scan(&unknown))
			assertEqual(t, unknown, 1)
			latest, err := db.LatestCompleteSnapshot(t.Context(), disk)
			assertNoErr(t, err)
			assertEqual(t, latest.Id, completed.Id)
		})
	}
}
