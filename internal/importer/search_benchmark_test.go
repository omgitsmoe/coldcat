package importer

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

const importHeapSampleInterval = 10 * time.Millisecond

type importHeapSample struct {
	peak    uint64
	samples int
}

func startImportHeapSampler() func() importHeapSample {
	stop := make(chan struct{})
	done := make(chan importHeapSample)
	var initial runtime.MemStats
	runtime.ReadMemStats(&initial)
	go func() {
		ticker := time.NewTicker(importHeapSampleInterval)
		defer ticker.Stop()
		result := importHeapSample{peak: initial.HeapAlloc, samples: 1}
		sample := func() {
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			result.peak = max(result.peak, memory.HeapAlloc)
			result.samples++
		}
		for {
			select {
			case <-ticker.C:
				sample()
			case <-stop:
				sample()
				done <- result
				return
			}
		}
	}()
	return func() importHeapSample {
		close(stop)
		return <-done
	}
}

func writeIndexedImportFixture(tb testing.TB, files int, fail bool) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "input.cshd")
	input, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { input.Close() })
	writer := bufio.NewWriter(input)
	for i := range files {
		if _, err := fmt.Fprintf(writer, ",sha256,%064x archive/report-%05d.txt\n", i, i); err != nil {
			tb.Fatal(err)
		}
	}
	if fail {
		if _, err := fmt.Fprintln(writer, "broken"); err != nil {
			tb.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		tb.Fatal(err)
	}
	if err := input.Close(); err != nil {
		tb.Fatal(err)
	}
	return path
}

func assertIndexedImport(
	tb testing.TB, db *database.DB, path string, files int, fail bool,
	result base.Snapshot, importErr error,
) {
	tb.Helper()
	if fail {
		if importErr == nil || !strings.Contains(importErr.Error(), fmt.Sprintf("line %d", files+1)) {
			tb.Fatalf("expected malformed final record: %v", importErr)
		}
	} else if importErr != nil || result.FileCount != int64(files) ||
		result.ContentCount != int64(files) {
		tb.Fatalf("import: %+v: %v", result, importErr)
	}
	state, err := db.GetCatalogState(tb.Context())
	if err != nil || (fail && state.Revision != 0) || (!fail && state.Revision != result.Id) {
		tb.Fatalf("catalog state: %+v: %v", state, err)
	}
	if !fail {
		directory, err := db.GetDirectory(tb.Context(), result.Id, "")
		if err != nil || directory.FileCount != int64(files) ||
			directory.ContentCount != int64(files) ||
			directory.UnknownSizeFileCount != int64(files) ||
			directory.UnknownSizeContentCount != int64(files) || directory.KnownBytes != 0 {
			tb.Fatalf("root directory: %+v: %v", directory, err)
		}
	}
	for _, match := range []string{"exact", "substring"} {
		page, err := db.Search(tb.Context(), base.SearchFilters{
			Query: "REPORT-00000.TXT", Field: "name", Match: match,
			ContentFilters: base.ContentFilters{
				Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks,
			},
		}, 50, base.SearchAnchor{}, nil)
		want := 1
		if fail {
			want = 0
		}
		if err != nil || len(page.Items) != want {
			tb.Fatalf("%s search: %+v: %v", match, page, err)
		}
	}
	// Import ownership has been released; this test connection only inspects committed indexes.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	defer raw.Close()
	if fail {
		for _, table := range []string{
			"snapshot", "observation", "content", "pending_size", "import_content",
			"search_path", "search_trigram", "directory", "directory_content",
			"directory_file", "directory_build",
		} {
			var count int
			if err := raw.QueryRowContext(tb.Context(), "SELECT COUNT(*) FROM "+table).
				Scan(&count); err != nil || count != 0 {
				tb.Fatalf("%s: count=%d: %v", table, count, err)
			}
		}
	} else {
		var complete int
		if err := raw.QueryRowContext(tb.Context(),
			"SELECT COUNT(*) FROM snapshot WHERE id=? AND state='complete'", result.Id).
			Scan(&complete); err != nil || complete != 1 {
			tb.Fatalf("complete snapshot: %d: %v", complete, err)
		}
	}
	rows, err := raw.QueryContext(tb.Context(), "PRAGMA foreign_key_check")
	if err != nil {
		tb.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		tb.Fatal("foreign key violation")
	}
	if err := rows.Err(); err != nil {
		tb.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		tb.Fatal(err)
	}
	if _, err := raw.ExecContext(tb.Context(),
		"INSERT INTO search_trigram(search_trigram,rank) VALUES('integrity-check',1)"); err != nil {
		tb.Fatal(err)
	}
}

func TestIndexedImportBenchmarkFixture(t *testing.T) {
	const files = 2 * defaultBatchSize
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("late_failure_%t", fail), func(t *testing.T) {
			input := writeIndexedImportFixture(t, files, fail)
			path := filepath.Join(t.TempDir(), "catalog.sqlite")
			db, err := database.OpenContext(t.Context(), path)
			assertNoErr(t, err)
			t.Cleanup(func() { db.Close() })
			disk, err := db.CreateDisk("disk", "", "", 0)
			assertNoErr(t, err)
			committed := int64(0)
			result, err := Import(t.Context(), db, Request{
				DiskID: base.DiskId(disk), Path: input, CapturedAt: time.Unix(10, 0),
				Progress: func(p Progress) error { committed = p.CommittedFiles; return nil },
			})
			if committed != files {
				t.Fatalf("committed files: %d, want %d", committed, files)
			}
			assertIndexedImport(t, db, path, files, fail, result, err)
		})
	}
}

func TestImportHeapSampler(t *testing.T) {
	stop := startImportHeapSampler()
	result := stop()
	if result.samples < 2 || result.peak == 0 {
		t.Fatalf("missing initial/final samples: %+v", result)
	}
}

func BenchmarkSearchIndexedImport(b *testing.B) {
	for _, files := range []int{50000, 1000000} {
		for _, fail := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/late_failure_%t", files, fail), func(b *testing.B) {
				inputPath := writeIndexedImportFixture(b, files, fail)
				b.ReportAllocs()
				var elapsed, ingestion, directories, publishing time.Duration
				var peak uint64
				var samples, catalogBytes int64
				for b.Loop() {
					b.StopTimer()
					path := filepath.Join(b.TempDir(), "catalog.sqlite")
					db, err := database.OpenContext(b.Context(), path)
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(func() { db.Close() })
					disk, err := db.CreateDisk("disk", "", "", 0)
					if err != nil {
						b.Fatal(err)
					}
					var directoriesAt, publishingAt time.Time
					stopSampler := startImportHeapSampler()
					b.StartTimer()
					started := time.Now()
					result, importErr := Import(b.Context(), db, Request{
						DiskID: base.DiskId(disk), Path: inputPath, CapturedAt: time.Unix(10, 0),
						Progress: func(p Progress) error {
							switch p.Phase {
							case ProgressDirectories:
								directoriesAt = time.Now()
							case ProgressPublishing:
								publishingAt = time.Now()
							}
							return nil
						},
					})
					finished := time.Now()
					b.StopTimer()
					memory := stopSampler()
					peak = max(peak, memory.peak)
					samples += int64(memory.samples)
					elapsed += finished.Sub(started)
					if !fail {
						if directoriesAt.IsZero() || publishingAt.IsZero() {
							b.Fatal("missing import phase transitions")
						}
						ingestion += directoriesAt.Sub(started)
						directories += publishingAt.Sub(directoriesAt)
						publishing += finished.Sub(publishingAt)
					}
					assertIndexedImport(b, db, path, files, fail, result, importErr)
					if err := db.Close(); err != nil {
						b.Fatal(err)
					}
					info, err := os.Stat(path)
					if err != nil {
						b.Fatal(err)
					}
					catalogBytes += info.Size()
					b.StartTimer()
				}
				b.ReportMetric(elapsed.Seconds()/float64(b.N), "import-s/op")
				b.ReportMetric(float64(files)*float64(b.N)/elapsed.Seconds(), "input-files/s")
				b.ReportMetric(float64(peak), "sampled-heap-bytes")
				b.ReportMetric(float64(samples)/float64(b.N), "heap-samples/op")
				b.ReportMetric(float64(catalogBytes)/float64(b.N), "catalog-bytes/op")
				if !fail {
					b.ReportMetric(ingestion.Seconds()/float64(b.N), "ingestion-s/op")
					b.ReportMetric(directories.Seconds()/float64(b.N), "directories-s/op")
					b.ReportMetric(publishing.Seconds()/float64(b.N), "publishing-s/op")
				}
			})
		}
	}
}
