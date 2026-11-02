// Package storage owns SQLite schema and migrations. Callers hold the writer lock.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/ContractorKeith/feeder/internal/state"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

var ErrBusy = errors.New("writer busy; retry or increase --lock-timeout")

// Lock creates private state and holds an OS advisory lock until release.
func Lock(ctx context.Context, home string, wait time.Duration) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := state.Open(home, true)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if err = dir.Private(); err != nil {
		return nil, err
	}
	file, err := dir.OpenFile("feeder.lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	deadline, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		if err = ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return file.Close, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			file.Close()
			return nil, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-deadline.Done():
			timer.Stop()
			file.Close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrBusy
		case <-timer.C:
		}
	}
}

// Open migrates transactionally and refuses to overwrite a newer or corrupt DB.
func Open(ctx context.Context, home string, now time.Time) (*sql.DB, error) {
	path := filepath.Join(home, "feeder.db")
	dir, err := state.Open(home, false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	f, err := dir.OpenFile("feeder.db", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*sql.DB, error) { db.Close(); return nil, err }
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fail(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version > 1 {
		return fail(errors.New("database schema is newer than this feeder; upgrade feeder"))
	}
	if version == 0 {
		_, err = tx.ExecContext(ctx, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
INSERT INTO schema_migrations(version, applied_at) VALUES (1, ?);
PRAGMA user_version = 1;`, now.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return fail(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return db, nil
}
