package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/ContractorKeith/feeder/internal/storage"
)

// withStore validates settings before acquiring the command's bounded writer lock.
func withStore(ctx context.Context, home string, d Dependencies, overrides map[string]string, write bool, fn func(*sql.DB) error) error {
	c, err := Read(home, d, overrides)
	if err != nil {
		return err
	}
	if write {
		wait, _ := time.ParseDuration(c["lock_timeout"].(string))
		release, err := storage.Lock(ctx, home, wait)
		if err != nil {
			return err
		}
		defer release()
		db, err := storage.Open(ctx, home, d.Now())
		if err != nil {
			return err
		}
		defer db.Close()
		return fn(db)
	}
	db, err := storage.OpenRead(ctx, home)
	if errors.Is(err, os.ErrNotExist) {
		return fn(nil)
	}
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(db)
}

func Tags(tags []string) ([]string, error) {
	result := []string{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || strings.ContainsRune(tag, ',') || strings.IndexFunc(tag, unicode.IsControl) >= 0 {
			return nil, Invalid(errors.New("tags must be nonempty text without commas or control characters"))
		}
		result = append(result, tag)
	}
	return result, nil
}

// Collection owns subscription operations and their state/lock lifecycle.
type Collection struct {
	Context      context.Context
	Home         string
	Dependencies Dependencies
	Overrides    map[string]string
}

func (c Collection) store(write bool, fn func(*sql.DB) error) error {
	return withStore(c.Context, c.Home, c.Dependencies, c.Overrides, write, fn)
}
func (c Collection) Add(raw, title string, tags []string) (int64, error) {
	u, err := FeedURL(raw)
	if err != nil {
		return 0, err
	}
	tags, err = Tags(tags)
	if err != nil {
		return 0, err
	}
	ids, err := c.register([]storage.Feed{{URL: u, Title: PlainText(title), Tags: tags}})
	if err != nil {
		return 0, err
	}
	return ids[0], nil
}
func (c Collection) register(feeds []storage.Feed) ([]int64, error) {
	var ids []int64
	err := c.store(true, func(db *sql.DB) error { var err error; ids, err = storage.Register(c.Context, db, feeds); return err })
	return ids, err
}
func (c Collection) List(all bool) ([]storage.Feed, error) {
	feeds := []storage.Feed{}
	err := c.store(false, func(db *sql.DB) error {
		if db == nil {
			return nil
		}
		var err error
		feeds, err = storage.List(c.Context, db, all)
		return err
	})
	return feeds, err
}
func (c Collection) Change(id int64, operation string, tags []string) (int, error) {
	tags, err := Tags(tags)
	if err != nil {
		return 0, err
	}
	count := 0
	err = c.store(true, func(db *sql.DB) error {
		var err error
		count, err = storage.Change(c.Context, db, id, operation, tags)
		return err
	})
	if errors.Is(err, storage.ErrFeed) {
		err = Invalid(err)
	}
	return count, err
}
func (c Collection) Fetch() ([]FetchResult, error) {
	settings, err := Read(c.Home, c.Dependencies, c.Overrides)
	if err != nil {
		return nil, err
	}
	timeout, _ := time.ParseDuration(settings["feed_timeout"].(string))
	var results []FetchResult
	err = c.store(true, func(db *sql.DB) error {
		var err error
		results, err = fetch(c.Context, db, c.Dependencies, timeout)
		return err
	})
	return results, err
}
func (c Collection) Import(reader io.Reader) ([]int64, error) {
	feeds, err := ImportOPML(reader)
	if err != nil {
		return nil, err
	}
	return c.register(feeds)
}
func (c Collection) Export() ([]byte, error) {
	feeds, err := c.List(false)
	if err != nil {
		return nil, err
	}
	return ExportOPML(feeds)
}
