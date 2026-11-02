package storage

import (
	"context"
	"database/sql"
	"errors"
)

const summaryDDL = "CREATE TABLE summaries (item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE, cache_key TEXT NOT NULL, text TEXT NOT NULL CHECK(text <> ''), PRIMARY KEY(item_id, cache_key))"

func Summary(ctx context.Context, db *sql.DB, itemID int64, key string) (string, error) {
	var text string
	err := db.QueryRowContext(ctx, "SELECT text FROM summaries WHERE item_id=? AND cache_key=?", itemID, key).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return text, err
}
func SaveSummary(ctx context.Context, db *sql.DB, itemID int64, key, text string) error {
	_, err := db.ExecContext(ctx, "INSERT INTO summaries(item_id,cache_key,text) VALUES (?,?,?) ON CONFLICT(item_id,cache_key) DO UPDATE SET text=excluded.text", itemID, key, text)
	return err
}
