//go:build transactiontiming

package importer

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type importTransactionTiming struct {
	duration time.Duration
	err      error
}

func assertImportTransactionTimings(
	tb testing.TB, timings []importTransactionTiming, files int, fail bool,
) {
	tb.Helper()
	want := 1 + files/defaultBatchSize + 2
	if fail {
		want-- // Late parse failure replaces directory building/publication with cleanup.
	}
	if len(timings) != want {
		tb.Fatalf("transactions: %d, want %d", len(timings), want)
	}
	for i, timing := range timings {
		if timing.duration <= 0 || timing.err != nil {
			tb.Fatalf("transaction %d: %+v", i, timing)
		}
	}
}

func timedIndexedImport(
	tb testing.TB, files int, fail bool, input string,
) ([]importTransactionTiming, time.Duration) {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "catalog.sqlite")
	db, err := database.OpenContext(tb.Context(), path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	disk, err := db.CreateDisk("disk", "", "", 0)
	if err != nil {
		tb.Fatal(err)
	}
	var timings []importTransactionTiming
	db.SetTransactionObserverForBenchmark(func(duration time.Duration, err error) {
		timings = append(timings, importTransactionTiming{duration, err})
	})
	if b, ok := tb.(*testing.B); ok {
		b.StartTimer()
	}
	started := time.Now()
	result, importErr := Import(tb.Context(), db, Request{
		DiskID: base.DiskId(disk), Path: input, CapturedAt: time.Unix(10, 0),
		Progress: func(p Progress) error {
			want := 1 + int(p.CommittedFiles)/defaultBatchSize
			if p.Phase == ProgressPublishing {
				want++
			}
			if len(timings) != want {
				return fmt.Errorf("%s transaction boundary: %d, want %d", p.Phase, len(timings), want)
			}
			return nil
		},
	})
	elapsed := time.Since(started)
	if b, ok := tb.(*testing.B); ok {
		b.StopTimer()
	}
	db.SetTransactionObserverForBenchmark(nil)
	assertImportTransactionTimings(tb, timings, files, fail)
	assertIndexedImport(tb, db, path, files, fail, result, importErr)
	if err := db.Close(); err != nil {
		tb.Fatal(err)
	}
	return timings, elapsed
}

func TestImportTransactionTimingFixture(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("late_failure_%t", fail), func(t *testing.T) {
			const files = 2 * defaultBatchSize
			timedIndexedImport(t, files, fail, writeIndexedImportFixture(t, files, fail))
		})
	}
}

func TestTransactionTimingRollback(t *testing.T) {
	db, err := database.OpenContext(t.Context(), filepath.Join(t.TempDir(), "catalog.sqlite"))
	assertNoErr(t, err)
	t.Cleanup(func() { db.Close() })
	want := errors.New("rollback fixture")
	var timings []importTransactionTiming
	db.SetTransactionObserverForBenchmark(func(duration time.Duration, err error) {
		timings = append(timings, importTransactionTiming{duration, err})
	})
	err = db.TransactionContext(t.Context(), func(tx *database.Tx) error {
		time.Sleep(10 * time.Millisecond)
		_, err := tx.ExecContext(t.Context(), "INSERT INTO disk(label,capacity) VALUES('rolled-back',0)")
		if err != nil {
			return err
		}
		return want
	})
	db.SetTransactionObserverForBenchmark(nil)
	if !errors.Is(err, want) || len(timings) != 1 ||
		timings[0].duration < 10*time.Millisecond || !errors.Is(timings[0].err, want) {
		t.Fatalf("rollback timing: %v, %+v", err, timings)
	}
	assertNoErr(t, db.TransactionContext(t.Context(), func(tx *database.Tx) error {
		var count int
		if err := tx.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM disk").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("rollback not complete: %d disks", count)
		}
		return nil
	}))
}

func BenchmarkImportTransactions(b *testing.B) {
	for _, files := range []int{50000, 1000000} {
		for _, fail := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/late_failure_%t", files, fail), func(b *testing.B) {
				input := writeIndexedImportFixture(b, files, fail)
				var total, transactionTotal, setup, batches, batchMax time.Duration
				var directories, publishing, cleanup time.Duration
				var count int
				for b.Loop() {
					b.StopTimer()
					timings, elapsed := timedIndexedImport(b, files, fail, input)
					total += elapsed
					count += len(timings)
					for _, timing := range timings {
						transactionTotal += timing.duration
					}
					setup += timings[0].duration
					batchCount := files / defaultBatchSize
					for _, timing := range timings[1 : 1+batchCount] {
						batches += timing.duration
						batchMax = max(batchMax, timing.duration)
					}
					if fail {
						cleanup += timings[len(timings)-1].duration
					} else {
						directories += timings[len(timings)-2].duration
						publishing += timings[len(timings)-1].duration
					}
					b.StartTimer()
				}
				b.ReportMetric(total.Seconds()/float64(b.N), "import-s/op")
				b.ReportMetric(float64(count)/float64(b.N), "transactions/op")
				b.ReportMetric(transactionTotal.Seconds()/float64(b.N), "transactions-s/op")
				b.ReportMetric(setup.Seconds()/float64(b.N), "setup-tx-s/op")
				b.ReportMetric(batches.Seconds()/float64(b.N), "batch-tx-s/op")
				b.ReportMetric(batchMax.Seconds(), "batch-tx-max-s")
				if fail {
					b.ReportMetric(cleanup.Seconds()/float64(b.N), "cleanup-tx-s/op")
				} else {
					b.ReportMetric(directories.Seconds()/float64(b.N), "directories-tx-s/op")
					b.ReportMetric(publishing.Seconds()/float64(b.N), "publishing-tx-s/op")
				}
			})
		}
	}
}
