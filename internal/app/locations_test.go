package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestContentLocationsAndCursorLifetime(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { db.Close() })
	a := New(db)
	initial, err := a.GetCatalogState(ctx)
	if err != nil || initial.Revision != 0 {
		t.Fatalf("initial: %+v %v", initial, err)
	}

	disk, err := a.CreateDisk(ctx, "source", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}

	importFiles := func(input string, captured int64) base.Snapshot {
		t.Helper()
		file := filepath.Join(t.TempDir(), "fixture.cshd")
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}

		s, err := a.Import(
			ctx,
			ImportRequest{DiskID: disk, Path: file, CapturedAt: time.Unix(captured, 0)},
		)
		if err != nil {
			t.Fatal(err)
		}

		return s
	}
	first := importFiles(",sha256,"+fixtureSHA256AB+" zéro/a\n"+
		",sha256,"+fixtureSHA256AB+" backup/a\n,sha256,"+fixtureSHA256CD+" gone\n", 10)
	content, err := a.LookupContent(ctx, LookupContentRequest{
		HashType: "sha256", Hash: strings.ToUpper(fixtureSHA256AB),
	})
	if err != nil || content.LocationCount != 2 {
		t.Fatalf("lookup: %+v %v", content, err)
	}

	req := ListContentObservationsRequest{ContentID: content.Content.Id, Limit: 1}
	page, err := a.ListContentObservations(ctx, req)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" ||
		page.Catalog.Revision != first.Id {
		t.Fatalf("first page: %+v %v", page, err)
	}

	req.Cursor = page.NextCursor
	if _, err := a.CreateDisk(ctx, "empty", "", "", 0); err != nil {
		t.Fatal(err)
	}

	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	a = New(db)
	next, err := a.ListContentObservations(ctx, req)
	if err != nil || len(next.Items) != 1 ||
		next.Items[0].Observation.Id <= page.Items[0].Observation.Id ||
		next.NextCursor != "" ||
		next.Catalog.Revision != page.Catalog.Revision {
		t.Fatalf("restart page: %+v %v", next, err)
	}

	bad := req
	bad.Scope = base.ScopeHistory
	if _, err := a.ListContentObservations(ctx, bad); !errors.Is(err, database.ErrValidation) {
		t.Fatalf("context mismatch: %v", err)
	}

	file := filepath.Join(t.TempDir(), "bad.cshd")
	for _, invalid := range []string{"invalid\n", ",sha256,ab invalid-hash\n"} {
		input := ",sha256," + fixtureSHA256AB + " temporary\n" + invalid
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := a.Import(ctx, ImportRequest{
			DiskID: disk, Path: file, CapturedAt: time.Unix(20, 0),
		})
		if !errors.Is(err, database.ErrValidation) {
			t.Fatalf("invalid import: %v", err)
		}

		if _, err := a.ListContentObservations(ctx, req); err != nil {
			t.Fatalf("failed import invalidated cursor: %v", err)
		}
	}

	importFiles("", 20)
	if _, err := a.ListContentObservations(ctx, req); !errors.Is(err, database.ErrStaleCursor) {
		t.Fatalf("stale cursor: %v", err)
	}

	req.Cursor = ""
	current, err := a.ListContentObservations(ctx, req)
	if err != nil || len(current.Items) != 0 {
		t.Fatalf("historical-only current list: %+v %v", current, err)
	}

	req.Scope = base.ScopeHistory
	history, err := a.ListContentObservations(ctx, req)
	if err != nil || len(history.Items) != 1 || history.Items[0].IsCurrent {
		t.Fatalf("history: %+v %v", history, err)
	}

	state, err := a.GetCatalogState(ctx)
	if err != nil {
		t.Fatal(err)
	}

	importFiles(",sha256,"+fixtureSHA256AB+" older\n", 5)
	updated, err := a.GetCatalogState(ctx)
	if err != nil || updated.Revision <= state.Revision {
		t.Fatalf("older import revision: %+v %v", updated, err)
	}

	latest, err := a.LatestCompleteSnapshot(ctx, disk)
	if err != nil || latest.CapturedAt.Unix() != 20 {
		t.Fatalf("older import replaced latest: %+v %v", latest, err)
	}
}
