package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type checkpoint struct {
	Stage           string `json:"stage"`
	Path            string `json:"path"`
	Device          uint64 `json:"device"`
	Inode           uint64 `json:"inode"`
	Links           uint64 `json:"links"`
	LogicalBytes    int64  `json:"logical_bytes"`
	StatBlocksBytes int64  `json:"stat_blocks_bytes"`
}

type fixture struct {
	dir    string
	marker io.Writer
	out    io.Writer
}

func (f fixture) emit(stage string, file *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}

	data, err := json.Marshal(checkpoint{
		Stage: stage, Path: file.Name(), Device: stat.Dev, Inode: stat.Ino,
		Links: stat.Nlink, LogicalBytes: stat.Size, StatBlocksBytes: stat.Blocks * 512,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(f.marker, "coldcat-physical %s\n", data); err != nil {
		return err
	}

	_, err = fmt.Fprintf(f.out, "%s\n", data)
	return err
}

func (f fixture) runFile(name string, body func(*os.File) error) (err error) {
	file, err := os.OpenFile(filepath.Join(f.dir, name), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()

	if err := f.emit("created", file); err != nil {
		return err
	}

	return body(file)
}

func writeAndSync(file *os.File, offset int64) error {
	if _, err := file.WriteAt(make([]byte, 1024*1024), offset); err != nil {
		return err
	}

	return file.Sync()
}

func (f fixture) run() error {
	if err := f.runFile("sparse-journal", func(file *os.File) error {
		if err := file.Truncate(16 * 1024 * 1024); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		if err := f.emit("sparse-extended", file); err != nil {
			return err
		}
		if _, err := file.WriteAt(make([]byte, 1024*1024), 8*1024*1024); err != nil {
			return err
		}
		if err := f.emit("sparse-written-buffered", file); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		if err := f.emit("sparse-written-synced", file); err != nil {
			return err
		}
		if err := file.Truncate(0); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		return f.emit("truncated-synced", file)
	}); err != nil {
		return err
	}

	if err := f.runFile("preallocated-journal", func(file *os.File) error {
		if err := unix.Fallocate(int(file.Fd()), 0, 0, 4*1024*1024); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		return f.emit("preallocated-synced", file)
	}); err != nil {
		return err
	}

	if err := f.runFile("unlinked-journal", func(file *os.File) error {
		if err := writeAndSync(file, 0); err != nil {
			return err
		}
		if err := f.emit("before-unlink", file); err != nil {
			return err
		}
		if err := os.Remove(file.Name()); err != nil {
			return err
		}
		if err := writeAndSync(file, 1024*1024); err != nil {
			return err
		}
		return f.emit("open-unlinked-written-synced", file)
	}); err != nil {
		return err
	}

	if err := f.runFile("simultaneous-a-journal", func(first *os.File) error {
		if err := writeAndSync(first, 0); err != nil {
			return err
		}
		return f.runFile("simultaneous-b-journal", func(second *os.File) error {
			if err := writeAndSync(second, 0); err != nil {
				return err
			}
			if err := f.emit("simultaneous-a", first); err != nil {
				return err
			}
			return f.emit("simultaneous-b", second)
		})
	}); err != nil {
		return err
	}

	for range 16 {
		if err := f.runFile("rapid-journal", func(file *os.File) error {
			if err := writeAndSync(file, 0); err != nil {
				return err
			}
			if err := f.emit("rapid-written-synced", file); err != nil {
				return err
			}
			return os.Remove(file.Name())
		}); err != nil {
			return err
		}
	}
	return nil
}

func run(parent, markerPath string, out io.Writer) (err error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(parent, &stat); err != nil {
		return err
	}
	if stat.Type != 0xef53 || stat.Bsize != 4096 {
		return fmt.Errorf("expected ext4 with 4096-byte blocks, got type %#x, block size %d",
			stat.Type, stat.Bsize)
	}

	marker, err := os.OpenFile(markerPath, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, marker.Close()) }()

	dir, err := os.MkdirTemp(parent, "coldcat-physical-validation-")
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(dir))
	}()

	if err := (fixture{dir: dir, marker: marker, out: out}).run(); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}

	parentDir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, parentDir.Close()) }()
	if err := unix.Syncfs(int(parentDir.Fd())); err != nil {
		return err
	}

	_, err = fmt.Fprintln(marker, "coldcat-physical cleanup-syncfs-complete")
	return err
}

func main() {
	parent := flag.String("dir", "", "existing ext4 parent directory for disposable files")
	marker := flag.String("trace-marker", "", "trace instance marker; /dev/null for smoke check")
	flag.Parse()
	if *parent == "" || *marker == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "required: -dir <ext4-directory> -trace-marker <path>")
		os.Exit(2)
	}
	if err := run(*parent, *marker, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
