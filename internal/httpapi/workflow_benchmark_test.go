package httpapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

const workflowExactURL = "/api/v1/search?q=KEEPSAKE-REPORT.TXT&match=exact&limit=1"
const workflowSubstringURL = "/api/v1/search?q=KEEPSAKE&limit=1"
const workflowBroadURL = "/api/v1/search?q=REPORT&limit=50"
const workflowNoMatchURL = workflowBroadURL + "&replica_metric=disks&other_replicas=3"

type httpWorkflowFixture struct {
	server *httptest.Server
}

func newHTTPWorkflowFixture(tb testing.TB, observations int) httpWorkflowFixture {
	tb.Helper()
	if observations < 104 {
		tb.Fatal("workflow fixture requires at least 104 observations")
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
	for diskIndex := range 3 {
		disk, err := a.CreateDisk(tb.Context(), fmt.Sprintf("disk-%d", diskIndex), "", "", 0)
		if err != nil {
			tb.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("disk-%d.cshd", diskIndex))
		writeHTTPWorkflowInput(tb, path, diskIndex, observations)
		if _, err := a.Import(tb.Context(), app.ImportRequest{
			DiskID: disk, Path: path, CapturedAt: time.Unix(10, 0),
		}); err != nil {
			tb.Fatal(err)
		}
	}
	server := httptest.NewServer(New(a))
	tb.Cleanup(server.Close)
	fixture := httpWorkflowFixture{server: server}
	var catalog catalogDTO
	fixture.get(tb, "/api/v1/catalog", &catalog)
	if catalog.DiskCount != "3" || catalog.FileCount != fmt.Sprint(observations) ||
		catalog.ContentCount != fmt.Sprint(observations-3) {
		tb.Fatalf("fixture catalog: %+v", catalog)
	}
	return fixture
}

func writeHTTPWorkflowInput(tb testing.TB, path string, diskIndex, observations int) {
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
	write := func(hash int, path string) {
		if _, err := fmt.Fprintf(writer, "5,4096,sha256,%064x %s\n", hash, path); err != nil {
			tb.Fatal(err)
		}
	}
	write(0, fmt.Sprintf("disk-%d/keepsake-report.txt", diskIndex))
	if diskIndex == 0 {
		write(0, "backup/keepsake-report.txt")
		for i := range observations - 4 {
			write(i+1, fmt.Sprintf("archive/report-%07d.txt", i))
		}
	}
	if err := writer.Flush(); err != nil {
		tb.Fatal(err)
	}
	if err := file.Close(); err != nil {
		tb.Fatal(err)
	}
}

func (f httpWorkflowFixture) get(tb testing.TB, path string, dst any) {
	tb.Helper()
	request, err := http.NewRequestWithContext(tb.Context(), http.MethodGet, f.server.URL+path, nil)
	if err != nil {
		tb.Fatal(err)
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		tb.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		tb.Fatal(readErr)
	}
	if closeErr != nil {
		tb.Fatal(closeErr)
	}
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("Content-Type") != "application/json" {
		tb.Fatalf("%s: status=%d content-type=%q body=%s", path,
			response.StatusCode, response.Header.Get("Content-Type"), data)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		tb.Fatal(err)
	}
}

func assertWorkflowTarget(tb testing.TB, content contentDTO) {
	tb.Helper()
	if content.ID == "" || content.LocationCount != "4" || content.DiskCount != "3" ||
		content.Hash.Hex != fmt.Sprintf("%064x", 0) {
		tb.Fatalf("unexpected target: %+v", content)
	}
}

func (f httpWorkflowFixture) workflow(tb testing.TB) {
	tb.Helper()
	var search searchPageDTO
	f.get(tb, workflowExactURL, &search)
	if len(search.Items) != 1 || search.NextCursor == nil {
		tb.Fatalf("target search: %+v", search)
	}
	assertWorkflowTarget(tb, search.Items[0].Content)
	var content contentDTO
	contentURL := "/api/v1/contents/" + search.Items[0].Content.ID
	f.get(tb, contentURL, &content)
	assertWorkflowTarget(tb, content)
	if content.ID != search.Items[0].Content.ID {
		tb.Fatal("content identity changed")
	}
	seen := make(map[string]bool)
	path := contentURL + "/observations?limit=2"
	for pages := 0; ; pages++ {
		if pages >= 2 {
			tb.Fatal("unexpected additional location page")
		}
		var locations observationPageDTO
		f.get(tb, path, &locations)
		if len(locations.Items) != 2 {
			tb.Fatalf("locations: %+v", locations)
		}
		for _, item := range locations.Items {
			if seen[item.Observation.ID] || item.Observation.ContentID != content.ID {
				tb.Fatalf("unexpected location: %+v", item)
			}
			seen[item.Observation.ID] = true
		}
		if locations.NextCursor == nil {
			break
		}
		path = contentURL + "/observations?limit=2&cursor=" +
			url.QueryEscape(*locations.NextCursor)
	}
	if len(seen) != 4 {
		tb.Fatalf("location count: %d", len(seen))
	}
}

func TestHTTPWorkflowBenchmarkFixture(t *testing.T) {
	f := newHTTPWorkflowFixture(t, 104)
	f.workflow(t)
	for _, path := range []string{workflowExactURL, workflowSubstringURL} {
		var first, next searchPageDTO
		f.get(t, path, &first)
		if len(first.Items) != 1 || first.NextCursor == nil {
			t.Fatalf("target page: %+v", first)
		}
		assertWorkflowTarget(t, first.Items[0].Content)
		f.get(t, path+"&cursor="+url.QueryEscape(*first.NextCursor), &next)
		if len(next.Items) != 1 || next.Items[0].Observation.ID == first.Items[0].Observation.ID {
			t.Fatalf("search pagination: %+v", next)
		}
	}
	var broad, next, empty searchPageDTO
	f.get(t, workflowBroadURL, &broad)
	if len(broad.Items) != 50 || broad.NextCursor == nil {
		t.Fatalf("broad page: %+v", broad)
	}
	f.get(t, workflowBroadURL+"&cursor="+url.QueryEscape(*broad.NextCursor), &next)
	seen := make(map[string]bool)
	for _, item := range broad.Items {
		seen[item.Observation.ID] = true
	}
	if len(next.Items) != 50 {
		t.Fatalf("broad next page: %+v", next)
	}
	for _, item := range next.Items {
		if seen[item.Observation.ID] {
			t.Fatal("broad pagination repeated observation")
		}
	}
	f.get(t, workflowNoMatchURL, &empty)
	if len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatalf("no-match page: %+v", empty)
	}
}

func BenchmarkHTTPWorkflow(b *testing.B) {
	for _, observations := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(observations), func(b *testing.B) {
			f := newHTTPWorkflowFixture(b, observations)
			f.workflow(b)
			var target, broad searchPageDTO
			f.get(b, workflowExactURL, &target)
			f.get(b, workflowBroadURL, &broad)
			if len(broad.Items) != 50 || broad.NextCursor == nil {
				b.Fatalf("broad page: %+v", broad)
			}
			contentURL := "/api/v1/contents/" + target.Items[0].Content.ID
			cases := []struct {
				name string
				path string
			}{
				{"exact", workflowExactURL},
				{"substring", workflowSubstringURL},
				{"broad", workflowBroadURL},
				{"broad_next", workflowBroadURL + "&cursor=" + url.QueryEscape(*broad.NextCursor)},
				{"replica_no_match", workflowNoMatchURL},
				{"content", contentURL},
				{"locations", contentURL + "/observations?limit=2"},
			}
			for _, test := range cases {
				b.Run(test.name, func(b *testing.B) {
					request := func() {
						switch test.name {
						case "content":
							var result contentDTO
							f.get(b, test.path, &result)
						case "locations":
							var result observationPageDTO
							f.get(b, test.path, &result)
						default:
							var result searchPageDTO
							f.get(b, test.path, &result)
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
			b.Run("complete", func(b *testing.B) {
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
