package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/database"
	"github.com/omgitsmoe/coldcat/internal/importer"
)

func importFixture(t *testing.T, n int) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateDisk("disk", "", "", 0); err != nil {
		t.Fatal(err)
	}
	db.Close()
	file := filepath.Join(t.TempDir(), "inventory.cshd")
	var input strings.Builder
	for i := range n {
		fmt.Fprintf(&input, ",sha256,"+fixtureSHA256AB+" file-%d\n", i)
	}
	if err := os.WriteFile(file, []byte(input.String()), 0600); err != nil {
		t.Fatal(err)
	}
	return path, file
}

func importArgs(path, file string, extra ...string) []string {
	args := []string{"coldcat", "--db", path, "import"}
	args = append(args, extra...)
	return append(args, "--captured-at", "2023-01-01T00:00:00Z", file)
}

func TestImportOutputContract(t *testing.T) {
	for _, n := range []int{0, 1, 2501} {
		for _, jsonMode := range []bool{false, true} {
			for _, selector := range []string{"--disk-id", "--label"} {
				t.Run(fmt.Sprintf("%d/json=%v/%s", n, jsonMode, selector), func(t *testing.T) {
					path, file := importFixture(t, n)
					value := "1"
					if selector == "--label" {
						value = "disk"
					}
					extra := []string{selector, value}
					if jsonMode {
						extra = append(extra, "--json")
					}
					cmd := newCommand()
					var stdout, stderr bytes.Buffer
					cmd.Writer, cmd.ErrWriter = &stdout, &stderr
					if err := cmd.Run(t.Context(), importArgs(path, file, extra...)); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(stderr.String(), fmt.Sprintf("directories: %d files", n)) ||
						!strings.Contains(stderr.String(), fmt.Sprintf("publishing: %d files", n)) {
						t.Fatalf("progress: %q", stderr.String())
					}
					if jsonMode {
						var result importResult
						decoder := json.NewDecoder(&stdout)
						if err := decoder.Decode(&result); err != nil {
							t.Fatal(err)
						}
						if result.Snapshot.FileCount != strconv.Itoa(n) ||
							result.Snapshot.ContentCount != strconv.Itoa(min(n, 1)) ||
							result.Snapshot.State != "complete" || result.Snapshot.DiskID != "1" ||
							result.Snapshot.CaptureProvenance != "explicit" {
							t.Fatalf("result: %+v", result)
						}
						if ms, err := strconv.ParseInt(result.ElapsedMS, 10, 64); err != nil || ms < 0 {
							t.Fatalf("elapsed: %q", result.ElapsedMS)
						}
						if err := decoder.Decode(new(any)); err != io.EOF {
							t.Fatalf("extra output: %v", err)
						}
					} else if !strings.Contains(stdout.String(), "elapsed ") ||
						!strings.HasPrefix(stdout.String(), "imported snapshot 1: complete,") {
						t.Fatalf("stdout: %q", stdout.String())
					}
				})
			}
		}
	}
}

func TestImportReporterThrottle(t *testing.T) {
	var output bytes.Buffer
	now := time.Unix(100, 0)
	r := importReporter{writer: &output, started: now, now: func() time.Time { return now }}
	for i, advance := range []time.Duration{0, 100 * time.Millisecond, 200 * time.Millisecond, time.Millisecond} {
		now = now.Add(advance)
		if err := r.report(importer.Progress{
			Phase: importer.ProgressImporting, CommittedFiles: int64((i + 1) * 1000),
			StreamingComplete: i == 3,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := output.String(); strings.Contains(got, "2000 files") ||
		strings.Count(got, "\n") != 3 || !strings.Contains(got, "elapsed 0.301s") {
		t.Fatalf("throttle: %q", got)
	}
}

type brokenImportWriter struct{ err error }

func (w brokenImportWriter) Write([]byte) (int, error) { return 0, w.err }

func TestImportWriterFailures(t *testing.T) {
	for _, progress := range []bool{false, true} {
		t.Run(fmt.Sprint(progress), func(t *testing.T) {
			path, file := importFixture(t, 1001)
			cmd := newCommand()
			var output bytes.Buffer
			failure := errors.New("writer failed")
			cmd.Writer, cmd.ErrWriter = &output, &output
			if progress {
				cmd.ErrWriter = brokenImportWriter{failure}
			} else {
				cmd.Writer = brokenImportWriter{failure}
			}
			err := cmd.Run(t.Context(), importArgs(path, file, "--disk-id", "1"))
			if !errors.Is(err, failure) {
				t.Fatalf("error: %v", err)
			}
			db, err := database.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			_, err = db.LatestCompleteSnapshot(t.Context(), 1)
			if progress && !errors.Is(err, database.ErrNotFound) || !progress && err != nil {
				t.Fatalf("publication: %v", err)
			}
		})
	}
}

func TestImportJSONSourceMTimeAndRepeat(t *testing.T) {
	path, file := importFixture(t, 1)
	captured := time.Unix(1700000000, 0)
	if err := os.Chtimes(file, captured, captured); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		cmd := newCommand()
		var stdout, stderr bytes.Buffer
		cmd.Writer, cmd.ErrWriter = &stdout, &stderr
		args := []string{"coldcat", "--db", path, "import", "--json",
			"--label", "disk", "--use-source-mtime"}
		if i == 1 {
			args = append(args, "--allow-repeat")
		}
		if err := cmd.Run(t.Context(), append(args, file)); err != nil {
			t.Fatal(err)
		}
		var result importResult
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Snapshot.ID != strconv.Itoa(i+1) ||
			result.Snapshot.CaptureProvenance != "source_mtime" ||
			result.Snapshot.CapturedAt != captured.UTC().Format(time.RFC3339Nano) {
			t.Fatalf("result: %+v", result)
		}
	}
}

func TestImportFailureHasNoSuccessOutput(t *testing.T) {
	for _, mode := range []string{"arguments", "parse", "duplicate", "busy"} {
		t.Run(mode, func(t *testing.T) {
			path, file := importFixture(t, 1)
			args := importArgs(path, file, "--disk-id", "1", "--json")
			switch mode {
			case "arguments":
				args = []string{"coldcat", "--db", path, "import", "--disk-id", "1", "--json", file}
			case "parse":
				if err := os.WriteFile(file, []byte("broken\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				cmd := newCommand()
				cmd.Writer, cmd.ErrWriter = &bytes.Buffer{}, &bytes.Buffer{}
				if err := cmd.Run(t.Context(), args); err != nil {
					t.Fatal(err)
				}
			case "busy":
				db, err := database.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
			}
			cmd := newCommand()
			var stdout, stderr bytes.Buffer
			cmd.Writer, cmd.ErrWriter = &stdout, &stderr
			if err := cmd.Run(t.Context(), args); err == nil || stdout.Len() != 0 {
				t.Fatalf("failure: stdout=%q err=%v", stdout.String(), err)
			}
		})
	}
}
