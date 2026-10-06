package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestGroupedDiskAndSnapshotCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	run := func(args ...string) (string, string, error) {
		t.Helper()
		cmd := newCommand()
		var stdout, stderr bytes.Buffer
		cmd.Writer, cmd.ErrWriter = &stdout, &stderr
		err := cmd.Run(t.Context(), append([]string{"coldcat", "--db", path}, args...))
		return stdout.String(), stderr.String(), err
	}
	if stdout, stderr, err := run("disk", "list", "--json"); err != nil || stdout != "[]\n" || stderr != "" {
		t.Fatalf("empty: %q %q %v", stdout, stderr, err)
	}
	if stdout, stderr, err := run("disk", "create", "--label", "archive α", "--capacity", "1TB",
		"--notes", "notes", "--serial", "serial"); err != nil || stdout != "created disk 1\n" || stderr != "" {
		t.Fatalf("grouped create: %q %q %v", stdout, stderr, err)
	}
	if stdout, stderr, err := run("create", "--label", "legacy", "--capacity", "0"); err != nil || stdout != "created disk 2\n" || stderr != "" {
		t.Fatalf("legacy create: %q %q %v", stdout, stderr, err)
	}
	stdout, stderr, err := run("disk", "list", "--json")
	var disks []diskListItem
	if err != nil || stderr != "" || json.Unmarshal([]byte(stdout), &disks) != nil || len(disks) != 2 ||
		disks[0].Label != "archive α" || disks[0].Capacity != "1000000000000" ||
		disks[0].Notes == nil || *disks[0].Notes != "notes" || disks[0].Serial == nil ||
		disks[0].LatestSnapshot != nil {
		t.Fatalf("disk JSON: %q %q %v", stdout, stderr, err)
	}
	if stdout, _, err := run("disk", "list"); err != nil ||
		!strings.Contains(stdout, `"archive α"`) || !strings.Contains(stdout, "no inventory") {
		t.Fatalf("disk text: %q %v", stdout, err)
	}
	if stdout, stderr, err := run("snapshot", "list", "--disk-id", "1", "--json"); err != nil || stdout != "[]\n" || stderr != "" {
		t.Fatalf("empty snapshots: %q %q %v", stdout, stderr, err)
	}
	file := filepath.Join(t.TempDir(), "inventory.cshd")
	if err := os.WriteFile(file, []byte(",sha256,"+fixtureSHA256AB+" file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run("import", "--disk-id", "1", "--captured-at",
		"2023-01-01T02:00:00+02:00", file); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = run("snapshot", "list", "--disk-id", "1", "--json")
	var snapshots []snapshotListItem
	if err != nil || stderr != "" || json.Unmarshal([]byte(stdout), &snapshots) != nil ||
		len(snapshots) != 1 || snapshots[0].ID != "1" || snapshots[0].FileCount != "1" ||
		snapshots[0].CapturedAt != "2023-01-01T00:00:00Z" {
		t.Fatalf("snapshot JSON: %q %q %v", stdout, stderr, err)
	}
	if stdout, _, err := run("snapshot", "list", "--disk-id", "1"); err != nil ||
		!strings.Contains(stdout, "2023-01-01T00:00:00Z\t1 files\t1 contents") {
		t.Fatalf("snapshot text: %q %v", stdout, err)
	}
	stdout, _, err = run("disk", "list", "--json")
	if err != nil || json.Unmarshal([]byte(stdout), &disks) != nil ||
		disks[0].LatestSnapshot == nil || disks[0].LatestSnapshot.ID != "1" {
		t.Fatalf("latest snapshot: %q %v", stdout, err)
	}
	for _, args := range [][]string{
		{"disk", "create", "--label", "bad"},
		{"disk", "create", "--label", "", "--capacity", "0"},
		{"disk", "create", "--label", "bad", "--capacity", "-1"},
		{"disk", "create", "--label", "archive α", "--capacity", "0"},
		{"snapshot", "list"}, {"snapshot", "list", "--disk-id", "0"},
		{"snapshot", "list", "--disk-id", "999", "--json"},
	} {
		if _, _, err := run(args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestCLIListsAllPagesAndWriterErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := app.New(db)
	for i := range 201 {
		if _, err := a.CreateDisk(t.Context(), fmt.Sprintf("disk-%03d", i), "", "", 0); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(t.TempDir(), "empty.cshd")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for i := range 201 {
		if _, err := a.Import(t.Context(), app.ImportRequest{
			DiskID: 1, Path: file, CapturedAt: time.Unix(int64(i+1), 0),
		}); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	for _, args := range [][]string{
		{"disk", "list", "--json"}, {"snapshot", "list", "--disk-id", "1", "--json"},
	} {
		cmd := newCommand()
		var stdout, stderr bytes.Buffer
		cmd.Writer, cmd.ErrWriter = &stdout, &stderr
		if err := cmd.Run(t.Context(), append([]string{"coldcat", "--db", path}, args...)); err != nil {
			t.Fatal(err)
		}
		var values []map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &values); err != nil || len(values) != 201 ||
			stderr.Len() != 0 {
			t.Fatalf("all pages: length %d stderr %q error %v", len(values), stderr.String(), err)
		}
		if args[0] == "disk" && (values[0]["id"] != "1" || values[200]["id"] != "201") {
			t.Fatalf("disk order: %v %v", values[0], values[200])
		}
		if args[0] == "snapshot" && (values[0]["id"] != "201" || values[200]["id"] != "1") {
			t.Fatalf("snapshot order: %v %v", values[0], values[200])
		}
	}
	for _, args := range [][]string{
		{"disk", "list"}, {"disk", "list", "--json"},
		{"snapshot", "list", "--disk-id", "1"},
		{"snapshot", "list", "--disk-id", "1", "--json"},
		{"disk", "create", "--label", "writer", "--capacity", "0"},
	} {
		cmd := newCommand()
		cmd.Writer = diskFailingWriter{}
		if err := cmd.Run(t.Context(), append([]string{"coldcat", "--db", path}, args...)); !errors.Is(err, errDiskWriter) {
			t.Fatalf("writer error %v: %v", args, err)
		}
	}
}

var errDiskWriter = errors.New("writer failed")

type diskFailingWriter struct{}

func (diskFailingWriter) Write([]byte) (int, error) { return 0, errDiskWriter }
