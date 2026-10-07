package importer

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func BenchmarkImportPhases(b *testing.B) {
	const files = 10000
	var input strings.Builder
	input.WriteString("# version 1\n")
	for i := range files {
		fmt.Fprintf(&input,
			"1700000000,4096,sha256,%064x archive/disk-%02d/year-%02d/month-%02d/report-%05d.txt\n",
			i%7500, i%4, i%10, i%12, i)
	}
	inventory := input.String()
	for _, batchSize := range []int{1000, 5000, 10000} {
		b.Run(fmt.Sprintf("batch_%d", batchSize), func(b *testing.B) {
			b.ReportAllocs()
			var ingestion, directories, publishing time.Duration
			for b.Loop() {
				b.StopTimer()
				db, err := database.Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
				if err != nil {
					b.Fatal(err)
				}
				disk, err := db.CreateDisk("disk", "", "", 0)
				if err != nil {
					b.Fatal(err)
				}
				var directoriesAt, publishingAt time.Time
				started := time.Now()
				b.StartTimer()
				_, err = importReader(b.Context(), db, Request{
					DiskID: base.DiskId(disk), CapturedAt: time.Unix(10, 0),
					Progress: func(p Progress) error {
						switch p.Phase {
						case ProgressDirectories:
							directoriesAt = time.Now()
						case ProgressPublishing:
							publishingAt = time.Now()
						}
						return nil
					},
				}, strings.NewReader(inventory), batchSize)
				finished := time.Now()
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				ingestion += directoriesAt.Sub(started)
				directories += publishingAt.Sub(directoriesAt)
				publishing += finished.Sub(publishingAt)
				if err := db.Close(); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			b.ReportMetric(ingestion.Seconds()/float64(b.N), "ingestion-s/op")
			b.ReportMetric(directories.Seconds()/float64(b.N), "directories-s/op")
			b.ReportMetric(publishing.Seconds()/float64(b.N), "publishing-s/op")
		})
	}
}
