package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/database"
	"github.com/urfave/cli/v3"
)

func TestServeProcessHelper(t *testing.T) {
	if os.Getenv("COLDCAT_SERVE_HELPER") != "1" {
		return
	}

	os.Args = []string{
		"coldcat",
		"--db",
		os.Getenv("COLDCAT_TEST_DB"),
		"serve",
		"--listen",
		os.Getenv("COLDCAT_TEST_LISTEN"),
	}
	if file := os.Getenv("COLDCAT_TEST_IMPORT"); file != "" {
		os.Args = []string{
			"coldcat",
			"--db",
			os.Getenv("COLDCAT_TEST_DB"),
			"import",
			"--disk-id",
			"1",
			"--captured-at",
			"2023-01-01T00:00:00Z",
			file,
		}
	}

	main()
	os.Exit(0)
}

func TestServeSignalsAndCatalogOwnership(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process signals")
	}

	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.sqlite")
			db, err := database.Open(path)
			if err != nil {
				t.Fatal(err)
			}

			db.Close()
			raw, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			if _, err := raw.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance) VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit');
INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',X'AB');
INSERT INTO observation(id,snapshot_id,content_id,path) VALUES(1,1,1,'unfinished');
INSERT INTO import_content(snapshot_id,content_id) VALUES(1,1);`); err != nil {
				t.Fatal(err)
			}

			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestServeProcessHelper$")
			cmd.Env = append(
				os.Environ(),
				"COLDCAT_SERVE_HELPER=1",
				"COLDCAT_TEST_DB="+path,
				"COLDCAT_TEST_LISTEN=127.0.0.1:0",
			)
			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}

			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { cmd.Process.Kill() })
			scanner := bufio.NewScanner(stderr)
			if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "serving http://127.0.0.1:") {
				t.Fatalf("startup: %s %v", scanner.Text(), scanner.Err())
			}

			address := strings.TrimPrefix(scanner.Text(), "serving ")
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Get(address + "/healthz")
			if err != nil {
				t.Fatal(err)
			}

			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("readiness: %d", resp.StatusCode)
			}

			var count int
			if err := raw.QueryRow("SELECT COUNT(*) FROM observation").Scan(&count); err != nil ||
				count != 0 {
				t.Fatalf("server startup did not recover: %d %v", count, err)
			}

			file := filepath.Join(t.TempDir(), "inventory.cshd")
			if err := os.WriteFile(file, []byte(",sha256,"+fixtureSHA256AB+" file\n"), 0600); err != nil {
				t.Fatal(err)
			}

			for _, extra := range [][]string{nil, {"COLDCAT_TEST_IMPORT=" + file}} {
				child := exec.CommandContext(ctx, executable, "-test.run=^TestServeProcessHelper$")
				child.Env = append(append([]string{}, cmd.Env...), extra...)
				output, err := child.CombinedOutput()
				if err == nil || child.ProcessState.ExitCode() != 1 ||
					!bytes.Contains(output, []byte("catalog is in use")) {
					t.Fatalf("competing command: %s %v", output, err)
				}
			}

			if db, err := database.Open(path); !errors.Is(err, database.ErrBusy) {
				if db != nil {
					db.Close()
				}

				t.Fatalf("server lock: %v", err)
			}

			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}

			for scanner.Scan() {
			}

			if err := cmd.Wait(); err != nil {
				t.Fatalf("shutdown exit: %v", err)
			}

			db, err = database.Open(path)
			if err != nil {
				t.Fatalf("lock after shutdown: %v", err)
			}

			db.Close()
		})
	}
}

func TestServeStartupFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := serveCatalog(t.Context(), path, "127.0.0.1:0", &output); !errors.Is(
		err,
		database.ErrBusy,
	) {
		t.Fatalf("competing catalog: %v", err)
	}

	db.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := serveCatalog(t.Context(), path, listener.Addr().String(), &output); err == nil {
		t.Fatal("accepted occupied address")
	}

	if err := serveCatalog(t.Context(), path, "invalid", &output); err == nil {
		t.Fatal("accepted malformed address")
	}

	if output.Len() != 0 {
		t.Fatalf("reported readiness on startup failure: %s", output.String())
	}

	db, err = database.Open(path)
	if err != nil {
		t.Fatalf("failed startup leaked lock: %v", err)
	}

	db.Close()
	cmd := newCommand()
	if listen := cmd.Commands[0].Flags[0].(*cli.StringFlag).Value; listen != "127.0.0.1:8080" {
		t.Fatalf("default listen: %s", listen)
	}
}

func TestServeRecoveryFailurePreventsStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	db.Close()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance) VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit');
CREATE TRIGGER fail_recovery BEFORE DELETE ON snapshot BEGIN SELECT RAISE(ABORT,'injected recovery failure'); END;`); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := serveCatalog(t.Context(), path, "127.0.0.1:0", &output); err == nil ||
		!strings.Contains(err.Error(), "injected recovery failure") {
		t.Fatalf("startup: %v", err)
	}

	if output.Len() != 0 {
		t.Fatalf("served before recovery: %s", output.String())
	}

	if _, err := raw.Exec("DROP TRIGGER fail_recovery"); err != nil {
		t.Fatal(err)
	}

	db, err = database.Open(path)
	if err != nil {
		t.Fatalf("failed startup leaked catalog ownership: %v", err)
	}

	db.Close()
}
