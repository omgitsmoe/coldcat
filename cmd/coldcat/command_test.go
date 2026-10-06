package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/database"
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

func TestCommandDuplicateImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	file := filepath.Join(t.TempDir(), "inventory.cshd")
	if err := os.WriteFile(file, []byte(",sha256,ab file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, string, error) {
		t.Helper()
		cmd := newCommand()
		var stdout, stderr bytes.Buffer
		cmd.Writer, cmd.ErrWriter = &stdout, &stderr
		err := cmd.Run(t.Context(), append([]string{"coldcat", "--db", path}, args...))
		return stdout.String(), stderr.String(), err
	}
	if _, _, err := run("create", "--label", "disk", "--capacity", "1TB"); err != nil {
		t.Fatal(err)
	}
	args := []string{"import", "--label", "disk", "--captured-at", "2023-01-01T00:00:00Z", file}
	if _, _, err := run(args...); err != nil {
		t.Fatal(err)
	}
	for _, selector := range [][]string{{"--label", "disk"}, {"--disk-id", "1"}} {
		args := append([]string{"import"}, selector...)
		args = append(args, "--captured-at", "2023-01-01T00:00:00Z", file)
		stdout, _, err := run(args...)
		var duplicate *database.DuplicateImportError
		if stdout != "" || !errors.As(err, &duplicate) || duplicate.SnapshotID != 1 || !strings.Contains(err.Error(), "--allow-repeat") {
			t.Fatalf("duplicate: stdout=%q err=%v", stdout, err)
		}
		args = append([]string{"import", "--allow-repeat"}, args[1:]...)
		if stdout, stderr, err := run(args...); err != nil || stderr != "" || !strings.Contains(stdout, "complete, 1 files, 1 contents") {
			t.Fatalf("explicit repeat: stdout=%q stderr=%q err=%v", stdout, stderr, err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestDuplicateImportProcessHelper$")
	child.Env = append(os.Environ(), "COLDCAT_TEST_DUPLICATE_DB="+path, "COLDCAT_TEST_DUPLICATE_FILE="+file)
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	if err := child.Run(); err == nil || child.ProcessState == nil || child.ProcessState.ExitCode() != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "inventory already imported as snapshot 1") {
		t.Fatalf("duplicate process: stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
	}
}

func TestDuplicateImportProcessHelper(t *testing.T) {
	path := os.Getenv("COLDCAT_TEST_DUPLICATE_DB")
	if path == "" {
		return
	}
	os.Args = []string{"coldcat", "--db", path, "import", "--disk-id", "1", "--captured-at", "2023-01-01T00:00:00Z", os.Getenv("COLDCAT_TEST_DUPLICATE_FILE")}
	main()
	os.Exit(0)
}
