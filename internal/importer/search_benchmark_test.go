package importer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func BenchmarkSearchIndexedImport(b *testing.B) {
	const files = 50000
	for _, fail := range []bool{false, true} {
		b.Run(fmt.Sprintf("late_failure_%t", fail), func(b *testing.B) {
			inputPath := filepath.Join(b.TempDir(), "input.cshd")
			input, err := os.Create(inputPath)
			if err != nil {
				b.Fatal(err)
			}
			writer := bufio.NewWriter(input)
			for i := range files {
				if _, err := fmt.Fprintf(writer, ",sha256,%064x archive/report-%05d.txt\n", i, i); err != nil {
					b.Fatal(err)
				}
			}
			if fail {
				if _, err := fmt.Fprintln(writer, "broken"); err != nil {
					b.Fatal(err)
				}
			}
			if err := writer.Flush(); err != nil {
				b.Fatal(err)
			}
			if err := input.Close(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			var elapsed time.Duration
			var peak uint64
			for b.Loop() {
				db, err := database.Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
				if err != nil {
					b.Fatal(err)
				}
				disk, err := db.CreateDisk("disk", "", "", 0)
				if err != nil {
					b.Fatal(err)
				}
				started := time.Now()
				_, err = Import(b.Context(), db, Request{
					DiskID: base.DiskId(disk), Path: inputPath, CapturedAt: time.Unix(10, 0),
					Progress: func(Progress) error {
						var memory runtime.MemStats
						runtime.ReadMemStats(&memory)
						peak = max(peak, memory.HeapAlloc)
						return nil
					},
				})
				elapsed += time.Since(started)
				if (err != nil) != fail {
					b.Fatalf("import: %v", err)
				}
				if fail {
					state, accessErr := db.GetCatalogState(b.Context())
					if accessErr != nil || state.Revision != 0 {
						b.Fatalf("cleanup did not finish: import %v; state %+v: %v", err, state, accessErr)
					}
				}
				if err := db.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(files*b.N)/elapsed.Seconds(), "files/s")
			b.ReportMetric(float64(peak), "sampled-heap-bytes")
		})
	}
}
