package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// WithAssets keeps file access confined to directory, including symlink resolution.
// The caller must close the root after all HTTP requests have stopped.
func WithAssets(backend http.Handler, directory string) (http.Handler, func() error, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, nil, fmt.Errorf("open assets: %w", err)
	}

	shell, err := root.Open("index.html")
	if err == nil {
		var info os.FileInfo
		info, err = shell.Stat()
		if err == nil && !info.Mode().IsRegular() {
			err = fmt.Errorf("index.html is not a regular file")
		}
		err = errors.Join(err, shell.Close())
	}
	if err != nil {
		_ = root.Close()
		return nil, nil, fmt.Errorf("open asset shell: %w", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !safeAssetPath(r) {
			http.Error(w, "invalid URL path", http.StatusBadRequest)
			return
		}

		p := r.URL.Path
		if p == "/api/v1" || strings.HasPrefix(p, "/api/v1/") ||
			p == "/healthz" || strings.HasPrefix(p, "/healthz/") {
			backend.ServeHTTP(w, r)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(p, "/")
		if applicationPath(p) {
			name = "index.html"
		}
		file, err := root.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, path.Base(name), info.ModTime(), file)
	})
	return handler, root.Close, nil
}

func safeAssetPath(r *http.Request) bool {
	p := r.URL.Path
	escaped := strings.ToLower(r.URL.EscapedPath())
	if !strings.HasPrefix(p, "/") || !utf8.ValidString(p) ||
		strings.ContainsAny(p, "\\\x00") || strings.Contains(escaped, "%2f") ||
		strings.Contains(escaped, "%5c") {
		return false
	}
	for _, c := range p {
		if c < 32 || c == 127 {
			return false
		}
	}
	parts := strings.Split(strings.TrimSuffix(p, "/"), "/")
	for i, part := range parts {
		if part == "." || part == ".." || (i > 0 && part == "") {
			return false
		}
	}
	return true
}

func applicationPath(p string) bool {
	// A generic document fallback would turn missing assets and malformed URLs into HTML success.
	if p == "/" || p == "/contents" || p == "/disks" {
		return true
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	if parts[0] != "contents" && parts[0] != "observations" &&
		parts[0] != "disks" && parts[0] != "snapshots" {
		return false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[1] {
		return false
	}
	return len(parts) == 2 || (parts[0] == "snapshots" && parts[2] == "directory")
}
