package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandCaptureTimeAndDatabasePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.sqlite")
	file := filepath.Join(t.TempDir(), "inventory.cshd")
	if err := os.WriteFile(file, []byte(",sha256,ab file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		cmd := newCommand()
		var output bytes.Buffer
		cmd.Writer = &output
		cmd.ErrWriter = &output
		err := cmd.Run(context.Background(), append([]string{"coldcat", "--db", path}, args...))
		return output.String(), err
	}
	if output, err := run("create", "--label", "archive", "--capacity", "1TB"); err != nil || !strings.Contains(output, "created disk") {
		t.Fatalf("create: %s %v", output, err)
	}
	for _, args := range [][]string{
		{"import", "--label", "archive", file},
		{"import", "--label", "archive", "--captured-at", "bad", file},
		{"import", "--label", "archive", "--captured-at", "2023-01-01T00:00:00Z", "--use-source-mtime", file},
		{"import", "--label", "archive", "--use-source-mtime=false", file},
	} {
		if _, err := run(args...); err == nil {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
	for _, args := range [][]string{
		{"import", "--label", "archive", "--captured-at", "2023-01-01T00:00:00Z", file},
		{"import", "--disk-id", "1", "--use-source-mtime", file},
	} {
		if output, err := run(args...); err != nil || !strings.Contains(output, "complete, 1 files, 1 contents") {
			t.Fatalf("import: %s %v", output, err)
		}
	}
}
