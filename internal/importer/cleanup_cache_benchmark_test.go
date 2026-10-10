//go:build cleanupcache && linux

package importer

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"slices"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/database"
)

var cleanupCacheCases = []struct {
	name string
	kib  int
}{
	{"default", 0},
	{"8MiB", 8192},
	{"32MiB", 32768},
}

func assertCleanupCache(tb testing.TB, result database.CleanupCacheResult, kib int) {
	tb.Helper()
	expected := result.Previous
	if kib != 0 {
		expected = -kib
	}
	if result.Applied != expected || result.Restored != result.Previous ||
		result.MaxOpenConnections != 1 {
		tb.Fatalf("cache configuration/restoration mismatch: %+v, KiB=%d", result, kib)
	}
}

func BenchmarkDistributionCleanupCache(b *testing.B) {
	for _, files := range []int{50000, 1000000} {
		for _, cache := range cleanupCacheCases {
			b.Run(fmt.Sprintf("%d/%s", files, cache.name), func(b *testing.B) {
				d := importDistributions(files)[1]
				baseline := distributionBaseline(b, d)
				input := writeDistributionInput(b, d, d.history, true)
				var peak uint64
				var samples, beforeHWM, afterHWM int64
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					c, id := prepareCleanupProfile(b, d, baseline, input)
					deferCleanupCatalog(b, &c)
					var result database.CleanupCacheResult
					beforeHWM = max(beforeHWM, cleanupCacheHWM(b))
					stop := startImportHeapSampler()
					b.Cleanup(func() { stop() })
					b.StartTimer()
					pprof.Do(b.Context(), pprof.Labels("phase", "cleanup"), func(ctx context.Context) {
						var err error
						result, err = c.db.CleanupImportWithCacheForBenchmark(ctx, id, cache.kib)
						if err != nil {
							logCleanupProfileCounts(b, c)
							b.Fatalf("cleanup cache=%s snapshot=%d: %v", cache.name, id, err)
						}
					})
					b.StopTimer()
					memory := stop()
					peak = max(peak, memory.peak)
					samples += int64(memory.samples)
					afterHWM = max(afterHWM, cleanupCacheHWM(b))
					assertCleanupCache(b, result, cache.kib)
					b.Logf("cache=%s previous=%d applied=%d restored=%d max_open=%d",
						cache.name, result.Previous, result.Applied, result.Restored,
						result.MaxOpenConnections)
					verifyCleanupProfile(b, d, &c)
					b.StartTimer()
				}
				b.ReportMetric(float64(peak), "sampled-heap-bytes")
				b.ReportMetric(float64(samples)/float64(b.N), "heap-samples/op")
				b.ReportMetric(float64(beforeHWM), "before-cleanup-HWM-bytes")
				b.ReportMetric(float64(afterHWM), "after-cleanup-HWM-bytes")
			})
		}
	}
}

func cleanupCacheHWM(tb testing.TB) int64 {
	tb.Helper()
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		tb.Fatal(err)
	}
	hwm, err := parseImportRSSHWM(string(status))
	if err != nil {
		tb.Fatal(err)
	}
	return hwm
}

func TestCleanupCacheFixture(t *testing.T) {
	for _, cache := range cleanupCacheCases {
		for _, fail := range []bool{false, true} {
			for _, sharedPaths := range []bool{false, true} {
				name := fmt.Sprintf("%s/rollback_%t/shared_paths_%t", cache.name, fail, sharedPaths)
				t.Run(name, func(t *testing.T) {
					d := importDistributions(defaultBatchSize)[1]
					generation := d.history
					if sharedPaths {
						generation = 0
					}
					c, id := prepareCleanupProfile(t, d, distributionBaseline(t, d),
						writeDistributionInput(t, d, generation, true))
					deferCleanupCatalog(t, &c)
					expected := -2000
					if cache.kib != 0 {
						expected = -cache.kib
					}
					_, err := c.raw.Exec(fmt.Sprintf(`CREATE TRIGGER assert_cleanup_cache
 BEFORE DELETE ON observation WHEN (SELECT cache_size FROM pragma_cache_size)!=%d
 BEGIN SELECT RAISE(ABORT,'wrong cleanup connection cache'); END`, expected))
					if err != nil {
						t.Fatal(err)
					}
					before := make(map[string]int64)
					if fail {
						tables := append(slices.Clone(distributionTables), "pending_size", "import_content")
						for _, table := range tables {
							before[table] = distributionCount(t, c.raw, "SELECT COUNT(*) FROM "+table)
						}
						_, err := c.raw.Exec(`CREATE TRIGGER fail_cache_cleanup BEFORE DELETE ON snapshot
 BEGIN SELECT RAISE(ABORT,'cache cleanup rollback'); END`)
						if err != nil {
							t.Fatal(err)
						}
					}
					result, err := c.db.CleanupImportWithCacheForBenchmark(t.Context(), id, cache.kib)
					assertCleanupCache(t, result, cache.kib)
					if !fail {
						if err != nil {
							t.Fatal(err)
						}
						verifyCleanupProfile(t, d, &c)
						return
					}
					if err == nil || !strings.Contains(err.Error(), "cache cleanup rollback") ||
						!strings.Contains(err.Error(), "delete import snapshot") {
						t.Fatalf("wrong cleanup failure: %v", err)
					}
					for table, want := range before {
						if got := distributionCount(t, c.raw, "SELECT COUNT(*) FROM "+table); got != want {
							t.Fatalf("rollback changed %s: %d, want %d", table, got, want)
						}
					}
					if _, err := c.raw.Exec(
						"INSERT INTO search_trigram(search_trigram,rank) VALUES('integrity-check',1)"); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}
