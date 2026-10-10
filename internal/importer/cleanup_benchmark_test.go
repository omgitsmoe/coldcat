package importer

import (
	"context"
	"fmt"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/database"
)

func BenchmarkDistributionCleanup(b *testing.B) {
	for _, files := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(files), func(b *testing.B) {
			d := importDistributions(files)[1]
			baseline := distributionBaseline(b, d)
			input := writeDistributionInput(b, d, d.history, true)
			for b.Loop() {
				b.StopTimer()
				c := openDistributionCatalog(b, d, baseline)
				_, err := c.raw.Exec(`CREATE TRIGGER hold_failed_import BEFORE DELETE ON observation
 BEGIN SELECT RAISE(ABORT,'profile fixture held'); END`)
				if err != nil {
					b.Fatal(err)
				}
				_, err = Import(b.Context(), c.db, Request{
					DiskID: c.disk, Path: input, CapturedAt: time.Unix(10, 0),
				})
				if err == nil || !strings.Contains(err.Error(), "profile fixture held") ||
					!strings.Contains(err.Error(), fmt.Sprintf("line %d", files+2)) {
					b.Fatalf("fixture did not retain failed import: %v", err)
				}
				if _, err := c.raw.Exec("DROP TRIGGER hold_failed_import"); err != nil {
					b.Fatal(err)
				}
				id := distributionCount(b, c.raw, "SELECT id FROM snapshot WHERE state='importing'")
				if got := distributionCount(b, c.raw,
					"SELECT COUNT(*) FROM observation WHERE snapshot_id=?", id); got != int64(files) {
					b.Fatalf("failed fixture observations: %d, want %d", got, files)
				}
				b.StartTimer()
				pprof.Do(b.Context(), pprof.Labels("phase", "cleanup"), func(ctx context.Context) {
					if err := c.db.CleanupImport(ctx, id); err != nil {
						b.Fatal(err)
					}
				})
				b.StopTimer()
				if err := c.db.Close(); err != nil {
					b.Fatal(err)
				}
				c.db, err = database.OpenContext(b.Context(), c.path)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { c.db.Close() })
				assertDistributionImport(b, d, c, c.completed[0],
					fmt.Errorf("line %d", files+2), "parse")
				b.StartTimer()
			}
		})
	}
}
