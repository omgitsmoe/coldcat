package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycleCheckpoints(t *testing.T) {
	dir := t.TempDir()
	var out, marker bytes.Buffer
	f := fixture{dir: dir, out: &out, marker: &marker}
	if err := f.run(); err != nil {
		t.Fatal(err)
	}

	stages := make(map[string][]checkpoint)
	decoder := json.NewDecoder(&out)
	for {
		var point checkpoint
		if err := decoder.Decode(&point); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		stages[point.Stage] = append(stages[point.Stage], point)
		if point.Inode == 0 || point.StatBlocksBytes < 0 || point.StatBlocksBytes%512 != 0 {
			t.Fatalf("invalid allocation checkpoint: %+v", point)
		}
	}

	for _, stage := range []string{
		"sparse-extended", "sparse-written-buffered", "sparse-written-synced",
		"truncated-synced", "preallocated-synced", "before-unlink",
		"open-unlinked-written-synced", "simultaneous-a", "simultaneous-b",
	} {
		if len(stages[stage]) != 1 {
			t.Fatalf("stage %q: got %d checkpoints", stage, len(stages[stage]))
		}
	}

	sparse := stages["sparse-extended"][0]
	if sparse.LogicalBytes != 16*1024*1024 || sparse.StatBlocksBytes != 0 {
		t.Fatalf("sparse extension allocated data: %+v", sparse)
	}
	truncated := stages["truncated-synced"][0]
	if truncated.LogicalBytes != 0 || truncated.StatBlocksBytes != 0 {
		t.Fatalf("truncated file retained allocation: %+v", truncated)
	}
	before := stages["before-unlink"][0]
	unlinked := stages["open-unlinked-written-synced"][0]
	if before.Inode != unlinked.Inode || before.Links != 1 || unlinked.Links != 0 ||
		unlinked.LogicalBytes != 2*1024*1024 || unlinked.StatBlocksBytes < 2*1024*1024 {
		t.Fatalf("unlinked allocation identity: before=%+v after=%+v", before, unlinked)
	}
	if _, err := os.Stat(unlinked.Path); !os.IsNotExist(err) {
		t.Fatalf("unlinked path lookup: %v", err)
	}
	if len(stages["rapid-written-synced"]) != 16 {
		t.Fatal("missing rapid lifecycle checkpoints")
	}
	if _, err := os.Stat(filepath.Join(dir, "rapid-journal")); !os.IsNotExist(err) {
		t.Fatalf("rapid file remains: %v", err)
	}
	if count := strings.Count(marker.String(), "coldcat-physical "); count != 46 {
		t.Fatalf("got %d trace checkpoints, want 46", count)
	}
}
