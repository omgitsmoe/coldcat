//go:build webui

package main

import (
	"bufio"
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/frontend"
)

func TestEmbeddedAssets(t *testing.T) {
	handler, err := defaultAssetHandler(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/", "/snapshots/1/directory"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "_app/") {
			t.Fatalf("embedded shell %s: %d %s", path, w.Code, w.Body.String())
		}
	}

	assets, err := frontend.Assets()
	if err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(assets, "_app/immutable/entry/*.js")
	if err != nil || len(files) == 0 {
		t.Fatalf("missing embedded SvelteKit assets: %v %v", files, err)
	}
	for _, file := range files {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/"+file, nil))
		if w.Code != http.StatusOK || !strings.Contains(
			w.Header().Get("Content-Type"), "javascript",
		) {
			t.Fatalf("embedded script %s: %d %v", file, w.Code, w.Header())
		}
	}
}

func TestEmbeddedServeModes(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"embedded", "override", "api-only"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestServeProcessHelper$")
			cmd.Dir = directory
			cmd.Env = append(os.Environ(),
				"COLDCAT_SERVE_HELPER=1",
				"COLDCAT_TEST_DB="+filepath.Join(directory, "catalog.sqlite"),
				"COLDCAT_TEST_LISTEN=127.0.0.1:0",
			)
			if mode == "override" {
				if err := os.WriteFile(
					filepath.Join(directory, "index.html"), []byte("override shell"), 0600,
				); err != nil {
					t.Fatal(err)
				}
				cmd.Env = append(cmd.Env, "COLDCAT_TEST_ASSETS="+directory)
			} else if mode == "api-only" {
				cmd.Env = append(cmd.Env, "COLDCAT_TEST_API_ONLY=1")
			}
			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cancel()
				_ = cmd.Wait()
			})

			scanner := bufio.NewScanner(stderr)
			if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "serving http://") {
				t.Fatalf("startup: %s %v", scanner.Text(), scanner.Err())
			}
			origin := strings.TrimPrefix(scanner.Text(), "serving ")
			client := &http.Client{Timeout: 3 * time.Second}
			for _, path := range []string{"/", "/disks/1", "/api/v1/catalog", "/healthz"} {
				response, err := client.Get(origin + path)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				status := http.StatusOK
				isUI := path == "/" || path == "/disks/1"
				if isUI && mode == "api-only" {
					status = http.StatusNotFound
				}
				if response.StatusCode != status {
					t.Fatalf("%s: %d %s", path, response.StatusCode, body)
				}
				if isUI && mode == "embedded" && !strings.Contains(string(body), "_app/") {
					t.Fatalf("missing embedded shell: %s", body)
				}
				if isUI && mode == "override" && string(body) != "override shell" {
					t.Fatalf("ignored asset override: %s", body)
				}
			}
		})
	}
}
