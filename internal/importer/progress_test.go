package importer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestProgressTracksCommittedBatches(t *testing.T) {
	for _, n := range []int{0, 1000, 2501} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			db, raw, disk := testDB(t)
			req := request(disk)
			var events []Progress
			req.Progress = func(p Progress) error {
				events = append(events, p)
				assertEqual(t, int64(count(t, raw, "observation")), p.CommittedFiles)
				var state string
				assertNoErr(t, raw.QueryRow("SELECT state FROM snapshot WHERE id=?", p.SnapshotID).
					Scan(&state))
				assertEqual(t, state, "importing")
				return nil
			}
			result, err := ImportReader(t.Context(), db, req, strings.NewReader(manyFiles(n, true)))
			assertNoErr(t, err)
			assertEqual(t, result.FileCount, int64(n))
			assertEqual(t, len(events), (n+999)/1000+1)
			for i, event := range events[:len(events)-1] {
				want := min(int64((i+1)*1000), int64(n))
				assertEqual(t, event.CommittedFiles, want)
				assertEqual(t, event.Phase, ProgressImporting)
			}
			last := events[len(events)-1]
			assertEqual(t, last.Phase, ProgressPublishing)
			assertEqual(t, last.CommittedFiles, int64(n))
			assertEqual(t, last.StreamingComplete, true)
		})
	}
}

func TestProgressFailuresCleanImports(t *testing.T) {
	for _, mode := range []string{"batch", "publishing", "cancel", "parse", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			db, raw, disk := testDB(t)
			_, err := ImportReader(t.Context(), db, request(disk),
				strings.NewReader(",sha256,"+fixtureSHA25600000000+" baseline\n"))
			assertNoErr(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req := request(disk)
			failure := errors.New("progress sink failed")
			var events []Progress
			req.Progress = func(p Progress) error {
				events = append(events, p)
				if mode == "batch" || mode == "publishing" && p.Phase == ProgressPublishing {
					return failure
				}
				if mode == "cancel" {
					cancel()
				}
				return nil
			}
			input := manyFiles(1001, true)
			if mode == "parse" {
				input += "broken\n"
			}
			if mode == "rollback" {
				input = manyFiles(999, true) + ",4,sha256," + fixtureSHA25600000000 + " tree/file-0\n"
			}
			_, err = ImportReader(ctx, db, req, strings.NewReader(input))
			assertErr(t, err)
			if mode == "batch" || mode == "publishing" {
				if !errors.Is(err, failure) {
					t.Fatalf("lost callback error: %v", err)
				}
			}
			if mode == "rollback" {
				assertEqual(t, len(events), 0)
			}
			if mode == "parse" {
				assertEqual(t, len(events), 1)
				assertEqual(t, events[0].Phase, ProgressImporting)
			}
			for _, table := range []string{"snapshot", "observation", "content"} {
				assertEqual(t, count(t, raw, table), 1)
			}
			for _, table := range []string{"pending_size", "import_content"} {
				assertEqual(t, count(t, raw, table), 0)
			}
			var size any
			assertNoErr(t, raw.QueryRow("SELECT size FROM content").Scan(&size))
			if size != nil {
				t.Fatalf("failed import enriched shared content: %v", size)
			}
		})
	}
}
