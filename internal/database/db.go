package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/gofrs/flock"
	"modernc.org/sqlite"
	"modernc.org/sqlite/lib"
)

type DB struct {
	db          *sql.DB
	lock        *flock.Flock
	access      sync.RWMutex
	closeOnce   sync.Once
	closeErr    error
	recoveryErr error
}

type Tx struct {
	*sql.Tx
}

// dsnParams are appended to the DSN of every connection.
//
// foreign_keys is off by default in SQLite and the pragma is per-connection,
// so it cannot be enabled once on the pool; the driver applies this to each
// connection it hands out. Without it the FOREIGN KEY clauses below are
// parsed and then never enforced.
const dsnParams = "?_pragma=foreign_keys(1)"

var ErrBusy = errors.New("catalog is in use; stop other catalog commands or the server first")
var ErrNotFound = errors.New("not found")
var ErrValidation = errors.New("invalid request")
var ErrConflict = errors.New("conflict")

func Open(path string) (*DB, error) {
	return OpenContext(context.Background(), path)
}

func OpenContext(ctx context.Context, path string) (*DB, error) {
	canonical, lock, err := acquireCatalogLock(path)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", databaseDSN(canonical))
	if err != nil {
		lock.Close()
		return nil, fmt.Errorf("failed to open database at %v: %w", path, err)
	}

	// Open() doesn't necessarily open the connection right away
	// force with Ping()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		lock.Close()
		return nil, err
	}

	result := &DB{db: db, lock: lock}
	err = result.migrate(ctx)
	if err != nil {
		result.Close()
		return nil, fmt.Errorf("failed to initialize DB schema: %w", err)
	}

	var obsoleteSearch bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema
 WHERE name IN ('search_fuzzy_signature','search_short','search_fold_short'))`).Scan(&obsoleteSearch)
	if err != nil || obsoleteSearch {
		result.Close()
		if err != nil {
			return nil, fmt.Errorf("check search schema: %w", err)
		}
		return nil, fmt.Errorf("obsolete pre-0.1 search schema: recreate the catalog and reimport inventories")
	}

	if err := result.RecoverImports(ctx); err != nil {
		result.Close()
		return nil, fmt.Errorf("recover abandoned imports: %w", err)
	}

	return result, nil
}

func (db *DB) Transaction(fn func(*Tx) error) error {
	return db.TransactionContext(context.Background(), fn)
}

func (db *DB) TransactionContext(ctx context.Context, fn func(*Tx) error) error {
	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	wrapped := &Tx{tx}

	if err := fn(wrapped); err != nil {
		return classifyError(err)
	}

	return classifyError(tx.Commit())
}

func (db *DB) CreateDisk(label, notes, serial string, capacity int64) (int64, error) {
	return db.CreateDiskContext(context.Background(), label, notes, serial, capacity)
}

func (db *DB) CreateDiskContext(
	ctx context.Context,
	label, notes, serial string,
	capacity int64,
) (int64, error) {
	release, err := db.readAccess()
	if err != nil {
		return 0, err
	}
	defer release()
	if label == "" || capacity < 0 {
		return 0, fmt.Errorf("%w: label and nonnegative capacity required", ErrValidation)
	}

	var notesValue, serialValue *string
	if notes != "" {
		notesValue = &notes
	}

	if serial != "" {
		serialValue = &serial
	}

	var id int64
	err = db.TransactionContext(ctx, func(tx *Tx) error {
		result, err := tx.ExecContext(ctx,
			"INSERT INTO disk(label, notes, serial, capacity) VALUES ($1, $2, $3, $4)",
			label, notesValue, serialValue, capacity,
		)
		if err != nil {
			return err
		}

		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create disk: %w", err)
	}

	return id, nil
}

func (db *DB) DiskIDByLabel(label string) (int64, error) {
	return db.DiskIDByLabelContext(context.Background(), label)
}

func (db *DB) DiskIDByLabelContext(ctx context.Context, label string) (int64, error) {
	release, err := db.readAccess()
	if err != nil {
		return 0, err
	}
	defer release()

	var id int64
	err = db.db.QueryRowContext(ctx, "SELECT id FROM disk WHERE label = $1", label).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("failed to find disk with label %q: %w", label, ErrNotFound)
	}

	if err != nil {
		return 0, fmt.Errorf("failed to find disk with label %q: %w", label, err)
	}

	return id, nil
}

func (db *DB) Close() error {
	db.closeOnce.Do(func() { db.closeErr = errors.Join(db.db.Close(), db.lock.Close()) })
	return db.closeErr
}

func (db *DB) AcquireImport(ctx context.Context) (func(), error) {
	if !db.access.TryLock() {
		return nil, ErrBusy
	}

	if err := db.RecoverImports(ctx); err != nil {
		db.recoveryErr = err
		db.access.Unlock()
		return nil, err
	}

	db.recoveryErr = nil
	return db.access.Unlock, nil
}

func (db *DB) readAccess() (func(), error) {
	if !db.access.TryRLock() {
		return nil, ErrBusy
	}

	if db.recoveryErr != nil {
		db.access.RUnlock()
		return nil, fmt.Errorf("catalog recovery required: %w", db.recoveryErr)
	}

	return db.access.RUnlock, nil
}

func (db *DB) ImportCleanupFailed(err error) { db.recoveryErr = err }

func classifyError(err error) error {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return errors.Join(ErrConflict, err)
		case sqlite3.SQLITE_CONSTRAINT_CHECK, sqlite3.SQLITE_CONSTRAINT_NOTNULL:
			return errors.Join(ErrValidation, err)
		}
	}

	return err
}
