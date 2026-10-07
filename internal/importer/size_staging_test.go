package importer

import (
	"strings"
	"testing"
)

func TestSizeStagingOnlyForUnknownContent(t *testing.T) {
	for _, known := range []bool{false, true} {
		name := "unknown"
		if known {
			name = "known"
		}
		t.Run(name, func(t *testing.T) {
			db, raw, disk := testDB(t)
			baseline := ",sha256," + fixtureSHA256AB + " original\n"
			if known {
				baseline = "# version 1\n,4,sha256," + fixtureSHA256AB + " original\n"
			}
			_, err := ImportReader(t.Context(), db, request(disk), strings.NewReader(baseline))
			assertNoErr(t, err)

			req := request(disk)
			seen := false
			req.Progress = func(p Progress) error {
				if p.Phase == ProgressDirectories {
					seen = true
					want := 1
					if known {
						want = 0
					}
					assertEqual(t, count(t, raw, "pending_size"), want)
				}
				return nil
			}
			input := "# version 1\n,4,sha256," + fixtureSHA256AB + " a\n" +
				",4,sha256," + fixtureSHA256AB + " b\n"
			result, err := ImportReader(t.Context(), db, req, strings.NewReader(input))
			assertNoErr(t, err)
			assertEqual(t, seen, true)
			assertEqual(t, result.FileCount, int64(2))
			assertEqual(t, result.ContentCount, int64(1))
			assertEqual(t, count(t, raw, "pending_size"), 0)
			var size int
			assertNoErr(t, raw.QueryRow("SELECT size FROM content").Scan(&size))
			assertEqual(t, size, 4)
		})
	}
}
