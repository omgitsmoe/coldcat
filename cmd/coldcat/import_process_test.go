package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type gatedImportWriter struct{ ctx context.Context }

func (w gatedImportWriter) Write(p []byte) (int, error) {
	n, err := os.Stderr.Write(p)
	if err == nil && bytes.HasPrefix(p, []byte("importing: 1000 files committed")) {
		<-w.ctx.Done()
	}
	return n, err
}

func TestInterruptedImportHelper(t *testing.T) {
	path := os.Getenv("COLDCAT_INTERRUPTED_DB")
	if path == "" {
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cmd := newCommand()
	cmd.Writer = os.Stdout
	cmd.ErrWriter = gatedImportWriter{ctx}
	args := importArgs(path, os.Getenv("COLDCAT_INTERRUPTED_FILE"), "--disk-id", "1", "--json")
	if os.Getenv("COLDCAT_IMPORT_INVALID_ARGS") == "1" {
		args = []string{"coldcat", "--db", path, "import", "--disk-id", "1", "--json",
			os.Getenv("COLDCAT_INTERRUPTED_FILE")}
	}
	os.Exit(runCommand(ctx, cmd, args))
}

func TestInterruptedImportRecoveryAndCursor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process signals")
	}
	for _, mode := range []string{"interrupt", "terminate", "kill"} {
		t.Run(mode, func(t *testing.T) {
			path, file := importFixture(t, 2)
			cmd := newCommand()
			cmd.Writer, cmd.ErrWriter = &bytes.Buffer{}, &bytes.Buffer{}
			if err := cmd.Run(t.Context(), importArgs(path, file, "--disk-id", "1")); err != nil {
				t.Fatal(err)
			}
			db, err := database.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			a := app.New(db)
			content, err := a.LookupContent(t.Context(), app.LookupContentRequest{
				HashType: "sha256", Hash: "ab",
			})
			if err != nil {
				t.Fatal(err)
			}
			req := app.ListContentObservationsRequest{ContentID: content.Content.Id, Limit: 1}
			first, err := a.ListContentObservations(t.Context(), req)
			if err != nil || first.NextCursor == "" {
				t.Fatalf("first: %+v %v", first, err)
			}
			req.Cursor = first.NextCursor
			want, err := a.ListContentObservations(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			db.Close()

			var input strings.Builder
			input.WriteString("# version 1\n,4,sha256,ab shared\n")
			for i := range 1000 {
				fmt.Fprintf(&input, ",4,sha256,%08x new/file-%d\n", i, i)
			}
			if err := os.WriteFile(file, []byte(input.String()), 0600); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, executable, "-test.run=^TestInterruptedImportHelper$")
			child.Env = append(os.Environ(), "COLDCAT_INTERRUPTED_DB="+path,
				"COLDCAT_INTERRUPTED_FILE="+file)
			var stdout bytes.Buffer
			child.Stdout = &stdout
			stderr, err := child.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { child.Process.Kill() })
			scanner := bufio.NewScanner(stderr)
			if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "importing: 1000 files committed") {
				t.Fatalf("commit handshake: %q %v", scanner.Text(), scanner.Err())
			}
			switch mode {
			case "interrupt":
				err = child.Process.Signal(os.Interrupt)
			case "terminate":
				err = child.Process.Signal(syscall.SIGTERM)
			case "kill":
				err = child.Process.Kill()
			}
			if err != nil {
				t.Fatal(err)
			}
			var diagnostics strings.Builder
			for scanner.Scan() {
				diagnostics.WriteString(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			err = child.Wait()
			if err == nil || stdout.Len() != 0 || ctx.Err() != nil {
				t.Fatalf("exit: %v stdout=%q timeout=%v", err, stdout.String(), ctx.Err())
			}
			if mode != "kill" && (child.ProcessState.ExitCode() != 1 ||
				!strings.Contains(diagnostics.String(), "context canceled")) {
				t.Fatalf("signal contract: %v %q", child.ProcessState, diagnostics.String())
			}

			raw, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			assertRows := func(table string, want int) {
				t.Helper()
				var got int
				if err := raw.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil || got != want {
					t.Fatalf("%s: got %d want %d: %v", table, got, want, err)
				}
			}
			if mode == "kill" {
				assertRows("snapshot", 2)
				assertRows("observation", 1002)
				assertRows("pending_size", 1000)
				assertRows("import_content", 999)
			} else {
				assertRows("snapshot", 1)
				assertRows("observation", 2)
				assertRows("content", 1)
				assertRows("pending_size", 0)
				assertRows("import_content", 0)
			}
			db, err = database.OpenContext(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			assertRows("snapshot", 1)
			assertRows("observation", 2)
			assertRows("content", 1)
			assertRows("pending_size", 0)
			assertRows("import_content", 0)
			a = app.New(db)
			got, err := a.ListContentObservations(t.Context(), req)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("recovered page: %+v %v", got, err)
			}
			content, err = a.LookupContent(t.Context(), app.LookupContentRequest{
				HashType: "sha256", Hash: "ab",
			})
			if err != nil || content.Content.Size != nil {
				t.Fatalf("shared metadata: %+v %v", content, err)
			}
			if _, err := a.Import(t.Context(), app.ImportRequest{
				DiskID: 1, Path: file, CapturedAt: time.Unix(1800000000, 0),
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := a.ListContentObservations(t.Context(), req); !errors.Is(err, database.ErrStaleCursor) {
				t.Fatalf("cursor after publication: %v", err)
			}
		})
	}
}

func TestImportProcessOutputAndExit(t *testing.T) {
	for _, mode := range []string{"success", "arguments", "parse", "duplicate", "busy"} {
		t.Run(mode, func(t *testing.T) {
			path, file := importFixture(t, 1)
			switch mode {
			case "parse":
				if err := os.WriteFile(file, []byte("broken\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				cmd := newCommand()
				cmd.Writer, cmd.ErrWriter = &bytes.Buffer{}, &bytes.Buffer{}
				if err := cmd.Run(t.Context(), importArgs(path, file, "--disk-id", "1")); err != nil {
					t.Fatal(err)
				}
			case "busy":
				db, err := database.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, executable, "-test.run=^TestInterruptedImportHelper$")
			child.Env = append(os.Environ(), "COLDCAT_INTERRUPTED_DB="+path,
				"COLDCAT_INTERRUPTED_FILE="+file)
			if mode == "arguments" {
				child.Env = append(child.Env, "COLDCAT_IMPORT_INVALID_ARGS=1")
			}
			var stdout, stderr bytes.Buffer
			child.Stdout, child.Stderr = &stdout, &stderr
			err = child.Run()
			if mode == "success" {
				if err != nil || !bytes.Contains(stdout.Bytes(), []byte(`"state":"complete"`)) ||
					!bytes.Contains(stderr.Bytes(), []byte("publishing: 1 files")) {
					t.Fatalf("success: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
				}
			} else if err == nil || child.ProcessState == nil || child.ProcessState.ExitCode() != 1 ||
				stdout.Len() != 0 || stderr.Len() == 0 || ctx.Err() != nil {
				t.Fatalf("failure: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
			}
		})
	}
}
