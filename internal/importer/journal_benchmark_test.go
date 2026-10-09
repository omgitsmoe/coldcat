package importer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const importJournalSampleInterval = 10 * time.Millisecond

type importJournalSample struct {
	journal int64
	wal     int64
	shm     int64
	samples int
	err     error
}

func (s *importJournalSample) sample(
	path string, stat func(string) (os.FileInfo, error),
) error {
	s.samples++
	for _, sidecar := range []struct {
		suffix string
		peak   *int64
	}{
		{"-journal", &s.journal},
		{"-wal", &s.wal},
		{"-shm", &s.shm},
	} {
		info, err := stat(path + sidecar.suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat import sidecar %q: %w", path+sidecar.suffix, err)
		}
		*sidecar.peak = max(*sidecar.peak, info.Size())
	}
	return nil
}

func startImportJournalSampler(path string) func() importJournalSample {
	stop := make(chan struct{})
	done := make(chan importJournalSample, 1)
	initial := importJournalSample{}
	initial.err = initial.sample(path, os.Stat)
	go func() {
		result := initial
		if result.err != nil {
			done <- result
			return
		}
		ticker := time.NewTicker(importJournalSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				result.err = result.sample(path, os.Stat)
				if result.err != nil {
					done <- result
					return
				}
			case <-stop:
				result.err = result.sample(path, os.Stat)
				done <- result
				return
			}
		}
	}()
	var once sync.Once
	var result importJournalSample
	return func() importJournalSample {
		once.Do(func() {
			close(stop)
			result = <-done
		})
		return result
	}
}

func TestImportJournalSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	var result importJournalSample
	assertNoErr(t, result.sample(path, os.Stat))
	if result.samples != 1 || result.journal != 0 || result.wal != 0 || result.shm != 0 {
		t.Fatalf("missing sidecars: %+v", result)
	}
	for _, sidecar := range []struct {
		suffix string
		size   int
	}{
		{"-journal", 100}, {"-wal", 200}, {"-shm", 300},
	} {
		assertNoErr(t, os.WriteFile(path+sidecar.suffix, make([]byte, sidecar.size), 0600))
	}
	assertNoErr(t, result.sample(path, os.Stat))
	if result.journal != 100 || result.wal != 200 || result.shm != 300 {
		t.Fatalf("sidecar sizes: %+v", result)
	}
	assertNoErr(t, os.Truncate(path+"-journal", 400))
	assertNoErr(t, result.sample(path, os.Stat))
	assertNoErr(t, os.Truncate(path+"-journal", 10))
	assertNoErr(t, os.Remove(path+"-wal"))
	assertNoErr(t, os.Remove(path+"-shm"))
	assertNoErr(t, result.sample(path, os.Stat))
	if result.journal != 400 || result.wal != 200 || result.shm != 300 || result.samples != 4 {
		t.Fatalf("maxima after shrink/deletion: %+v", result)
	}
}

func TestImportJournalStatError(t *testing.T) {
	want := errors.New("injected stat failure")
	var result importJournalSample
	err := result.sample("catalog.sqlite", func(string) (os.FileInfo, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("stat error: %v, want %v", err, want)
	}
}

func TestImportJournalSampler(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	stop := startImportJournalSampler(path)
	t.Cleanup(func() { stop() })
	assertNoErr(t, os.WriteFile(path+"-journal", make([]byte, 123), 0600))
	result := stop()
	if result.err != nil || result.samples < 2 || result.journal != 123 {
		t.Fatalf("initial/final sampling: %+v", result)
	}
	if again := stop(); again != result {
		t.Fatalf("repeated stop: %+v, want %+v", again, result)
	}
}

func TestImportJournalSamplerError(t *testing.T) {
	stop := startImportJournalSampler("\x00")
	t.Cleanup(func() { stop() })
	if result := stop(); result.err == nil {
		t.Fatalf("expected invalid-path stat error: %+v", result)
	}
}
