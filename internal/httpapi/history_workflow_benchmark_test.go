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

const historyWorkflowGenerations = 5
const historyWorkflowRetiredURL = "/api/v1/search?q=RETIRED-REPORT.TXT&match=exact&limit=1"

type httpHistorySnapshot struct {
	id         string
	disk       string
	diskIndex  int
	generation int
}

type httpHistoryWorkflowFixture struct {
	httpWorkflowFixture
	currentObservations int
	snapshots           []httpHistorySnapshot
}

func newHTTPHistoryWorkflowFixture(tb testing.TB, observations int) httpHistoryWorkflowFixture {
	tb.Helper()
	if observations%historyWorkflowGenerations != 0 ||
		observations/historyWorkflowGenerations < 104 {
		tb.Fatal("history fixture requires five equal generations of at least 104 observations")
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
	f := httpHistoryWorkflowFixture{
		currentObservations: observations / historyWorkflowGenerations,
	}
	for diskIndex := range 3 {
		disk, err := a.CreateDisk(tb.Context(), fmt.Sprintf("disk-%d", diskIndex), "", "", 0)
		if err != nil {
			tb.Fatal(err)
		}
		for generation := range historyWorkflowGenerations {
			path := filepath.Join(dir, fmt.Sprintf("disk-%d-%d.cshd", diskIndex, generation))
			writeHTTPHistoryWorkflowInput(tb, path, diskIndex, generation, f.currentObservations)
			snapshot, err := a.Import(tb.Context(), app.ImportRequest{
				DiskID: disk, Path: path, CapturedAt: time.Unix(int64(10+generation), 0),
			})
			if err != nil {
				tb.Fatal(err)
			}
			files, contents := int64(1), int64(1)
			if diskIndex == 0 {
				files = int64(f.currentObservations - 2)
				contents = files - 1
			}
			if snapshot.FileCount != files || snapshot.ContentCount != contents {
				tb.Fatalf("history snapshot counts: %+v, want %d/%d", snapshot, files, contents)
			}
			f.snapshots = append(f.snapshots, httpHistorySnapshot{
				id: decimal(snapshot.Id), disk: decimal(disk),
				diskIndex: diskIndex, generation: generation,
			})
		}
	}
	server := httptest.NewServer(New(a))
	tb.Cleanup(server.Close)
	f.httpWorkflowFixture = httpWorkflowFixture{server: server}
	var catalog catalogDTO
	f.get(tb, "/api/v1/catalog", &catalog)
	if catalog.DiskCount != "3" || catalog.FileCount != fmt.Sprint(f.currentObservations) ||
		catalog.ContentCount != fmt.Sprint(f.currentObservations-3) {
		tb.Fatalf("history fixture catalog: %+v", catalog)
	}
	f.workflow(tb)
	var target searchPageDTO
	f.get(tb, workflowExactURL, &target)
	assertHTTPHistoryTarget(tb, target.Items[0].Content, base.ScopeCurrent)
	var history searchPageDTO
	f.get(tb, workflowExactURL+"&scope=history", &history)
	if len(history.Items) != 1 || history.NextCursor == nil ||
		history.Items[0].Content.ID != target.Items[0].Content.ID {
		tb.Fatalf("history target search: %+v", history)
	}
	assertHTTPHistoryTarget(tb, history.Items[0].Content, base.ScopeHistory)
	var retired searchPageDTO
	f.get(tb, historyWorkflowRetiredURL+"&scope=history", &retired)
	if len(retired.Items) != 1 || retired.NextCursor == nil || retired.Items[0].IsCurrent ||
		retired.Items[0].Content.ObservationCount != "4" ||
		retired.Items[0].Content.CurrentLocationCount != "0" ||
		retired.Items[0].Content.CurrentDiskCount != "0" {
		tb.Fatalf("history-only search: %+v", retired)
	}
	return f
}

func httpHistoryWorkflowRecords(diskIndex, generation, current int, write func(int, string)) {
	write(0, fmt.Sprintf("disk-%d/keepsake-report.txt", diskIndex))
	if diskIndex != 0 {
		return
	}
	write(0, "backup/keepsake-report.txt")
	for i := range current - 4 {
		hash := i + 1
		if i%5 == 0 {
			hash += generation * current
		}
		path := fmt.Sprintf("archive/report-%07d.txt", i)
		if i == 0 && generation < historyWorkflowGenerations-1 {
			hash = historyWorkflowGenerations*current + 1
			path = "archive/retired-report.txt"
		}
		write(hash, path)
	}
}

func writeHTTPHistoryWorkflowInput(
	tb testing.TB, path string, diskIndex, generation, current int,
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
	httpHistoryWorkflowRecords(diskIndex, generation, current, func(hash int, path string) {
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

func assertHTTPHistoryTarget(tb testing.TB, content contentDTO, scope base.Scope) {
	tb.Helper()
	assertWorkflowTarget(tb, content)
	if content.Scope != scope || content.ObservationCount != "20" ||
		content.CurrentLocationCount != "4" || content.CurrentDiskCount != "3" {
		tb.Fatalf("history target: %+v", content)
	}
}

func historyObservationKey(snapshot, path string) string { return snapshot + "\x00" + path }

func (f httpHistoryWorkflowFixture) assertSearch(
	tb testing.TB, path string, scope base.Scope, want map[string]string,
) []searchItemDTO {
	tb.Helper()
	remaining := make(map[string]string, len(want))
	for key, hash := range want {
		remaining[key] = hash
	}
	snapshots := make(map[string]httpHistorySnapshot)
	for _, snapshot := range f.snapshots {
		snapshots[snapshot.id] = snapshot
	}
	var items []searchItemDTO
	seen := make(map[string]bool)
	contentIDs := make(map[string]string)
	requestPath := path
	for pages := 0; ; pages++ {
		if pages > len(want) {
			tb.Fatal("search pagination did not terminate")
		}
		var page searchPageDTO
		f.get(tb, requestPath, &page)
		if page.Scope != scope {
			tb.Fatalf("search scope: %s, want %s", page.Scope, scope)
		}
		for _, item := range page.Items {
			key := historyObservationKey(item.Snapshot.ID, item.Observation.Path)
			hash, ok := remaining[key]
			snapshot := snapshots[item.Snapshot.ID]
			if !ok || hash != item.Content.Hash.Hex || seen[item.Observation.ID] ||
				item.IsCurrent != (snapshot.generation == historyWorkflowGenerations-1) ||
				item.Disk.ID != snapshot.disk || item.Snapshot.DiskID != snapshot.disk ||
				item.Observation.SnapshotID != snapshot.id ||
				item.Snapshot.CapturedAt != timestamp(time.Unix(int64(10+snapshot.generation), 0)) ||
				item.Observation.ContentID != item.Content.ID || item.Content.Scope != scope {
				tb.Fatalf("unexpected search observation: %+v", item)
			}
			if id, exists := contentIDs[hash]; exists && id != item.Content.ID {
				tb.Fatalf("content identity changed for %s", hash)
			}
			contentIDs[hash] = item.Content.ID
			delete(remaining, key)
			seen[item.Observation.ID] = true
			items = append(items, item)
		}
		if page.NextCursor == nil {
			break
		}
		if len(page.Items) == 0 {
			tb.Fatal("empty search page has a continuation cursor")
		}
		requestPath = path + "&cursor=" + url.QueryEscape(*page.NextCursor)
	}
	if len(remaining) != 0 {
		tb.Fatalf("search omitted %d expected observations", len(remaining))
	}
	return items
}

func TestHTTPHistoryWorkflowBenchmarkFixture(t *testing.T) {
	f := newHTTPHistoryWorkflowFixture(t, 520)
	for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
		t.Run(string(scope), func(t *testing.T) {
			want := make(map[string]string)
			targets := make(map[string]string)
			retired := make(map[string]string)
			for _, snapshot := range f.snapshots {
				if scope == base.ScopeCurrent && snapshot.generation != 4 {
					continue
				}
				httpHistoryWorkflowRecords(snapshot.diskIndex, snapshot.generation, 104,
					func(hash int, path string) {
						key := historyObservationKey(snapshot.id, path)
						want[key] = fmt.Sprintf("%064x", hash)
						if hash == 0 {
							targets[key] = want[key]
						}
						if path == "archive/retired-report.txt" {
							retired[key] = want[key]
						}
					})
			}
			if len(want) != map[base.Scope]int{base.ScopeCurrent: 104, base.ScopeHistory: 520}[scope] {
				t.Fatalf("unexpected expected fixture size: %d", len(want))
			}
			items := f.assertSearch(t, workflowBroadURL+"&scope="+string(scope), scope, want)
			assertHTTPHistoryDistribution(t, f, scope, items)
			for _, path := range []string{workflowExactURL, workflowSubstringURL} {
				matches := f.assertSearch(t, path+"&scope="+string(scope), scope, targets)
				for _, item := range matches {
					assertHTTPHistoryTarget(t, item.Content, scope)
				}
				assertHTTPHistoryObservations(t, f, matches[0].Content.ID, scope, matches)
			}
			matches := f.assertSearch(t, historyWorkflowRetiredURL+"&scope="+string(scope), scope, retired)
			if scope == base.ScopeHistory {
				if len(matches) != 4 {
					t.Fatalf("retired observations: %d", len(matches))
				}
				var content contentDTO
				f.get(t, "/api/v1/contents/"+matches[0].Content.ID, &content)
				if content.LocationCount != "0" || content.DiskCount != "0" ||
					content.CurrentLocationCount != "0" || content.ObservationCount != "4" {
					t.Fatalf("retired current summary: %+v", content)
				}
			}
		})
	}
	var empty searchPageDTO
	f.get(t, workflowNoMatchURL, &empty)
	if len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatalf("history fixture replica no-match: %+v", empty)
	}
}

func assertHTTPHistoryDistribution(
	tb testing.TB, f httpHistoryWorkflowFixture, scope base.Scope, items []searchItemDTO,
) {
	tb.Helper()
	snapshots := make(map[string]httpHistorySnapshot)
	for _, snapshot := range f.snapshots {
		snapshots[snapshot.id] = snapshot
	}
	contents := make(map[string]bool)
	paths := make(map[string]bool)
	var changed, stable, retired, replacement int
	for _, item := range items {
		contents[item.Content.Hash.Hex] = true
		paths[item.Observation.Path] = true
		generation := snapshots[item.Snapshot.ID].generation
		var hash int
		switch item.Observation.Path {
		case "archive/report-0000005.txt":
			changed++
			hash = 6 + 104*generation
		case "archive/report-0000006.txt":
			stable++
			hash = 7
		case "archive/retired-report.txt":
			retired++
			hash = 521
		case "archive/report-0000000.txt":
			replacement++
			hash = 417
		default:
			continue
		}
		if item.Content.Hash.Hex != fmt.Sprintf("%064x", hash) {
			tb.Fatalf("history distribution identity: %+v", item)
		}
	}
	if scope == base.ScopeCurrent {
		if len(contents) != 101 || len(paths) != 104 ||
			changed != 1 || stable != 1 || retired != 0 || replacement != 1 {
			tb.Fatalf("current distribution: contents=%d paths=%d samples=%d/%d/%d/%d",
				len(contents), len(paths), changed, stable, retired, replacement)
		}
	} else if len(contents) != 178 || len(paths) != 105 ||
		changed != 5 || stable != 5 || retired != 4 || replacement != 1 {
		tb.Fatalf("history distribution: contents=%d paths=%d samples=%d/%d/%d/%d",
			len(contents), len(paths), changed, stable, retired, replacement)
	}
}

func assertHTTPHistoryObservations(
	tb testing.TB, f httpHistoryWorkflowFixture, contentID string,
	scope base.Scope, matches []searchItemDTO,
) {
	tb.Helper()
	want := make(map[string]searchItemDTO, len(matches))
	for _, match := range matches {
		want[match.Observation.ID] = match
	}
	path := "/api/v1/contents/" + contentID + "/observations?limit=2&scope=" + string(scope)
	requestPath := path
	for pages := 0; ; pages++ {
		if pages > len(matches) {
			tb.Fatal("observation pagination did not terminate")
		}
		var page observationPageDTO
		f.get(tb, requestPath, &page)
		if page.Scope != scope {
			tb.Fatalf("observation scope: %s", page.Scope)
		}
		for _, item := range page.Items {
			match, ok := want[item.Observation.ID]
			if !ok || item.Observation.ContentID != contentID ||
				item.Observation.Path != match.Observation.Path ||
				item.Snapshot.ID != match.Snapshot.ID || item.Disk.ID != match.Disk.ID ||
				item.IsCurrent != match.IsCurrent {
				tb.Fatalf("unexpected content observation: %+v", item)
			}
			delete(want, item.Observation.ID)
		}
		if page.NextCursor == nil {
			break
		}
		if len(page.Items) == 0 {
			tb.Fatal("empty observation page has a continuation cursor")
		}
		requestPath = path + "&cursor=" + url.QueryEscape(*page.NextCursor)
	}
	if len(want) != 0 {
		tb.Fatalf("observation pages omitted %d expected observations", len(want))
	}
}

func BenchmarkHTTPHistoryWorkflow(b *testing.B) {
	for _, observations := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(observations), func(b *testing.B) {
			f := newHTTPHistoryWorkflowFixture(b, observations)
			var target searchPageDTO
			f.get(b, workflowExactURL, &target)
			contentURL := "/api/v1/contents/" + target.Items[0].Content.ID
			for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
				b.Run(string(scope), func(b *testing.B) {
					suffix := "&scope=" + string(scope)
					var broad searchPageDTO
					f.get(b, workflowBroadURL+suffix, &broad)
					if len(broad.Items) != 50 || broad.NextCursor == nil {
						b.Fatalf("history fixture broad page: %+v", broad)
					}
					locationURL := contentURL + "/observations?limit=2" + suffix
					cases := []struct {
						name string
						path string
					}{
						{"exact", workflowExactURL + suffix},
						{"substring", workflowSubstringURL + suffix},
						{"broad", workflowBroadURL + suffix},
						{"broad_next", workflowBroadURL + suffix +
							"&cursor=" + url.QueryEscape(*broad.NextCursor)},
						{"content", contentURL + "?scope=" + string(scope)},
						{"locations", locationURL},
					}
					if scope == base.ScopeCurrent {
						cases = append(cases, struct{ name, path string }{
							"replica_no_match", workflowNoMatchURL,
						})
					} else {
						var locations observationPageDTO
						f.get(b, locationURL, &locations)
						if len(locations.Items) != 2 || locations.NextCursor == nil {
							b.Fatalf("history locations: %+v", locations)
						}
						cases = append(cases, struct{ name, path string }{
							"locations_next", locationURL +
								"&cursor=" + url.QueryEscape(*locations.NextCursor),
						}, struct{ name, path string }{
							"retired", historyWorkflowRetiredURL + suffix,
						})
					}
					for _, test := range cases {
						b.Run(test.name, func(b *testing.B) {
							request := func() {
								switch test.name {
								case "content":
									var result contentDTO
									f.get(b, test.path, &result)
									assertHTTPHistoryTarget(b, result, scope)
								case "locations", "locations_next":
									var result observationPageDTO
									f.get(b, test.path, &result)
									if len(result.Items) != 2 {
										b.Fatalf("location page: %+v", result)
									}
								default:
									var result searchPageDTO
									f.get(b, test.path, &result)
									want := 1
									if test.name == "broad" || test.name == "broad_next" {
										want = 50
									} else if test.name == "replica_no_match" {
										want = 0
									}
									if len(result.Items) != want || result.Scope != scope ||
										(want == 0 && result.NextCursor != nil) {
										b.Fatalf("search page: %+v", result)
									}
									if test.name == "exact" || test.name == "substring" {
										assertHTTPHistoryTarget(b, result.Items[0].Content, scope)
									}
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
			b.Run("current_complete", func(b *testing.B) {
				f.workflow(b)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					f.workflow(b)
				}
			})
		})
	}
}
