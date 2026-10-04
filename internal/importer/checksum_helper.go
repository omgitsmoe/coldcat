package importer

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrMissingField = errors.New("missing or empty field")

func Parse(r io.Reader, fn FileFunc) error {
	scanner := bufio.NewScanner(r)

	seenHeader := false
	var version int
	for scanner.Scan() {
		line := scanner.Text()
		if !seenHeader && strings.HasPrefix(line, "#") {
			var err error
			version, err = parseHeader(line)
			if err != nil {
				return err
			}
			seenHeader = true
			continue
		} else if strings.HasPrefix(line, "#") {
			// skip comments
			continue
		}

		file, err := parseLine(line, version)
		if err != nil {
			return err
		}
		err = fn(file)
		if err != nil {
			return err
		}
	}
	return nil
}

func parseHeader(line string) (version int, err error) {
	after, found := strings.CutPrefix(line, "# version ")
	if !found {
		return
	}

	version_str := strings.TrimSpace(after)
	version, err = strconv.Atoi(version_str)
	if err != nil {
		err = fmt.Errorf(
			"failed to parse header version number from '%s': %w",
			version_str, err)
	}
	return
}

func parseLine(line string, version int) (File, error) {
	numFields := 3
	if version == 1 {
		numFields = 4
	}

	allFields, path, found := strings.Cut(line, " ")
	if !found {
		return File{}, fmt.Errorf(
			"expected a space separating fields and path: %q", line)
	}

	fields := strings.SplitN(allFields, ",", numFields)
	if len(fields) != numFields {
		return File{}, fmt.Errorf(
			"%w: expected %d comma-separated fields, got %q",
			ErrMissingField, numFields, allFields)
	}

	var (
		mtime    time.Time
		size     uint64
		hashType HashType
		hash     []byte
		err      error
	)

	mtime, err = parseMTime(fields[0])
	if err != nil {
		return File{}, fmt.Errorf("parse line %q: %w", line, err)
	}

	idx := 1

	if version == 1 {
		size, err = parseSize(fields[idx])
		if err != nil {
			return File{}, fmt.Errorf("parse line %q: %w", line, err)
		}
		idx++
	}

	// --- hash type ---
	hashType, err = parseHashType(fields[idx])
	if err != nil {
		return File{}, fmt.Errorf("parse line %q: %w", line, err)
	}
	idx++

	// --- hash ---
	hash, err = parseHash(fields[idx])
	if err != nil {
		return File{}, fmt.Errorf("parse line %q: %w", line, err)
	}

	dirPath, name := filepath.Split(path)

	file := File{
		Name:               name,
		PathRelativeToRoot: dirPath,
		MTime:              mtime,
		SizeInBytes:        size,
		HashType:           hashType,
		Hash:               hash,
	}

	return file, nil
}

func parseMTime(field string) (time.Time, error) {
	if field != "" {
		f, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf(
				"invalid mtime %q: %w", field, err)
		}
		return mTimeF64ToTime(f), nil
	}

	return time.Time{}, nil
}

func mTimeF64ToTime(mtime float64) time.Time {
	const NS_PER_SEC = 1_000_000_000.0

	seconds := int64(math.Trunc(mtime))
	nanoseconds := int64(math.Mod(mtime, 1.0) * NS_PER_SEC)

	t := time.Unix(seconds, nanoseconds)

	return t
}

func parseSize(field string) (uint64, error) {
	if field != "" {
		size, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, fmt.Errorf(
				"invalid size %q: %w", field, err)
		}

		return size, nil
	}

	return 0, nil
}

func parseHashType(field string) (HashType, error) {
	if field != "" {
		hashType, err := FromIdentifier(field)
		if err != nil {
			return HashType{}, fmt.Errorf(
				"invalid hash type %q: %w", field, err)
		}

		return hashType, nil
	}

	return HashType{}, fmt.Errorf("empty hash type: %w", ErrMissingField)
}

func parseHash(field string) ([]byte, error) {
	if field != "" {
		hash, err := hex.DecodeString(field)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid hex hash %q: %w", field, err)
		}

		return hash, nil
	}

	return nil, fmt.Errorf("empty hash: %w", ErrMissingField)
}
