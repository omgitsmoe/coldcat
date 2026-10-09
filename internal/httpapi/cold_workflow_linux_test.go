//go:build linux

package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type coldHTTPWorkflow struct {
	httpWorkflowFixture
	path    string
	mu      sync.Mutex
	db      *database.DB
	handler http.Handler
}

func newColdHTTPWorkflow(tb testing.TB, observations int) *coldHTTPWorkflow {
	tb.Helper()
	if observations < 104 {
		tb.Fatal("cold workflow fixture requires at least 104 observations")
	}
	dir := tb.TempDir()
	f := &coldHTTPWorkflow{path: filepath.Join(dir, "catalog.sqlite")}
	db, err := database.OpenContext(tb.Context(), f.path)
	if err != nil {
		tb.Fatal(err)
	}
	f.db = db
	tb.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.db != nil {
			if err := f.db.Close(); err != nil {
				tb.Error(err)
			}
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
	f.handler = New(a)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.db == nil {
			db, err := database.OpenContext(r.Context(), f.path)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			f.db = db
			f.handler = New(app.New(db))
		}
		f.handler.ServeHTTP(w, r)
	}))
	tb.Cleanup(f.server.Close)
	var catalog catalogDTO
	f.get(tb, "/api/v1/catalog", &catalog)
	if catalog.DiskCount != "3" || catalog.FileCount != fmt.Sprint(observations) ||
		catalog.ContentCount != fmt.Sprint(observations-3) {
		tb.Fatalf("cold fixture catalog: %+v", catalog)
	}
	f.workflow(tb)
	return f
}

func (f *coldHTTPWorkflow) reset() (catalogResidency, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.db == nil {
		return catalogResidency{}, fmt.Errorf("cold reset requires an open catalog")
	}
	if err := f.db.Close(); err != nil {
		return catalogResidency{}, err
	}
	f.db, f.handler = nil, nil
	// A normal SQLite close removes rollback journals. Refuse any surviving
	// journal/WAL rather than measure cold main-file data with warm sidecars.
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if _, err := os.Stat(f.path + suffix); err == nil {
			return catalogResidency{}, fmt.Errorf("catalog sidecar survives close: %s", f.path+suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			return catalogResidency{}, err
		}
	}
	return evictCatalogFile(f.path)
}

type coldHTTPCase struct {
	name string
	path string
	want any
}

func (f *coldHTTPWorkflow) cases(tb testing.TB) []coldHTTPCase {
	tb.Helper()
	var target, broad searchPageDTO
	f.get(tb, workflowExactURL, &target)
	f.get(tb, workflowBroadURL, &broad)
	if len(target.Items) != 1 || target.NextCursor == nil ||
		len(broad.Items) != 50 || broad.NextCursor == nil {
		tb.Fatal("missing workflow search continuation")
	}
	assertWorkflowTarget(tb, target.Items[0].Content)
	contentURL := "/api/v1/contents/" + target.Items[0].Content.ID
	locationsURL := contentURL + "/observations?limit=2"
	var locations observationPageDTO
	f.get(tb, locationsURL, &locations)
	if len(locations.Items) != 2 || locations.NextCursor == nil {
		tb.Fatal("missing workflow location continuation")
	}
	cases := []coldHTTPCase{
		{name: "exact", path: workflowExactURL},
		{name: "substring", path: workflowSubstringURL},
		{name: "broad", path: workflowBroadURL},
		{name: "replica_no_match", path: workflowNoMatchURL},
		{name: "exact_next", path: workflowExactURL +
			"&cursor=" + url.QueryEscape(*target.NextCursor)},
		{name: "broad_next", path: workflowBroadURL +
			"&cursor=" + url.QueryEscape(*broad.NextCursor)},
		{name: "content", path: contentURL},
		{name: "locations", path: locationsURL},
		{name: "locations_next", path: locationsURL +
			"&cursor=" + url.QueryEscape(*locations.NextCursor)},
	}
	for i := range cases {
		f.get(tb, cases[i].path, &cases[i].want)
	}
	var empty searchPageDTO
	f.get(tb, workflowNoMatchURL, &empty)
	if len(empty.Items) != 0 || empty.NextCursor != nil {
		tb.Fatalf("replica no-match: %+v", empty)
	}
	return cases
}

func TestHTTPColdWorkflow(t *testing.T) {
	f := newColdHTTPWorkflow(t, 104)
	for _, test := range f.cases(t) {
		t.Run(test.name, func(t *testing.T) {
			if _, err := f.reset(); err != nil {
				t.Fatal(err)
			}
			if _, err := f.reset(); err == nil {
				t.Fatal("accepted reset without an open catalog")
			}
			var got any
			f.get(t, test.path, &got)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("cold response differs from validated warm response: %s", test.path)
			}
		})
	}
}

func TestHTTPColdWorkflowRejectsSidecar(t *testing.T) {
	f := newColdHTTPWorkflow(t, 104)
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.path+"-wal", []byte("not a checkpointed catalog"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.reset(); err == nil {
		t.Fatal("accepted a surviving WAL")
	}
}

func TestHTTPColdWorkflowOpenFailure(t *testing.T) {
	f := newColdHTTPWorkflow(t, 104)
	if _, err := f.reset(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.path, []byte("not a SQLite catalog"), 0600); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		f.server.URL+workflowExactURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("catalog open failure returned %d", response.StatusCode)
	}
}

// The request includes catalog open/recovery: opening outside the timer would
// warm schema and recovery pages before the allegedly cold HTTP query.
func BenchmarkHTTPCatalogColdOpen(b *testing.B) {
	for _, observations := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(observations), func(b *testing.B) {
			f := newColdHTTPWorkflow(b, observations)
			for _, test := range f.cases(b) {
				b.Run(test.name, func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					var pages int
					for b.Loop() {
						b.StopTimer()
						residency, err := f.reset()
						if err != nil {
							b.Fatal(err)
						}
						pages = residency.pages
						var got any
						b.StartTimer()
						f.get(b, test.path, &got)
						b.StopTimer()
						if !reflect.DeepEqual(got, test.want) {
							b.Fatal("cold response differs from validated warm response")
						}
						b.StartTimer()
					}
					b.ReportMetric(float64(pages), "catalog-pages")
					b.ReportMetric(0, "pre-open-resident-pages")
				})
			}
		})
	}
}
