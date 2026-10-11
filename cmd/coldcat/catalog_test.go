package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestCatalogMaintenanceCommands(t *testing.T) {
	path, _ := importFixture(t, 0)
	output := filepath.Join(t.TempDir(), "compacted.sqlite")
	for _, args := range [][]string{
		{"stats"}, {"stats", "--json"}, {"check"}, {"compact", "--output", output},
	} {
		cmd := newCommand()
		var stdout bytes.Buffer
		cmd.Writer = &stdout
		if err := cmd.Run(t.Context(), append([]string{"coldcat", "--db", path, "catalog"},
			args...)); err != nil {
			t.Fatal(err)
		}
		if len(args) > 1 && args[1] == "--json" {
			var report database.StorageReport
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report.PageCount == 0 {
				t.Fatalf("stats JSON: %s, %v", stdout.String(), err)
			}
		} else if stdout.Len() == 0 {
			t.Fatal("missing maintenance output")
		}
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogMaintenanceRequiresExistingSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	err := newCommand().Run(t.Context(), []string{"coldcat", "--db", path, "catalog", "stats"})
	if err == nil || !strings.Contains(err.Error(), "existing catalog required") {
		t.Fatalf("missing source: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("created missing source: %v", err)
	}
}
