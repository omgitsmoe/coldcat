package httpapi

import (
	"bufio"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type httpDistributionProfile struct {
	current   int
	disks     int
	snapshots int
}

func (p httpDistributionProfile) files(disk int) int {
	n := p.current / p.disks
	if disk < p.current%p.disks {
		n++
	}
	return n
}

type httpDistributionFixture struct {
	httpWorkflowFixture
	profile   httpDistributionProfile
	snapshots []httpHistorySnapshot
}

func newHTTPDistributionFixture(tb testing.TB, p httpDistributionProfile) httpDistributionFixture {
	tb.Helper()
	if (p.disks != 3 && p.disks != 12) || (p.snapshots != 1 && p.snapshots != 5) ||
		p.current/p.disks < 8 || p.current*p.snapshots > 1000000 {
		tb.Fatal("distribution fixture requires 3/12 disks, 1/5 snapshots, " +
			"8+ files per disk, <=1m history")
	}
	dir := tb.TempDir()
	db, err := database.OpenContext(tb.Context(), filepath.Join(dir, "catalog.sqlite"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := db.Close(); err != nil {
			tb.Error(err)
		}
	})
	a := app.New(db)
	f := httpDistributionFixture{profile: p}
	for diskIndex := range p.disks {
		disk, err := a.CreateDisk(tb.Context(), fmt.Sprintf("balanced-%02d", diskIndex), "", "", 0)
		if err != nil {
			tb.Fatal(err)
		}
		for generation := range p.snapshots {
			path := filepath.Join(dir, "inventory.cshd")
			writeHTTPDistributionInput(tb, path, p, diskIndex, generation)
			snapshot, err := a.Import(tb.Context(), app.ImportRequest{
				DiskID: disk, Path: path, CapturedAt: time.Unix(int64(10+generation), 0),
			})
			if err != nil {
				tb.Fatal(err)
			}
			if snapshot.FileCount != int64(p.files(diskIndex)) ||
				snapshot.ContentCount != snapshot.FileCount-1 {
				tb.Fatalf("distribution snapshot counts: %+v", snapshot)
			}
			f.snapshots = append(f.snapshots, httpHistorySnapshot{
				id: decimal(snapshot.Id), disk: decimal(disk),
				diskIndex: diskIndex, generation: generation,
			})
		}
	}
	if err := os.Remove(filepath.Join(dir, "inventory.cshd")); err != nil {
		tb.Fatal(err)
	}
	server := httptest.NewServer(New(a))
	tb.Cleanup(server.Close)
	f.httpWorkflowFixture = httpWorkflowFixture{server: server}
	var catalog catalogDTO
	f.get(tb, "/api/v1/catalog", &catalog)
	if catalog.DiskCount != fmt.Sprint(p.disks) || catalog.FileCount != fmt.Sprint(p.current) {
		tb.Fatalf("distribution catalog: %+v", catalog)
	}
	return f
}

// Equal-size inventories keep current totals constant as history grows. Replacing the
// retired path rather than appending a file avoids conflating history cost with size.
func httpDistributionRecords(
	p httpDistributionProfile, disk, generation int, write func(int, string),
) {
	for slot := range p.files(disk) {
		hash := 100 + disk*p.current + slot
		path := fmt.Sprintf("disk-%02d/archive/report-%07d.txt", disk, slot)
		switch {
		case slot < 2:
			hash = 0
			path = fmt.Sprintf("disk-%02d/copy-%d/keepsake-report.txt", disk, slot)
		case slot == 3 || slot > 4 && slot%5 == 0:
			hash += p.current * p.disks * (generation + 1)
		case slot == 4 && generation < p.snapshots-1:
			hash = 100 + p.current*p.disks*(p.snapshots+1) + disk
			path = fmt.Sprintf("disk-%02d/archive/retired-report.txt", disk)
		}
		write(hash, path)
	}
}

func writeHTTPDistributionInput(
	tb testing.TB, path string, p httpDistributionProfile, disk, generation int,
) {
	tb.Helper()
	file, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	if _, err := fmt.Fprintln(writer, "# version 1"); err != nil {
		tb.Fatal(err)
	}
	httpDistributionRecords(p, disk, generation, func(hash int, path string) {
		if _, err := fmt.Fprintf(writer, "5,4096,sha256,%064x %s\n", hash, path); err != nil {
			tb.Fatal(err)
		}
	})
	if err := writer.Flush(); err != nil {
		tb.Fatal(err)
	}
	if err := file.Close(); err != nil {
		tb.Fatal(err)
	}
}

func (f httpDistributionFixture) assertTarget(tb testing.TB, content contentDTO, scope base.Scope) {
	tb.Helper()
	p := f.profile
	locations, disks := 2*p.disks, p.disks
	if content.ID == "" || content.Hash.Hex != fmt.Sprintf("%064x", 0) ||
		content.Scope != scope || content.LocationCount != fmt.Sprint(locations) ||
		content.DiskCount != fmt.Sprint(disks) ||
		content.ObservationCount != fmt.Sprint(2*p.disks*p.snapshots) ||
		content.CurrentLocationCount != fmt.Sprint(2*p.disks) ||
		content.CurrentDiskCount != fmt.Sprint(p.disks) {
		tb.Fatalf("distribution target: %+v", content)
	}
}

func (f httpDistributionFixture) primary(tb testing.TB, scope base.Scope) {
	tb.Helper()
	var search searchPageDTO
	f.get(tb, workflowExactURL+"&scope="+string(scope), &search)
	if len(search.Items) != 1 || search.NextCursor == nil {
		tb.Fatalf("distribution primary search: %+v", search)
	}
	f.assertTarget(tb, search.Items[0].Content, scope)
	contentURL := "/api/v1/contents/" + search.Items[0].Content.ID
	var content contentDTO
	f.get(tb, contentURL+"?scope="+string(scope), &content)
	f.assertTarget(tb, content, scope)
	if content.ID != search.Items[0].Content.ID {
		tb.Fatal("distribution primary content identity changed")
	}
	query := contentURL + "/observations?limit=50&scope=" + string(scope)
	requestPath := query
	seen := make(map[string]bool)
	want := 2 * f.profile.disks
	if scope == base.ScopeHistory {
		want *= f.profile.snapshots
	}
	for pages := 0; ; pages++ {
		if pages > want/50 {
			tb.Fatal("distribution primary observation pagination did not terminate")
		}
		var page observationPageDTO
		f.get(tb, requestPath, &page)
		if page.Scope != scope || len(page.Items) == 0 {
			tb.Fatalf("distribution primary observations: %+v", page)
		}
		for _, item := range page.Items {
			if seen[item.Observation.ID] || item.Observation.ContentID != content.ID ||
				(scope == base.ScopeCurrent && !item.IsCurrent) {
				tb.Fatalf("distribution primary observation: %+v", item)
			}
			seen[item.Observation.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
		requestPath = query + "&cursor=" + url.QueryEscape(*page.NextCursor)
	}
	if len(seen) != want {
		tb.Fatalf("distribution primary observations: %d, want %d", len(seen), want)
	}
}

// Scales denote total current observations, not per-disk or all-history counts.
// One fixture serves all warm serial cases and is released before the next profile.
func BenchmarkHTTPDistributionWorkflow(b *testing.B) {
	for _, current := range []int{50000, 200000} {
		b.Run(fmt.Sprint(current), func(b *testing.B) {
			for _, disks := range []int{3, 12} {
				for _, snapshots := range []int{1, 5} {
					b.Run(fmt.Sprintf("disks%d/snapshots%d", disks, snapshots), func(b *testing.B) {
						f := newHTTPDistributionFixture(b, httpDistributionProfile{current, disks, snapshots})
						for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
							b.Run(string(scope), func(b *testing.B) {
								b.Run("primary", func(b *testing.B) {
									f.primary(b, scope)
									b.ReportAllocs()
									b.ResetTimer()
									for b.Loop() {
										f.primary(b, scope)
									}
								})
								suffix := "&scope=" + string(scope)
								var broad searchPageDTO
								f.get(b, workflowBroadURL+suffix, &broad)
								if len(broad.Items) != 50 || broad.NextCursor == nil {
									b.Fatalf("distribution broad page: %+v", broad)
								}
								cases := []struct {
									name string
									path string
									want int
								}{
									{"exact", workflowExactURL + suffix, 1},
									{"broad", workflowBroadURL + suffix, 50},
									{"broad_next", workflowBroadURL + suffix +
										"&cursor=" + url.QueryEscape(*broad.NextCursor), 50},
								}
								if scope == base.ScopeCurrent {
									cases = append(cases, struct {
										name string
										path string
										want int
									}{"replica_no_match", workflowBroadURL + suffix +
										"&replica_metric=disks&other_replicas=" + fmt.Sprint(disks), 0})
								}
								for _, test := range cases {
									b.Run(test.name, func(b *testing.B) {
										request := func() {
											var page searchPageDTO
											f.get(b, test.path, &page)
											if page.Scope != scope || len(page.Items) != test.want ||
												(test.want == 0 && page.NextCursor != nil) {
												b.Fatalf("distribution search page: %+v", page)
											}
											if test.name == "exact" {
												f.assertTarget(b, page.Items[0].Content, scope)
											}
										}
										request()
										b.ReportAllocs()
										b.ResetTimer()
										for b.Loop() {
											request()
										}
									})
								}
							})
						}
					})
				}
			}
		})
	}
}
