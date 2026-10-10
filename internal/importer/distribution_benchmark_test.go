package importer

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type importDistribution struct {
	name     string
	files    int
	contents int
	history  int
	deep     bool
	unknown  bool
}

func importDistributions(files int) []importDistribution {
	return []importDistribution{
		{name: "deep_known", files: files, contents: files, history: 1, deep: true},
		{name: "duplicate_shared", files: files, contents: files / 100, history: 1},
		{name: "history_enrichment", files: files, contents: files, history: 3, unknown: true},
	}
}

func writeDistributionInput(
	tb testing.TB, d importDistribution, generation int, fail bool,
) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "inventory.cshd")
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { f.Close() })
	w := bufio.NewWriter(f)
	if _, err := fmt.Fprintln(w, "# version 1"); err != nil {
		tb.Fatal(err)
	}
	for i := range d.files {
		size := "4096"
		if d.unknown && generation < d.history {
			size = ""
		}
		prefix := fmt.Sprintf("generation-%d", generation)
		if d.deep {
			prefix += fmt.Sprintf(
				"/disk-%02d/year-%02d/month-%02d/week/day/hour/minute/second/leaf",
				i%4, i%10, i%12,
			)
		}
		if _, err := fmt.Fprintf(w, ",%s,sha256,%064x %s/report-%07d.txt\n",
			size, i%d.contents, prefix, i); err != nil {
			tb.Fatal(err)
		}
	}
	if fail {
		if _, err := fmt.Fprintln(w, "broken"); err != nil {
			tb.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		tb.Fatal(err)
	}
	if err := f.Close(); err != nil {
		tb.Fatal(err)
	}
	return path
}

type distributionCatalog struct {
	db        *database.DB
	raw       *sql.DB
	path      string
	disk      base.DiskId
	completed []base.Snapshot
	counts    map[string]int64
}

var distributionTables = []string{
	"snapshot", "observation", "content", "search_path", "search_trigram",
	"directory", "directory_content", "directory_file", "directory_build",
}

func distributionCount(tb testing.TB, raw *sql.DB, query string, args ...any) int64 {
	tb.Helper()
	var n int64
	if err := raw.QueryRowContext(tb.Context(), query, args...).Scan(&n); err != nil {
		tb.Fatal(err)
	}
	return n
}

func openDistributionCatalog(
	tb testing.TB, d importDistribution, baseline []string,
) distributionCatalog {
	tb.Helper()
	c := distributionCatalog{path: filepath.Join(tb.TempDir(), "catalog.sqlite")}
	var err error
	c.db, err = database.OpenContext(tb.Context(), c.path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { c.db.Close() })
	disk, err := c.db.CreateDisk("distribution", "", "", 0)
	if err != nil {
		tb.Fatal(err)
	}
	c.disk = base.DiskId(disk)
	for generation, path := range baseline {
		snapshot, err := Import(tb.Context(), c.db, Request{
			DiskID: c.disk, Path: path, CapturedAt: time.Unix(int64(generation+1), 0),
		})
		if err != nil {
			tb.Fatal(err)
		}
		if snapshot.FileCount != int64(d.files) || snapshot.ContentCount != int64(d.contents) {
			tb.Fatalf("baseline counts: %+v", snapshot)
		}
		c.completed = append(c.completed, snapshot)
	}
	c.raw, err = sql.Open("sqlite", c.path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { c.raw.Close() })
	c.counts = make(map[string]int64)
	for _, table := range distributionTables {
		c.counts[table] = distributionCount(tb, c.raw, "SELECT COUNT(*) FROM "+table)
	}
	wantUnknown := int64(0)
	if d.unknown {
		wantUnknown = int64(d.contents)
	}
	if got := distributionCount(tb, c.raw, "SELECT COUNT(*) FROM content WHERE size IS NULL"); got != wantUnknown {
		tb.Fatalf("baseline unknown contents: %d, want %d", got, wantUnknown)
	}
	return c
}

func distributionBaseline(tb testing.TB, d importDistribution) []string {
	tb.Helper()
	paths := make([]string, d.history)
	for generation := range paths {
		paths[generation] = writeDistributionInput(tb, d, generation, false)
	}
	return paths
}

func assertDistributionImport(
	tb testing.TB, d importDistribution, c distributionCatalog,
	result base.Snapshot, importErr error, failure string,
) {
	tb.Helper()
	failed := failure != ""
	if failed {
		tb.Logf("failed import result: %v", importErr)
		fragment := "distribution publication failure"
		if failure == "parse" {
			fragment = fmt.Sprintf("line %d", d.files+2)
		}
		if importErr == nil || !strings.Contains(importErr.Error(), fragment) {
			tb.Fatalf("expected %s failure: %v", failure, importErr)
		}
	} else if importErr != nil || result.FileCount != int64(d.files) ||
		result.ContentCount != int64(d.contents) {
		tb.Fatalf("import: %+v: %v", result, importErr)
	}
	wantSnapshots := int64(d.history)
	wantObservations := int64(d.history * d.files)
	if !failed {
		wantSnapshots++
		wantObservations += int64(d.files)
	}
	directoriesPerSnapshot := int64(2)
	if d.deep {
		directoriesPerSnapshot += 4 + 20 + 60 + 6*60
	}
	for table, want := range map[string]int64{
		"snapshot": wantSnapshots, "observation": wantObservations,
		"content": int64(d.contents), "search_path": wantObservations,
		"directory_file": wantObservations, "directory": wantSnapshots * directoriesPerSnapshot,
		"pending_size": 0, "import_content": 0, "directory_build": wantSnapshots,
	} {
		if got := distributionCount(tb, c.raw, "SELECT COUNT(*) FROM "+table); got != want {
			tb.Fatalf("%s: %d, want %d", table, got, want)
		}
	}
	if failed {
		for table, want := range c.counts {
			if got := distributionCount(tb, c.raw, "SELECT COUNT(*) FROM "+table); got != want {
				tb.Fatalf("completed %s changed: %d, want %d", table, got, want)
			}
		}
	}
	wantUnknown := int64(0)
	if d.unknown && failed {
		wantUnknown = int64(d.contents)
	}
	if got := distributionCount(tb, c.raw, "SELECT COUNT(*) FROM content WHERE size IS NULL"); got != wantUnknown {
		tb.Fatalf("unknown contents: %d, want %d", got, wantUnknown)
	}
	if got := distributionCount(tb, c.raw,
		"SELECT COUNT(*) FROM content WHERE size IS NOT NULL AND size!=4096"); got != 0 {
		tb.Fatalf("incorrect content sizes: %d", got)
	}
	snapshots := append([]base.Snapshot(nil), c.completed...)
	if !failed {
		snapshots = append(snapshots, result)
	}
	for generation, snapshot := range snapshots {
		root, err := c.db.GetDirectory(tb.Context(), snapshot.Id, "")
		unknownFiles, knownBytes, uniqueBytes := int64(0), int64(d.files)*4096,
			int64(d.contents)*4096
		if wantUnknown != 0 {
			unknownFiles, knownBytes, uniqueBytes = int64(d.files), 0, 0
		}
		if err != nil || root.FileCount != int64(d.files) ||
			root.ContentCount != int64(d.contents) || root.KnownBytes != knownBytes ||
			root.UniqueContentKnownBytes != uniqueBytes ||
			root.UnknownSizeFileCount != unknownFiles ||
			root.UnknownSizeContentCount != wantUnknown {
			tb.Fatalf("snapshot %d directory: %+v: %v", snapshot.Id, root, err)
		}
		if got := distributionCount(tb, c.raw,
			"SELECT COUNT(*) FROM snapshot WHERE id=? AND state='complete'", snapshot.Id); got != 1 {
			tb.Fatalf("snapshot %d not complete", snapshot.Id)
		}
		if d.deep {
			leaf, err := c.db.GetDirectory(tb.Context(), snapshot.Id, fmt.Sprintf(
				"generation-%d/disk-00/year-00/month-00/week/day/hour/minute/second/leaf",
				generation,
			))
			wantFiles := int64((d.files + 59) / 60)
			if err != nil || leaf.FileCount != wantFiles || leaf.ContentCount != wantFiles ||
				leaf.KnownBytes != wantFiles*4096 || leaf.UnknownSizeFileCount != 0 {
				tb.Fatalf("snapshot %d deep leaf: %+v: %v", snapshot.Id, leaf, err)
			}
		}
	}
	wantCurrent := snapshots[len(snapshots)-1].Id
	state, err := c.db.GetCatalogState(tb.Context())
	if err != nil || state.Revision != wantCurrent {
		tb.Fatalf("catalog state: %+v: %v", state, err)
	}
	for _, match := range []string{"exact", "substring"} {
		page, err := c.db.Search(tb.Context(), base.SearchFilters{
			Query: "REPORT-0000000.TXT", Field: "name", Match: match,
			ContentFilters: base.ContentFilters{
				Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks,
			},
		}, 50, base.SearchAnchor{}, nil)
		if err != nil || len(page.Items) != 1 || page.Items[0].Snapshot.Id != wantCurrent {
			tb.Fatalf("%s completed search: %+v: %v", match, page, err)
		}
	}
	if got := distributionCount(tb, c.raw, "SELECT COUNT(*) FROM pragma_foreign_key_check"); got != 0 {
		tb.Fatalf("foreign key violations: %d", got)
	}
	if _, err := c.raw.ExecContext(tb.Context(),
		"INSERT INTO search_trigram(search_trigram,rank) VALUES('integrity-check',1)"); err != nil {
		tb.Fatal(err)
	}
}

func TestImportDistributionFixtures(t *testing.T) {
	for _, d := range importDistributions(2 * defaultBatchSize) {
		t.Run(d.name, func(t *testing.T) {
			baseline := distributionBaseline(t, d)
			for _, failure := range []string{"", "parse", "publication"} {
				name := "success"
				if failure != "" {
					name = "failure_" + failure
				}
				t.Run(name, func(t *testing.T) {
					c := openDistributionCatalog(t, d, baseline)
					input := writeDistributionInput(t, d, d.history, failure == "parse")
					if failure == "publication" {
						_, err := c.raw.ExecContext(t.Context(), `CREATE TRIGGER fail_distribution
 BEFORE UPDATE ON snapshot WHEN NEW.state='complete'
 BEGIN SELECT RAISE(ABORT,'distribution publication failure'); END`)
						assertNoErr(t, err)
					}
					var committed int64
					publishingSeen := false
					result, err := Import(t.Context(), c.db, Request{
						DiskID: c.disk, Path: input, CapturedAt: time.Unix(10, 0),
						Progress: func(p Progress) error {
							committed = p.CommittedFiles
							if p.Phase == ProgressPublishing {
								publishingSeen = true
								if d.unknown {
									assertEqual(t, distributionCount(t, c.raw,
										"SELECT COUNT(*) FROM pending_size"), int64(d.contents))
									assertEqual(t, distributionCount(t, c.raw,
										"SELECT COUNT(*) FROM content WHERE size IS NULL"),
										int64(d.contents))
									assertEqual(t, distributionCount(t, c.raw,
										`SELECT unknown_size_file_count FROM directory
 WHERE snapshot_id=? AND path=''`, c.completed[0].Id), int64(d.files))
								}
							}
							return nil
						},
					})
					if committed != int64(d.files) {
						t.Fatalf("committed files: %d, want %d", committed, d.files)
					}
					assertEqual(t, publishingSeen, failure != "parse")
					assertDistributionImport(t, d, c, result, err, failure)
				})
			}
		})
	}
}

func BenchmarkImportDistributions(b *testing.B) {
	for _, files := range []int{50000, 1000000} {
		for _, d := range importDistributions(files) {
			for _, fail := range []bool{false, true} {
				b.Run(fmt.Sprintf("%d/%s/late_failure_%t", files, d.name, fail), func(b *testing.B) {
					baseline := distributionBaseline(b, d)
					input := writeDistributionInput(b, d, d.history, fail)
					var elapsed, ingestion, directories, publishing time.Duration
					var peak uint64
					var samples, catalogBytes int64
					b.ReportAllocs()
					for b.Loop() {
						b.StopTimer()
						c := openDistributionCatalog(b, d, baseline)
						var directoriesAt, publishingAt time.Time
						stop := startImportHeapSampler()
						b.Cleanup(func() { stop() })
						b.StartTimer()
						started := time.Now()
						result, err := Import(b.Context(), c.db, Request{
							DiskID: c.disk, Path: input, CapturedAt: time.Unix(10, 0),
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
						memory := stop()
						peak = max(peak, memory.peak)
						samples += int64(memory.samples)
						elapsed += finished.Sub(started)
						failure := ""
						if fail {
							failure = "parse"
						} else {
							if directoriesAt.IsZero() || publishingAt.IsZero() {
								b.Fatal("missing import phase transitions")
							}
							ingestion += directoriesAt.Sub(started)
							directories += publishingAt.Sub(directoriesAt)
							publishing += finished.Sub(publishingAt)
						}
						assertDistributionImport(b, d, c, result, err, failure)
						if err := c.raw.Close(); err != nil {
							b.Fatal(err)
						}
						if err := c.db.Close(); err != nil {
							b.Fatal(err)
						}
						info, err := os.Stat(c.path)
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
}
