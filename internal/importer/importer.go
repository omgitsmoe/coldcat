package importer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type File struct {
	Name               string
	PathRelativeToRoot string
	MTime              time.Time
	SizeInBytes        uint64
	HashType           base.HashType
	Hash               []byte
}

// path rejoins the path that parsing split into PathRelativeToRoot and Name.
func (f File) path() string {
	return f.PathRelativeToRoot + f.Name
}

type FileFunc = func(file File) error
type ParseFunc = func(r io.Reader, fn FileFunc) error

var extensionToParseFunc = map[string]ParseFunc{
	".cshd": ParseCshd,
}

func Import(db *database.DB, disk base.DiskId, path string) error {
	ext := filepath.Ext(path)
	if ext == "" {
		return errors.New("import path has no extension, failed to determine import type")
	}

	parse, ok := extensionToParseFunc[ext]
	if !ok {
		return fmt.Errorf("import path extension '%v' is not supported!", ext)
	}

	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file to import at '%q': %w", path, err)
	}

	createdAt := st.ModTime()

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file to import at '%q': %w", path, err)
	}
	defer f.Close()

	var snapshotId int64
	err = db.Transaction(func(tx *database.Tx) error {
		result, err := tx.Exec(
			"INSERT INTO snapshot(disk_id, created_at) VALUES ($1, $2)",
			disk,
			database.FormatTime(createdAt),
		)
		if err != nil {
			return err
		}

		snapshotId, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to add snapshot to DB: %w", err)
	}

	const BATCH_SIZE = 1000
	batch := make([]File, 0, BATCH_SIZE)

	flush := func() error {
		err := db.Transaction(func(tx *database.Tx) error {
			return importBatch(tx, snapshotId, batch)
		})
		if err != nil {
			return fmt.Errorf("import transaction failed: %w", err)
		}

		batch = batch[:0]
		return nil
	}

	err = parse(f, func(file File) error {
		batch = append(batch, file)

		if len(batch) >= BATCH_SIZE {
			return flush()
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to parse %q: %w", path, err)
	}

	// the last batch is usually not full, so it has to be flushed here
	return flush()
}

func importBatch(tx *database.Tx, snapshotId int64, batch []File) error {
	if len(batch) == 0 {
		return nil
	}

	// A content is identified by its hash type together with its hash: the
	// same bytes hashed with a different algorithm are different contents.
	//
	// The hash is a string only so it can key a map; Go maps cannot key on
	// a slice. It is never stored and never bound as a query argument.
	type contentKey struct {
		hashType string
		hash     string
	}
	keyOf := func(file File) (contentKey, error) {
		hashType, err := file.HashType.ToIdentifier()
		if err != nil {
			return contentKey{}, fmt.Errorf("file %q: %w", file.path(), err)
		}
		return contentKey{hashType: hashType, hash: string(file.Hash)}, nil
	}

	// Contents are deduplicated, files are not: every File in the batch gets
	// its own observation, so the observation insert below iterates batch
	// and not this slice. Only the content upsert is collapsed, because a
	// repeated (hash_type, hash) within one multi-row insert would conflict
	// with itself.
	//
	// distinctContents keeps the first File per content; its Hash is bound
	// as-is, so the bytes are never re-encoded on the way to the DB.
	// distinctContentKeys is parallel to it.
	distinctContents := make([]File, 0, len(batch))
	distinctContentKeys := make([]contentKey, 0, len(batch))
	seen := make(map[contentKey]struct{}, len(batch))
	for _, file := range batch {
		key, err := keyOf(file)
		if err != nil {
			return err
		}

		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		distinctContents = append(distinctContents, file)
		distinctContentKeys = append(distinctContentKeys, key)
	}

	// v0 checksum files carry no size, so a content first seen in one is
	// stored with size 0 until an import that knows the size fills it in.
	// A size that is already known is never overwritten, so a later v0
	// line cannot blank it out again.
	var insertContent strings.Builder
	insertContent.WriteString(
		"INSERT INTO content (size, hash_type, hash) VALUES ")
	contentArgs := make([]any, 0, len(distinctContents)*3)
	for i, u := range distinctContents {
		if i > 0 {
			insertContent.WriteByte(',')
		}
		insertContent.WriteString("(?,?,?)")
		contentArgs = append(contentArgs,
			int64(u.SizeInBytes), distinctContentKeys[i].hashType, u.Hash)
	}
	insertContent.WriteString(
		" ON CONFLICT(hash_type, hash) DO UPDATE SET size = excluded.size" +
			" WHERE content.size = 0")

	if _, err := tx.Exec(insertContent.String(), contentArgs...); err != nil {
		return fmt.Errorf("failed to insert content: %w", err)
	}

	// The ids are resolved with a lookup instead of LastInsertId, which is
	// unchanged by a conflicting insert, and instead of RETURNING id, which
	// yields no row for one.
	var selectContent strings.Builder
	selectContent.WriteString(
		"SELECT hash_type, hash, id FROM content WHERE (hash_type, hash) IN (")
	contentKeys := make([]any, 0, len(distinctContents)*2)
	for i, u := range distinctContents {
		if i > 0 {
			selectContent.WriteByte(',')
		}
		selectContent.WriteString("(?,?)")
		contentKeys = append(contentKeys, distinctContentKeys[i].hashType, u.Hash)
	}
	selectContent.WriteByte(')')

	rows, err := tx.Query(selectContent.String(), contentKeys...)
	if err != nil {
		return fmt.Errorf("failed to look up content: %w", err)
	}
	defer rows.Close()

	contentIds := make(map[contentKey]base.ContentId, len(distinctContents))
	for rows.Next() {
		var (
			hashType string
			hash     []byte
			id       base.ContentId
		)
		if err := rows.Scan(&hashType, &hash, &id); err != nil {
			return fmt.Errorf("failed to read content: %w", err)
		}
		contentIds[contentKey{hashType: hashType, hash: string(hash)}] = id
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read content: %w", err)
	}

	// Every file in the batch is observed, including files whose content
	// another file already contributed above.
	var insertObservation strings.Builder
	insertObservation.WriteString(
		"INSERT INTO observation (snapshot_id, content_id, path, mtime) VALUES ")
	observationArgs := make([]any, 0, len(batch)*4)
	for i, file := range batch {
		key, err := keyOf(file)
		if err != nil {
			return err
		}

		contentId, ok := contentIds[key]
		if !ok {
			// the content was just inserted, so it has to be there
			return fmt.Errorf(
				"no content for %s hash %x of file %q",
				key.hashType, file.Hash, file.path())
		}

		if i > 0 {
			insertObservation.WriteByte(',')
		}
		insertObservation.WriteString("(?,?,?,?)")
		observationArgs = append(observationArgs,
			snapshotId,
			contentId,
			file.path(),
			database.FormatTime(file.MTime),
		)
	}

	if _, err := tx.Exec(insertObservation.String(), observationArgs...); err != nil {
		return fmt.Errorf("failed to insert observations: %w", err)
	}

	return nil
}
