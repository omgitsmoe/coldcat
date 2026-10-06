package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestSnapshotAndReplicaSemantics(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := New(db)
	disks := make([]base.DiskId, 3)
	for i := range disks {
		disks[i], err = a.CreateDisk(ctx, fmt.Sprintf("disk-%d", i), "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
	}

	t1 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	importFixture := func(disk base.DiskId, captured time.Time, input string) base.Snapshot {
		t.Helper()
		file := filepath.Join(t.TempDir(), "fixture.cshd")
		if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}

		s, err := a.Import(ctx, ImportRequest{DiskID: disk, Path: file, CapturedAt: captured})
		if err != nil {
			t.Fatal(err)
		}

		return s
	}
	old := importFixture(
		disks[0],
		t1,
		",sha256,ab photos/x.jpg\n,sha256,ab backup/x.jpg\n,sha256,cd removed\n,sha256,ee changed\n",
	)
	newest := importFixture(
		disks[0],
		t1.Add(48*time.Hour),
		",sha256,ab photos/x.jpg\n,sha256,ab backup/x.jpg\n,sha256,ff changed\n",
	)
	importFixture(disks[1], t1.Add(24*time.Hour), ",sha256,ab copies/x.jpg\n")
	importFixture(disks[0], t1.Add(24*time.Hour), ",sha256,ab older-only.jpg\n")
	latest, err := a.LatestCompleteSnapshot(ctx, disks[0])
	if err != nil {
		t.Fatal(err)
	}

	if latest.Id != newest.Id {
		t.Fatal("later import of older inventory replaced current")
	}

	tied := importFixture(
		disks[0],
		newest.CapturedAt,
		",sha256,ab photos/x.jpg\n1,sha256,ab backup/x.jpg\n,sha256,ff changed\n",
	)
	latest, err = a.LatestCompleteSnapshot(ctx, disks[0])
	if err != nil {
		t.Fatal(err)
	}

	if latest.Id != tied.Id {
		t.Fatal("snapshot ID tie-break failed")
	}

	importFixture(disks[2], t1, ",md5,ab unrelated\n")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	var x, removed base.ContentId
	if err := raw.QueryRow("SELECT id FROM content WHERE hash_type='sha256' AND hash=X'AB'").Scan(&x); err != nil {
		t.Fatal(err)
	}

	if err := raw.QueryRow("SELECT id FROM content WHERE hash=X'CD'").Scan(&removed); err != nil {
		t.Fatal(err)
	}

	if _, err := raw.Exec(`INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance) VALUES(100,?,'importing',?,?,'explicit');
INSERT INTO content(id,hash_type,hash) VALUES(100,'sha256',X'AA');
INSERT INTO observation(id,snapshot_id,content_id,path) VALUES(100,100,?,'unfinished'),(101,100,100,'unfinished-content');`, disks[2], database.FormatTime(t1.Add(96*time.Hour)), database.FormatTime(t1), x); err != nil {
		t.Fatal(err)
	}

	current, err := a.GetContentSummary(ctx, x, base.ScopeCurrent)
	if err != nil {
		t.Fatal(err)
	}

	if current.LocationCount != 3 || current.DiskCount != 2 || current.ObservationCount != 8 ||
		current.Scope != base.ScopeCurrent {
		t.Fatalf("current: %+v", current)
	}

	history, err := a.GetContentSummary(ctx, x, base.ScopeHistory)
	if err != nil {
		t.Fatal(err)
	}

	if history.LocationCount != 4 || history.DiskCount != 2 || history.ObservationCount != 8 ||
		history.CurrentLocationCount != 3 {
		t.Fatalf("history: %+v", history)
	}

	var observation base.FileObservationId
	if err := raw.QueryRow("SELECT id FROM observation WHERE snapshot_id=? AND path='photos/x.jpg'", tied.Id).Scan(&observation); err != nil {
		t.Fatal(err)
	}

	detail, err := a.GetObservationSummary(ctx, observation)
	if err != nil {
		t.Fatal(err)
	}

	if detail.OtherLocationCount != 2 || detail.OtherDiskCount != 1 ||
		detail.Snapshot.Id != tied.Id ||
		detail.Observation.MTime != nil {
		t.Fatalf("observation: %+v", detail)
	}

	if err := raw.QueryRow("SELECT id FROM observation WHERE path='older-only.jpg'").Scan(&observation); err != nil {
		t.Fatal(err)
	}

	detail, err = a.GetObservationSummary(ctx, observation)
	if err != nil {
		t.Fatal(err)
	}

	if detail.OtherLocationCount != 3 || detail.OtherDiskCount != 1 {
		t.Fatalf("absent historical location incorrectly subtracted: %+v", detail)
	}

	if err := raw.QueryRow("SELECT id FROM observation WHERE snapshot_id=? AND path='removed'", old.Id).Scan(&observation); err != nil {
		t.Fatal(err)
	}

	detail, err = a.GetObservationSummary(ctx, observation)
	if err != nil {
		t.Fatal(err)
	}

	if detail.OtherLocationCount != 0 || detail.OtherDiskCount != 0 {
		t.Fatal("historical-only observation has negative replicas")
	}

	for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
		gone, err := a.GetContentSummary(ctx, removed, scope)
		if err != nil {
			t.Fatal(err)
		}

		if gone.CurrentLocationCount != 0 || gone.CurrentDiskCount != 0 ||
			gone.ObservationCount != 1 {
			t.Fatalf("historical-only: %+v", gone)
		}
	}

	for _, id := range []base.SnapshotId{100, 999} {
		if _, err := a.GetCompleteSnapshot(ctx, id); !errors.Is(err, database.ErrNotFound) {
			t.Fatalf("snapshot %d: %v", id, err)
		}
	}

	for _, id := range []base.FileObservationId{100, 101, 999} {
		if _, err := a.GetObservationSummary(ctx, id); !errors.Is(err, database.ErrNotFound) {
			t.Fatalf("observation %d: %v", id, err)
		}
	}

	if _, err := a.GetContentSummary(ctx, 100, base.ScopeHistory); !errors.Is(
		err,
		database.ErrNotFound,
	) {
		t.Fatalf("unfinished content: %v", err)
	}

	if _, err := a.GetContentSummary(ctx, x, "invalid"); !errors.Is(err, database.ErrValidation) {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.GetContentSummary(cancelled, x, base.ScopeCurrent); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal(err)
	}

	importFixture(disks[1], t1.Add(96*time.Hour), "")
	if err := raw.QueryRow("SELECT id FROM observation WHERE snapshot_id=? AND path='photos/x.jpg'", tied.Id).Scan(&observation); err != nil {
		t.Fatal(err)
	}

	detail, err = a.GetObservationSummary(ctx, observation)
	if err != nil {
		t.Fatal(err)
	}

	if detail.OtherLocationCount != 1 || detail.OtherDiskCount != 0 {
		t.Fatalf("same-disk copies counted as other disks: %+v", detail)
	}
}

func TestDirectoryShapedFixtureAndMetadata(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := New(db)
	for i, input := range []string{
		"# version 1\n1,0,sha256,ab source/empty\n,,sha256,cd source/nested/unknown\n,4,sha256,ef source/data\n",
		"# version 1\n2,0,sha256,ab renamed/empty\n,,sha256,cd renamed/nested/unknown\n,4,sha256,ef renamed/data\n,4,sha256,ef extra/data\n,0,sha256,ab extra/empty\n,,sha256,cd extra/nested/unknown\n,1,sha256,aa extra/additional\n",
		"# version 1\n,0,sha256,ab rearranged/nested/empty\n,,sha256,cd partial/unknown\n,4,sha256,ef elsewhere/data\n",
	} {
		disk, err := a.CreateDisk(t.Context(), fmt.Sprint(i), "", "", 100)
		if err != nil {
			t.Fatal(err)
		}

		path := filepath.Join(t.TempDir(), "fixture.cshd")
		if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}

		result, err := a.Import(
			t.Context(),
			ImportRequest{DiskID: disk, Path: path, CapturedAt: time.Unix(1, 0)},
		)
		if err != nil {
			t.Fatal(err)
		}

		if result.FileCount == 0 {
			t.Fatal("empty directory fixture")
		}
	}
}
