package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupCacheOverlay(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "database", "imports.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source, err := overlaySource(string(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"cleanupImportTxForBenchmark(ctx, tx, id)",
		"CleanupImportWithCacheForBenchmark",
		"db.db.SetMaxOpenConns(1)",
		"conn.BeginTx(ctx, nil)",
		"tx.ExecContext(ctx, fmt.Sprintf(\"PRAGMA cache_size=%d\", expected))",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("missing overlay invariant %q", required)
		}
	}
	changed := strings.Replace(string(data),
		"func (db *DB) CleanupImport(", "func (db *DB) CleanupChanged(", 1)
	if _, err := overlaySource(changed); err == nil {
		t.Fatal("changed cleanup anchor accepted")
	}
}
