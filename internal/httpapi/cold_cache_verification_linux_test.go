//go:build linux

package httpapi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogResidencyRejectsUnverifiedEviction(t *testing.T) {
	for _, result := range []catalogResidency{{}, {pages: 4, resident: 1}} {
		if err := requireCatalogEvicted(result); err == nil {
			t.Fatalf("accepted unverified eviction: %+v", result)
		}
	}
	if err := requireCatalogEvicted(catalogResidency{pages: 4}); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogFileResidencyErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := evictCatalogFile(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	for _, path := range []string{dir, filepath.Join(dir, "empty")} {
		if path != dir {
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := catalogFileResidency(file); err == nil {
			t.Fatalf("accepted invalid file %s", path)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := catalogFileResidency(file); err == nil {
			t.Fatal("accepted closed file")
		}
	}
}

func TestCatalogFileEviction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pages")
	data := make([]byte, 16*os.Getpagesize()+17)
	for i := range data {
		data[i] = byte(i)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before, inspectErr := catalogFileResidency(file)
	closeErr := file.Close()
	if err := errors.Join(inspectErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if before.pages != 17 || before.resident != before.pages {
		t.Fatalf("written file not resident: %+v", before)
	}
	after, err := evictCatalogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.pages != before.pages || after.resident != 0 {
		t.Fatalf("evicted file: %+v", after)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatal("eviction changed file data")
	}
}
