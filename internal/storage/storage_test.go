package storage_test

import (
	"context"
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
