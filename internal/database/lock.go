package database

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

func databaseDSN(path string) string {
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	return (&url.URL{Scheme: "file", Path: uriPath}).String() + dsnParams
}

func acquireCatalogLock(path string) (string, *flock.Flock, error) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return "", nil, fmt.Errorf("%w: catalog must be a filesystem path", ErrValidation)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if errors.Is(err, os.ErrNotExist) {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(abs))
		if parentErr != nil {
			return "", nil, parentErr
		}
		canonical = filepath.Join(parent, filepath.Base(abs))
	} else if err != nil {
		return "", nil, err
	}
	lock := flock.New(canonical+".lock", flock.SetPermissions(0o600))
	locked, err := lock.TryLock()
	if err != nil {
		lock.Close()
		return "", nil, fmt.Errorf("lock catalog: %w", err)
	}
	if !locked {
		lock.Close()
		return "", nil, fmt.Errorf("%w: %s", ErrBusy, canonical)
	}
	return canonical, lock, nil
}
