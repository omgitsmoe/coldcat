package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
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
	MTimeKnown         bool
	SizeInBytes        uint64
	SizeKnown          bool
	HashType           base.HashType
	Hash               []byte
}

func (f File) path() string { return f.PathRelativeToRoot + f.Name }

type FileFunc = func(File) error

type ProgressPhase string

const (
	ProgressImporting   ProgressPhase = "importing"
	ProgressDirectories ProgressPhase = "directories"
	ProgressPublishing  ProgressPhase = "publishing"
)

type Progress struct {
	SnapshotID        base.SnapshotId
	Phase             ProgressPhase
	CommittedFiles    int64
	StreamingComplete bool
}

type ProgressFunc func(Progress) error

const (
	defaultBatchSize     = 5000
	observationBatchSize = 1000
)

type Request struct {
	DiskID         base.DiskId
	Path           string
	CapturedAt     time.Time
	UseSourceMTime bool
	AllowRepeat    bool
	Progress       ProgressFunc
}

func Import(ctx context.Context, db *database.DB, req Request) (base.Snapshot, error) {
	if filepath.Ext(req.Path) != ".cshd" {
		return base.Snapshot{}, fmt.Errorf(
			"%w: unsupported import extension %q",
			database.ErrValidation,
			filepath.Ext(req.Path),
		)
	}

	if req.UseSourceMTime {
		if !req.CapturedAt.IsZero() {
			return base.Snapshot{}, fmt.Errorf(
				"%w: choose captured-at or source mtime",
				database.ErrValidation,
			)
		}
	}

	f, err := os.Open(req.Path)
	if err != nil {
		return base.Snapshot{}, fmt.Errorf("open input %q: %w", req.Path, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return base.Snapshot{}, fmt.Errorf("stat input %q: %w", req.Path, err)
	}

	if !st.Mode().IsRegular() {
		return base.Snapshot{}, fmt.Errorf(
			"%w: input must be a regular file",
			database.ErrValidation,
		)
	}

	if req.UseSourceMTime {
		req.CapturedAt = st.ModTime()
	}

	return ImportReader(ctx, db, req, f)
}

func ImportReader(
	ctx context.Context,
	db *database.DB,
	req Request,
	r io.Reader,
) (result base.Snapshot, err error) {
	return importReader(ctx, db, req, r, defaultBatchSize)
}

func importReader(
	ctx context.Context,
	db *database.DB,
	req Request,
	r io.Reader,
	batchSize int,
) (result base.Snapshot, err error) {
	if req.DiskID <= 0 || req.CapturedAt.IsZero() || req.CapturedAt.UTC().Year() < 1 ||
		req.CapturedAt.UTC().Year() > 9999 {
		return result, fmt.Errorf(
			"%w: disk and explicit valid capture time are required",
			database.ErrValidation,
		)
	}

	release, err := db.AcquireImport(ctx)
	if err != nil {
		return result, err
	}
	defer release()
	provenance := "explicit"
	if req.UseSourceMTime {
		provenance = "source_mtime"
	}

	var snapshotID int64
	err = db.TransactionContext(ctx, func(tx *database.Tx) error {
		var diskID int64
		if err := tx.QueryRowContext(ctx, "SELECT id FROM disk WHERE id=?", req.DiskID).Scan(&diskID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return database.ErrNotFound
			}

			return err
		}

		insert, err := tx.ExecContext(
			ctx,
			`INSERT INTO snapshot(disk_id,state,captured_at,imported_at,capture_provenance,input_path,input_format)
 VALUES(?,'importing',?,?,?,?, 'cshd')`,
			req.DiskID,
			database.FormatTime(req.CapturedAt),
			database.FormatTime(time.Now()),
			provenance,
			req.Path,
		)
		if err != nil {
			return err
		}

		snapshotID, err = insert.LastInsertId()
		return err
	})
	if err != nil {
		return result, err
	}
	defer func() {
		if err == nil {
			return
		}

		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if cleanupErr := db.CleanupImport(cleanupCtx, snapshotID); cleanupErr != nil {
			db.ImportCleanupFailed(cleanupErr)
			err = errors.Join(
				err,
				fmt.Errorf("cleanup snapshot %d failed: %w", snapshotID, cleanupErr),
			)
		}
	}()

	var committed int64
	report := func(phase ProgressPhase, final bool) error {
		if req.Progress != nil {
			if err := req.Progress(Progress{
				SnapshotID: base.SnapshotId(snapshotID), Phase: phase,
				CommittedFiles: committed, StreamingComplete: final,
			}); err != nil {
				return fmt.Errorf("report import progress: %w", err)
			}
		}
		return ctx.Err()
	}
	batch := make([]File, 0, batchSize)
	flush := func(final bool) error {
		if len(batch) == 0 {
			return ctx.Err()
		}

		if err := db.TransactionContext(ctx, func(tx *database.Tx) error { return importBatch(ctx, tx, snapshotID, batch) }); err != nil {
			return err
		}

		committed += int64(len(batch))
		batch = batch[:0]
		return report(ProgressImporting, final)
	}
	digest := newInventoryDigest()
	err = ParseCshd(contextReader{ctx: ctx, reader: r}, func(file File) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := digest.add(file); err != nil {
			return err
		}

		batch = append(batch, file)
		if len(batch) == batchSize {
			return flush(false)
		}

		return nil
	})
	if err != nil {
		return result, fmt.Errorf("parse %q: %w", req.Path, err)
	}

	if err = flush(true); err != nil {
		return result, err
	}

	if err = report(ProgressDirectories, true); err != nil {
		return result, err
	}

	if err = db.BuildDirectories(ctx, snapshotID); err != nil {
		return result, fmt.Errorf("build directories: %w", err)
	}

	if err = report(ProgressPublishing, true); err != nil {
		return result, err
	}

	result, err = db.PublishImport(
		ctx,
		database.PublishImportRequest{
			SnapshotID:   snapshotID,
			SourceDigest: digest.sum(),
			AllowRepeat:  req.AllowRepeat,
		},
	)
	if err != nil {
		return result, err
	}

	return result, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.reader.Read(p)
}

func importBatch(ctx context.Context, tx *database.Tx, snapshotID int64, batch []File) error {
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM snapshot WHERE id=?", snapshotID).Scan(&state); err != nil {
		return err
	}

	if state != "importing" {
		return fmt.Errorf("%w: snapshot is not importing", database.ErrConflict)
	}

	insert, err := tx.PrepareContext(
		ctx,
		"INSERT INTO content(size,hash_type,hash) VALUES(NULL,?,?) ON CONFLICT(hash_type,hash) DO NOTHING",
	)
	if err != nil {
		return err
	}
	defer insert.Close()
	lookup, err := tx.PrepareContext(
		ctx,
		"SELECT id,size FROM content WHERE hash_type=? AND hash=?",
	)
	if err != nil {
		return err
	}
	defer lookup.Close()

	observations := make([]any, 0, 4*observationBatchSize)
	flushObservations := func() error {
		query := "INSERT INTO observation(snapshot_id,content_id,path,mtime) VALUES " +
			strings.TrimSuffix(strings.Repeat("(?,?,?,?),", len(observations)/4), ",")
		if _, err := tx.ExecContext(ctx, query, observations...); err != nil {
			return fmt.Errorf("observe %d paths from %q through %q: %w",
				len(observations)/4, observations[2], observations[len(observations)-2], err)
		}
		observations = observations[:0]
		return nil
	}

	trackContent, err := tx.PrepareContext(ctx,
		"INSERT INTO import_content(snapshot_id,content_id) VALUES(?,?)")
	if err != nil {
		return err
	}
	defer trackContent.Close()

	lookupSize, err := tx.PrepareContext(ctx,
		"SELECT size FROM pending_size WHERE snapshot_id=? AND content_id=?")
	if err != nil {
		return err
	}
	defer lookupSize.Close()

	stageSize, err := tx.PrepareContext(ctx,
		"INSERT INTO pending_size(snapshot_id,content_id,size) VALUES(?,?,?)")
	if err != nil {
		return err
	}
	defer stageSize.Close()

	for _, file := range batch {
		if err := validateFile(file); err != nil {
			return fmt.Errorf("file %q: %w", file.path(), err)
		}

		algorithm, err := file.HashType.ToIdentifier()
		if err != nil {
			return fmt.Errorf("file %q: %w", file.path(), err)
		}

		created, err := insert.ExecContext(ctx, algorithm, file.Hash)
		if err != nil {
			return err
		}

		var id int64
		var known sql.NullInt64
		if err := lookup.QueryRowContext(ctx, algorithm, file.Hash).Scan(&id, &known); err != nil {
			return err
		}

		added, err := created.RowsAffected()
		if err != nil {
			return err
		}

		if added > 0 {
			if _, err := trackContent.ExecContext(ctx, snapshotID, id); err != nil {
				return err
			}
		}

		if file.SizeKnown {
			size := int64(file.SizeInBytes)
			if known.Valid && known.Int64 != size {
				return fmt.Errorf(
					"%w: file %q has size %d, content has known size %d",
					database.ErrConflict,
					file.path(),
					size,
					known.Int64,
				)
			}

			if !known.Valid {
				var staged int64
				err := lookupSize.QueryRowContext(ctx, snapshotID, id).Scan(&staged)
				if err == nil && staged != size {
					return fmt.Errorf(
						"%w: file %q conflicts with staged content size",
						database.ErrConflict,
						file.path(),
					)
				}

				if errors.Is(err, sql.ErrNoRows) {
					if _, err := stageSize.ExecContext(ctx, snapshotID, id, size); err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
			}
		}

		var mtime any
		if file.MTimeKnown {
			mtime = database.FormatTime(file.MTime)
		}

		observations = append(observations, snapshotID, id, file.path(), mtime)
		if len(observations) == 4*observationBatchSize {
			if err := flushObservations(); err != nil {
				return err
			}
		}
	}
	if len(observations) > 0 {
		return flushObservations()
	}

	return nil
}

func validateFile(file File) error {
	path := file.path()
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsRune(path, 0) ||
		(len(path) > 1 && path[1] == ':' && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z'))) {
		return fmt.Errorf("%w: invalid disk-relative path", database.ErrValidation)
	}

	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("%w: invalid path segment", database.ErrValidation)
		}
	}

	if file.SizeKnown && file.SizeInBytes > math.MaxInt64 {
		return fmt.Errorf("%w: size overflows signed 64-bit storage", database.ErrValidation)
	}

	expected, err := file.HashType.DigestSize()
	if err != nil {
		return fmt.Errorf("%w: %w", database.ErrValidation, err)
	}

	if len(file.Hash) != expected {
		return fmt.Errorf(
			"%w: %s hash must be %d bytes, got %d",
			database.ErrValidation, file.HashType.Hash.String(), expected, len(file.Hash),
		)
	}

	if file.MTimeKnown && (file.MTime.UTC().Year() < 1 || file.MTime.UTC().Year() > 9999) {
		return fmt.Errorf("%w: mtime outside supported range", database.ErrValidation)
	}

	return nil
}
