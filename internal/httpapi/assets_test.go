package httpapi

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func assetBodies() map[string]string {
	return map[string]string{
		"index.html":     "<!doctype html><title>shell</title>",
		"_app/main.js":   "console.log('asset')",
		"_app/main.css":  "body { color: black }",
		"snow 雪.txt":     "literal asset",
		"api/v1/missing": "must not serve",
		"healthz":        "must not serve",
	}
}

func assetFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range assetBodies() {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAssetsDispatch(t *testing.T) {
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":"backend"}`))
	})
	t.Run("directory", func(t *testing.T) {
		handler, closeAssets, err := WithAssets(backend, assetFixture(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = closeAssets() })
		testAssetsDispatch(t, handler)
	})
	t.Run("filesystem", func(t *testing.T) {
		assets := fstest.MapFS{}
		for name, body := range assetBodies() {
			assets[name] = &fstest.MapFile{Data: []byte(body)}
		}
		handler, err := WithAssetFS(backend, assets)
		if err != nil {
			t.Fatal(err)
		}
		testAssetsDispatch(t, handler)
	})
}

func testAssetsDispatch(t *testing.T, handler http.Handler) {
	t.Helper()
	for _, tc := range []struct {
		path, method, mime string
		status             int
	}{
		{"/", "GET", "text/html", 200},
		{"/?q=a%2Fb", "GET", "text/html", 200},
		{"/contents", "GET", "text/html", 200},
		{"/contents/9223372036854775807", "GET", "text/html", 200},
		{"/observations/1", "GET", "text/html", 200},
		{"/disks", "GET", "text/html", 200},
		{"/disks/1", "GET", "text/html", 200},
		{"/snapshots/1", "GET", "text/html", 200},
		{"/snapshots/1/directory?path=a%2F%E9%9B%AA%5Cb%3F", "GET", "text/html", 200},
		{"/disks/%31", "GET", "text/html", 200},
		{"/disks/1", "HEAD", "text/html", 200},
		{"/_app/main.js?v=1", "GET", "javascript", 200},
		{"/_app/main.css", "HEAD", "text/css", 200},
		{"/snow%20%E9%9B%AA.txt", "GET", "text/plain", 200},
		{"/_app/missing.js", "GET", "text/plain", 404},
		{"/_app/", "GET", "text/plain", 404},
		{"/missing.css", "GET", "text/plain", 404},
		{"/unknown", "GET", "text/plain", 404},
		{"/disks/0", "GET", "text/plain", 404},
		{"/disks/01", "GET", "text/plain", 404},
		{"/disks/9223372036854775808", "GET", "text/plain", 404},
		{"/disks/abc", "GET", "text/plain", 404},
		{"/disks/1/extra", "GET", "text/plain", 404},
		{"/contents/lookup", "GET", "text/plain", 404},
		{"/disks/1/", "GET", "text/plain", 404},
		{"//disks/1", "GET", "text/plain", 400},
		{"/../index.html", "GET", "text/plain", 400},
		{"/%2e%2e/index.html", "GET", "text/plain", 400},
		{"/_app/../index.html", "GET", "text/plain", 400},
		{"/disks%2f1", "GET", "text/plain", 400},
		{"/disks%252f1", "GET", "text/plain", 404},
		{"/%5cindex.html", "GET", "text/plain", 400},
		{"/%00", "GET", "text/plain", 400},
		{"/%ff", "GET", "text/plain", 400},
		{"/", "POST", "text/plain", 405},
		{"/api/v1/missing", "GET", "application/json", 503},
		{"/api/v1", "POST", "application/json", 503},
		{"/healthz", "GET", "application/json", 503},
		{"/healthz/missing", "GET", "application/json", 503},
		{"/%61pi/v1/missing", "GET", "application/json", 503},
		{"/api/v1/../contents", "GET", "text/plain", 400},
		{"/api%2fv1/catalog", "GET", "text/plain", 400},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Header().Get("Content-Type"), tc.mime) {
				t.Fatalf("response: %d %v %s", w.Code, w.Header(), w.Body.String())
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		})
	}
}

func TestAssetFSStartupAndRange(t *testing.T) {
	for _, assets := range []fstest.MapFS{
		{},
		{"index.html": {Mode: os.ModeDir}},
	} {
		if _, err := WithAssetFS(http.NotFoundHandler(), assets); err == nil {
			t.Fatal("accepted invalid asset shell")
		}
	}

	handler, err := WithAssetFS(http.NotFoundHandler(), fstest.MapFS{
		"index.html":   {Data: []byte("<!doctype html>")},
		"_app/main.js": {Data: []byte("console.log('asset')")},
	})
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/_app/main.js", nil)
	r.Header.Set("Range", "bytes=0-6")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != "console" {
		t.Fatalf("range response: %d %s", w.Code, w.Body.String())
	}
}

func TestAssetsConfinementAndStartup(t *testing.T) {
	dir := assetFixture(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	handler, closeAssets, err := WithAssets(http.NotFoundHandler(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeAssets() })
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/escape.txt", nil))
	if w.Code != 404 || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("escaped asset root: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{filepath.Join(dir, "missing"), t.TempDir(), outside} {
		if _, close, err := WithAssets(http.NotFoundHandler(), path); err == nil {
			_ = close()
			t.Fatalf("accepted invalid assets: %s", path)
		}
	}
	if err := os.Remove(filepath.Join(dir, "index.html")); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/disks/1", nil))
	if w.Code != 404 {
		t.Fatalf("missing shell: %d", w.Code)
	}
}

func TestAssetsRealAPIErrors(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	handler, closeAssets, err := WithAssets(New(app.New(db)), assetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeAssets() })
	for _, tc := range []struct {
		path, method string
		status       int
	}{
		{"/api/v1/search?q=x", "GET", 400},
		{"/api/v1/disks/999", "GET", 404},
		{"/api/v1/catalog", "POST", 405},
		{"/api/v1/missing", "GET", 404},
		{"/healthz?bad=1", "GET", 400},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || strings.Contains(w.Body.String(), "<!doctype") {
			t.Fatalf("API response %s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for path, status := range map[string]int{"/healthz": 503, "/api/v1/catalog": 500} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != status || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("unavailable backend %s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestAssetsHTTPWire(t *testing.T) {
	handler, closeAssets, err := WithAssets(http.NotFoundHandler(), assetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeAssets() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/disks/1?q=%2F", 200},
		{"/_app/main.js", 200},
		{"/_app/missing.js", 404},
		{"/api/v1/missing", 404},
		{"/%2e%2e/index.html", 400},
	} {
		request, err := http.NewRequest("HEAD", server.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || len(body) != 0 || response.StatusCode != tc.status {
			t.Fatalf("HEAD %s: %d %q %v", tc.path, response.StatusCode, body, err)
		}
	}
	for _, target := range []string{"/bad%zz", "/api/v1/%", "/healthz%0"} {
		conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		_, err = fmt.Fprintf(conn,
			"GET %s HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", target)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		_ = conn.Close()
		if err != nil || response.StatusCode != 400 || strings.Contains(string(body), "<!doctype") {
			t.Fatalf("malformed encoding %s: %d %q %v", target, response.StatusCode, body, err)
		}
	}
}
