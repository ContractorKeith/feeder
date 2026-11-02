// Package output validates destinations and atomically publishes complete files.
package output

import (
	"errors"
	"os"
	"path/filepath"
)

// Validate refuses state targets, aliases and nonregular destinations.
func Validate(home, path string, force bool) error {
	if path == "" {
		return errors.New("output path must not be empty")
	}
	if path == "-" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return err
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	homeAbs, err := filepath.Abs(home)
	if err != nil {
		return err
	}
	// Resolve the longest existing ancestor, including when state is not created yet.
	canonical := func(p string) string {
		tail := []string{}
		for {
			if real, e := filepath.EvalSymlinks(p); e == nil {
				for i := len(tail) - 1; i >= 0; i-- {
					real = filepath.Join(real, tail[i])
				}
				return real
			}
			next := filepath.Dir(p)
			if next == p {
				return p
			}
			tail = append(tail, filepath.Base(p))
			p = next
		}
	}
	homeAbs = canonical(homeAbs)
	info, statErr := os.Lstat(abs)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if info != nil {
		if !info.Mode().IsRegular() {
			return errors.New("output must be a regular file")
		}
		if !force {
			return errors.New("output exists; use --force to overwrite")
		}
	}
	for _, name := range []string{"config.json", "feeder.db", "feeder.lock", "feeder.db-journal", "feeder.db-wal", "feeder.db-shm"} {
		target := filepath.Join(homeAbs, name)
		if abs == target {
			return errors.New("output cannot replace feeder state")
		}
		if stateInfo, e := os.Stat(target); e == nil && info != nil && os.SameFile(info, stateInfo) {
			return errors.New("output aliases feeder state")
		}
	}
	return nil
}
func Write(home, path string, force bool, data []byte) error {
	if err := Validate(home, path, force); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".feeder-output-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = Validate(home, path, force); err != nil {
		return err
	}
	if force {
		return os.Rename(name, path)
	}
	// Link publishes without a check/rename overwrite race.
	return os.Link(name, path)
}
