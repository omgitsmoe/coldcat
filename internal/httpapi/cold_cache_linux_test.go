//go:build linux

package httpapi

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type catalogResidency struct {
	pages    int
	resident int
}

func catalogFileResidency(file *os.File) (result catalogResidency, err error) {
	info, err := file.Stat()
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || int64(int(info.Size())) != info.Size() {
		return result, fmt.Errorf("residency requires a nonempty, addressable regular file")
	}
	mapping, err := unix.Mmap(int(file.Fd()), 0, int(info.Size()), unix.PROT_NONE, unix.MAP_SHARED)
	if err != nil {
		return result, fmt.Errorf("map catalog for residency: %w", err)
	}
	defer func() { err = errors.Join(err, unix.Munmap(mapping)) }()
	pageSize := os.Getpagesize()
	result.pages = 1 + (len(mapping)-1)/pageSize
	vector := make([]byte, result.pages)
	// x/sys has no Linux mincore wrapper. PROT_NONE prevents this inspection
	// from faulting catalog data into memory; keep both syscall buffers alive.
	_, _, errno := unix.Syscall(unix.SYS_MINCORE,
		uintptr(unsafe.Pointer(&mapping[0])), uintptr(len(mapping)),
		uintptr(unsafe.Pointer(&vector[0])))
	runtime.KeepAlive(mapping)
	runtime.KeepAlive(vector)
	if errno != 0 {
		return result, fmt.Errorf("inspect catalog residency: %w", errno)
	}
	for _, state := range vector {
		if state&1 != 0 {
			result.resident++
		}
	}
	return result, nil
}

func requireCatalogEvicted(result catalogResidency) error {
	if result.pages == 0 || result.resident != 0 {
		return fmt.Errorf("catalog cache eviction unverified: %d/%d pages resident",
			result.resident, result.pages)
	}
	return nil
}

// SQLite must be closed first: dirty pages or live mappings can defeat DONTNEED.
// This evicts only catalog file data, not filesystem metadata or device caches.
func evictCatalogFile(path string) (result catalogResidency, err error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := file.Sync(); err != nil {
		return result, fmt.Errorf("sync catalog: %w", err)
	}
	if err := unix.Fadvise(int(file.Fd()), 0, 0, unix.FADV_DONTNEED); err != nil {
		return result, fmt.Errorf("evict catalog: %w", err)
	}
	result, err = catalogFileResidency(file)
	if err != nil {
		return result, err
	}
	return result, requireCatalogEvicted(result)
}
