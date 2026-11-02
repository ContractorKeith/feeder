package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var domainSchema = []string{
	`CREATE TABLE subscriptions (id INTEGER PRIMARY KEY AUTOINCREMENT, url TEXT NOT NULL UNIQUE, title TEXT NOT NULL, active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)), initialized INTEGER NOT NULL DEFAULT 0 CHECK(initialized IN (0,1)), etag TEXT NOT NULL DEFAULT '', modified TEXT NOT NULL DEFAULT '', last_attempt TEXT NOT NULL DEFAULT '', last_success TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '')`,
	`CREATE TABLE tags (feed_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE, tag TEXT NOT NULL, PRIMARY KEY(feed_id, tag))`,
	`CREATE TABLE items (id INTEGER PRIMARY KEY AUTOINCREMENT, feed_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE, item_key TEXT NOT NULL, title TEXT NOT NULL, url TEXT NOT NULL, content TEXT NOT NULL, content_hash TEXT NOT NULL, first_seen TEXT NOT NULL, published TEXT NOT NULL, baseline INTEGER NOT NULL CHECK(baseline IN (0,1)), UNIQUE(feed_id,item_key))`,
}

const migrationDDL = "CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)"

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func versionAfterInit(v int) int {
	if v == 0 {
		return 1
	}
	return v
}

// Check both migration ledger and owned DDL; never repair a damaged schema.
func validateSchema(ctx context.Context, q queryer, version int) error {
	var integrity string
	if err := q.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		return errors.New("database integrity check failed")
	}
	ddls := []string{migrationDDL}
	if version >= 2 {
		ddls = append(ddls, domainSchema...)
	}
	if version >= 3 {
		ddls = append(ddls, summaryDDL)
	}
	for _, ddl := range ddls {
		name := strings.Fields(ddl)[2]
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&actual); err != nil || actual != ddl {
			return fmt.Errorf("inconsistent database schema: %s", name)
		}
	}
	var count int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version BETWEEN 1 AND ? AND applied_at <> ''", version).Scan(&count); err != nil || count != version {
		return errors.New("inconsistent migration history")
	}
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != version {
		return errors.New("inconsistent migration history")
	}
	rows, err := q.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("inconsistent database foreign keys")
	}
	return rows.Err()
}

type Feed struct {
	ID          int64    `json:"id"`
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Tags        []string `json:"tags"`
	Active      bool     `json:"active"`
	Initialized bool     `json:"initialized"`
	ETag        string   `json:"etag"`
	Modified    string   `json:"last_modified"`
	LastAttempt string   `json:"last_attempt"`
	LastSuccess string   `json:"last_success"`
	LastError   string   `json:"last_error"`
}
type Item struct {
	ID        int64  `json:"id"`
	FeedID    int64  `json:"feed_id"`
	Key       string `json:"key"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Content   string `json:"content"`
	Hash      string `json:"content_hash"`
	FirstSeen string `json:"first_seen"`
	Published string `json:"published"`
	Baseline  bool   `json:"baseline"`
}

func List(ctx context.Context, db *sql.DB, all bool) ([]Feed, error) {
	feeds := []Feed{}
	// M0 has no subscriptions yet; read commands do not migrate.
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return nil, err
	}
	if version == 1 {
		return feeds, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id,url,title,active,initialized,etag,modified,last_attempt,last_success,last_error FROM subscriptions WHERE active=1 OR ? ORDER BY id`, all)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f Feed
		f.Tags = []string{}
		if err = rows.Scan(&f.ID, &f.URL, &f.Title, &f.Active, &f.Initialized, &f.ETag, &f.Modified, &f.LastAttempt, &f.LastSuccess, &f.LastError); err != nil {
			rows.Close()
			return nil, err
		}
		feeds = append(feeds, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range feeds {
		rows, err = db.QueryContext(ctx, "SELECT tag FROM tags WHERE feed_id=? ORDER BY tag", feeds[i].ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var tag string
			if err = rows.Scan(&tag); err != nil {
				rows.Close()
				return nil, err
			}
			feeds[i].Tags = append(feeds[i].Tags, tag)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return feeds, nil
}

func setTags(ctx context.Context, tx *sql.Tx, id int64, tags []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM tags WHERE feed_id=?", id); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO tags VALUES (?,?)", id, tag); err != nil {
			return err
		}
	}
	return nil
}

// Register preserves identity/history and merges tags on repeated imports/adds.
func Register(ctx context.Context, db *sql.DB, feeds []Feed) ([]int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids := []int64{}
	for _, f := range feeds {
		_, err = tx.ExecContext(ctx, `INSERT INTO subscriptions(url,title) VALUES (?,?) ON CONFLICT(url) DO UPDATE SET active=1,title=CASE WHEN excluded.title='' THEN subscriptions.title ELSE excluded.title END`, f.URL, f.Title)
		if err != nil {
			return nil, err
		}
		var id int64
		if err = tx.QueryRowContext(ctx, "SELECT id FROM subscriptions WHERE url=?", f.URL).Scan(&id); err != nil {
			return nil, err
		}
		for _, tag := range f.Tags {
			if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO tags VALUES (?,?)", id, tag); err != nil {
				return nil, err
			}
		}
		ids = append(ids, id)
	}
	return ids, tx.Commit()
}

var ErrFeed = errors.New("subscription not found or not eligible for operation")

func Change(ctx context.Context, db *sql.DB, id int64, action string, tags []string) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var active bool
	if err = tx.QueryRowContext(ctx, "SELECT active FROM subscriptions WHERE id=?", id).Scan(&active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrFeed
		}
		return 0, err
	}
	var count int
	switch action {
	case "tag":
		err = setTags(ctx, tx, id, tags)
	case "remove":
		_, err = tx.ExecContext(ctx, "UPDATE subscriptions SET active=0 WHERE id=?", id)
	case "purge", "preview":
		if active {
			return 0, ErrFeed
		}
		err = tx.QueryRowContext(ctx, "SELECT count(*) FROM items WHERE feed_id=?", id).Scan(&count)
		if err == nil && action == "purge" {
			_, err = tx.ExecContext(ctx, "DELETE FROM subscriptions WHERE id=?", id)
		}
	default:
		return 0, errors.New("unknown subscription operation")
	}
	if err != nil {
		return 0, err
	}
	return count, tx.Commit()
}

// CommitFetch holds no transaction while fetching; items and validators commit together.
func CommitFetch(ctx context.Context, db *sql.DB, f Feed, items []Item, now time.Time, etag, modified string, unchanged bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	if !unchanged {
		for _, it := range items {
			_, err = tx.ExecContext(ctx, `INSERT INTO items(feed_id,item_key,title,url,content,content_hash,first_seen,published,baseline) VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(feed_id,item_key) DO UPDATE SET title=excluded.title,url=excluded.url,content=excluded.content,content_hash=excluded.content_hash,published=excluded.published`, f.ID, it.Key, it.Title, it.URL, it.Content, it.Hash, stamp, it.Published, !f.Initialized)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE subscriptions SET initialized=1,etag=?,modified=? WHERE id=?", etag, modified, f.ID)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE subscriptions SET last_attempt=?,last_success=?,last_error='' WHERE id=?", stamp, stamp, f.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func FailedFetch(ctx context.Context, db *sql.DB, id int64, now time.Time, message string) error {
	_, err := db.ExecContext(ctx, "UPDATE subscriptions SET last_attempt=?,last_error=? WHERE id=?", now.UTC().Format(time.RFC3339Nano), message, id)
	return err
}

// Items exposes stored source records to digest selection without leaking SQL.
func Items(ctx context.Context, db *sql.DB) ([]Item, error) {
	rows, err := db.QueryContext(ctx, "SELECT id,feed_id,item_key,title,url,content,content_hash,first_seen,published,baseline FROM items ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var it Item
		if err = rows.Scan(&it.ID, &it.FeedID, &it.Key, &it.Title, &it.URL, &it.Content, &it.Hash, &it.FirstSeen, &it.Published, &it.Baseline); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
