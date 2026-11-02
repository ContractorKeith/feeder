package storage_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ContractorKeith/feeder/internal/storage"
)

func TestNewerSchemaIsPreserved(t *testing.T) {
	h := t.TempDir()
	db, err := storage.Open(context.Background(), h, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.Open(context.Background(), h, time.Now())
	if err == nil {
		db.Close()
		t.Fatal("accepted future schema")
	}
}

func TestMissingMigrationMetadataRejected(t *testing.T) {
	h := t.TempDir()
	db, err := storage.Open(context.Background(), h, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DROP TABLE schema_migrations; PRAGMA user_version=1; CREATE TABLE precious(value TEXT); INSERT INTO precious VALUES ('keep')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = storage.Open(context.Background(), h, time.Now())
	if err == nil {
		db.Close()
		t.Fatal("accepted missing migration metadata")
	}
}

func TestInconsistentSchemaPreservesData(t *testing.T) {
	for _, damage := range []string{
		"DROP TABLE schema_migrations; PRAGMA user_version=1",
		"DELETE FROM schema_migrations WHERE version=1",
		"ALTER TABLE items ADD COLUMN surprise TEXT",
		"PRAGMA user_version=0",
	} {
		t.Run(damage, func(t *testing.T) {
			h := t.TempDir()
			ctx := context.Background()
			db, err := storage.Open(ctx, h, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("CREATE TABLE precious(value TEXT); INSERT INTO precious VALUES ('keep'); " + damage); err != nil {
				t.Fatal(err)
			}
			db.Close()
			if reopened, err := storage.Open(ctx, h, time.Now()); err == nil {
				reopened.Close()
				t.Fatal("accepted inconsistency")
			}
			raw, err := sql.Open("sqlite", filepath.Join(h, "feeder.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			var value string
			if err = raw.QueryRow("SELECT value FROM precious").Scan(&value); err != nil || value != "keep" {
				t.Fatal("lost data", value, err)
			}
		})
	}
}
func TestM0MigratesWithoutResettingLedger(t *testing.T) {
	h := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(h, "feeder.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL); INSERT INTO schema_migrations VALUES(1,'2026-11-01T00:00:00Z'); PRAGMA user_version=1")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = storage.Open(context.Background(), h, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var stamp string
	if err = db.QueryRow("SELECT applied_at FROM schema_migrations WHERE version=1").Scan(&stamp); err != nil || stamp != "2026-11-01T00:00:00Z" {
		t.Fatal(stamp, err)
	}
	if _, err = storage.Register(context.Background(), db, []storage.Feed{{URL: "https://example.org/rss"}}); err != nil {
		t.Fatal(err)
	}
}
