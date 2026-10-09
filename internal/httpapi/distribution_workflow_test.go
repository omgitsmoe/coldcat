package httpapi

import (
	"fmt"
	"net/url"
	"path"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

type httpDistributionExpected struct {
	hash     string
	snapshot httpHistorySnapshot
}

type httpDistributionCopies struct {
	observations int
	current      int
	locations    map[string]bool
	disks        map[string]bool
	currentDisks map[string]bool
}

// The oracle deliberately does not call the input generator: wrong record identities
// or inventory sizes must fail even when HTTP faithfully returns the imported data.
func distributionExpectations(f httpDistributionFixture) (
	map[string]httpDistributionExpected, map[string]*httpDistributionCopies,
) {
	p := f.profile
	want := make(map[string]httpDistributionExpected)
	copies := make(map[string]*httpDistributionCopies)
	for _, snapshot := range f.snapshots {
		n := p.current / p.disks
		if snapshot.diskIndex < p.current%p.disks {
			n++
		}
		for slot := 0; slot < n; slot++ {
			disk, generation := snapshot.diskIndex, snapshot.generation
			identity := 100 + disk*p.current + slot
			name := fmt.Sprintf("disk-%02d/archive/report-%07d.txt", disk, slot)
			if slot == 0 || slot == 1 {
				identity = 0
				name = fmt.Sprintf("disk-%02d/copy-%d/keepsake-report.txt", disk, slot)
			} else if slot == 4 && generation != p.snapshots-1 {
				identity = 100 + p.current*p.disks*(p.snapshots+1) + disk
				name = fmt.Sprintf("disk-%02d/archive/retired-report.txt", disk)
			} else if slot == 3 || slot >= 5 && slot%5 == 0 {
				identity += (generation + 1) * p.current * p.disks
			}
			hash := fmt.Sprintf("%064x", identity)
			want[historyObservationKey(snapshot.id, name)] = httpDistributionExpected{hash, snapshot}
			if copies[hash] == nil {
				copies[hash] = &httpDistributionCopies{
					locations: make(map[string]bool), disks: make(map[string]bool),
					currentDisks: make(map[string]bool),
				}
			}
			c := copies[hash]
			c.observations++
			c.locations[historyObservationKey(snapshot.disk, name)] = true
			c.disks[snapshot.disk] = true
			if generation == p.snapshots-1 {
				c.current++
				c.currentDisks[snapshot.disk] = true
			}
		}
	}
	return want, copies
}

func assertDistributionCopies(
	tb testing.TB, content contentDTO, scope base.Scope, c *httpDistributionCopies,
) {
	tb.Helper()
	locations, disks := c.current, len(c.currentDisks)
	if scope == base.ScopeHistory {
		locations, disks = len(c.locations), len(c.disks)
	}
	if content.Scope != scope || content.Hash.Algorithm != "sha256" ||
		content.Size == nil || *content.Size != "4096" ||
		content.LocationCount != fmt.Sprint(locations) || content.DiskCount != fmt.Sprint(disks) ||
		content.ObservationCount != fmt.Sprint(c.observations) ||
		content.CurrentLocationCount != fmt.Sprint(c.current) ||
		content.CurrentDiskCount != fmt.Sprint(len(c.currentDisks)) {
		tb.Fatalf("distribution copies: %+v, want %+v", content, c)
	}
}

func (f httpDistributionFixture) assertSearch(
	tb testing.TB, query string, scope base.Scope, want map[string]httpDistributionExpected,
	copies map[string]*httpDistributionCopies,
) []searchItemDTO {
	tb.Helper()
	remaining := make(map[string]httpDistributionExpected, len(want))
	for key, value := range want {
		remaining[key] = value
	}
	var items []searchItemDTO
	seen := make(map[string]bool)
	contentIDs := make(map[string]string)
	requestPath := query
	for pages := 0; ; pages++ {
		if pages > len(want) {
			tb.Fatal("distribution search pagination did not terminate")
		}
		var page searchPageDTO
		f.get(tb, requestPath, &page)
		if page.Scope != scope {
			tb.Fatalf("distribution search scope: %s", page.Scope)
		}
		for _, item := range page.Items {
			key := historyObservationKey(item.Snapshot.ID, item.Observation.Path)
			expected, ok := remaining[key]
			snapshot := expected.snapshot
			if !ok || seen[item.Observation.ID] || item.Content.Hash.Hex != expected.hash ||
				item.IsCurrent != (snapshot.generation == f.profile.snapshots-1) ||
				item.Disk.ID != snapshot.disk || item.Snapshot.DiskID != snapshot.disk ||
				item.Observation.SnapshotID != snapshot.id || item.Snapshot.State != "complete" ||
				item.Snapshot.CapturedAt != timestamp(time.Unix(int64(10+snapshot.generation), 0)) ||
				item.Observation.ContentID != item.Content.ID ||
				item.Basename != path.Base(item.Observation.Path) {
				tb.Fatalf("unexpected distribution observation: %+v", item)
			}
			assertDistributionCopies(tb, item.Content, scope, copies[expected.hash])
			if id, exists := contentIDs[expected.hash]; exists && id != item.Content.ID {
				tb.Fatalf("distribution content identity changed: %s", expected.hash)
			}
			contentIDs[expected.hash] = item.Content.ID
			seen[item.Observation.ID] = true
			delete(remaining, key)
			items = append(items, item)
		}
		if page.NextCursor == nil {
			break
		}
		if len(page.Items) == 0 {
			tb.Fatal("empty distribution search page has a continuation cursor")
		}
		requestPath = query + "&cursor=" + url.QueryEscape(*page.NextCursor)
	}
	if len(remaining) != 0 {
		tb.Fatalf("distribution search omitted %d observations", len(remaining))
	}
	return items
}

func TestHTTPDistributionWorkflowFixture(t *testing.T) {
	for _, disks := range []int{3, 12} {
		for _, snapshots := range []int{1, 5} {
			t.Run(fmt.Sprintf("disks%d/snapshots%d", disks, snapshots), func(t *testing.T) {
				p := httpDistributionProfile{current: disks*8 + 1, disks: disks, snapshots: snapshots}
				f := newHTTPDistributionFixture(t, p)
				all, copies := distributionExpectations(f)
				if len(all) != p.current*p.snapshots {
					t.Fatalf("expected inventory size: %d", len(all))
				}
				for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
					t.Run(string(scope), func(t *testing.T) {
						want := make(map[string]httpDistributionExpected)
						targets := make(map[string]httpDistributionExpected)
						retired := make(map[string]httpDistributionExpected)
						for key, expected := range all {
							if scope == base.ScopeCurrent && expected.snapshot.generation != p.snapshots-1 {
								continue
							}
							want[key] = expected
							if expected.hash == fmt.Sprintf("%064x", 0) {
								targets[key] = expected
							}
							if path.Base(key) == "retired-report.txt" {
								retired[key] = expected
							}
						}
						suffix := "&scope=" + string(scope)
						f.primary(t, scope)
						f.assertSearch(t, "/api/v1/search?q=REPORT&limit=7"+suffix, scope, want, copies)
						matches := f.assertSearch(t, workflowExactURL+suffix, scope, targets, copies)
						f.assertTarget(t, matches[0].Content, scope)
						contentURL := "/api/v1/contents/" + matches[0].Content.ID
						var content contentDTO
						f.get(t, contentURL+"?scope="+string(scope), &content)
						f.assertTarget(t, content, scope)
						assertHTTPHistoryObservations(t, httpHistoryWorkflowFixture{
							httpWorkflowFixture: f.httpWorkflowFixture,
						}, content.ID, scope, matches)
						f.assertSearch(t, historyWorkflowRetiredURL+suffix, scope, retired, copies)
					})
				}
				for _, metric := range []string{"disks", "locations"} {
					bound := p.disks
					if metric == "locations" {
						bound *= 2
					}
					query := workflowBroadURL + "&replica_metric=" + metric + "&other_replicas="
					f.assertSearch(t, query+fmt.Sprint(bound), base.ScopeCurrent,
						map[string]httpDistributionExpected{}, copies)
					for _, hash := range []int{0, 102} {
						c := copies[fmt.Sprintf("%064x", hash)]
						other := len(c.currentDisks) - 1
						if metric == "locations" {
							other = c.current - 1
						}
						want := make(map[string]httpDistributionExpected)
						for key, expected := range all {
							ec := copies[expected.hash]
							n := len(ec.currentDisks) - 1
							if metric == "locations" {
								n = ec.current - 1
							}
							if expected.snapshot.generation == p.snapshots-1 && n == other {
								want[key] = expected
							}
						}
						f.assertSearch(t, query+fmt.Sprint(other), base.ScopeCurrent, want, copies)
					}
				}
			})
		}
	}
}
