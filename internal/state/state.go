// Package state centralizes the Linux/macOS policy for app-owned state paths.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var targets = []string{"config.json", "feeder.db", "feeder.lock", "feeder.db-journal", "feeder.db-wal", "feeder.db-shm"}

type Dir struct {
	file *os.File
}

// Open pins a directory descriptor. Existing targets are checked before any
// permission changes or creation of other state files. Home itself cannot be a
// symlink; symlinks in its parent path are resolved as part of selecting home.
func Open(home string, create bool) (*Dir, error) {
	if create {
		if err := os.MkdirAll(home, 0700); err != nil {
			return nil, err
		}
	}
	fd, err := unix.Open(home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	d := &Dir{file: os.NewFile(uintptr(fd), home)}
	if err := d.Check(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *Dir) Close() error   { return d.file.Close() }
func (d *Dir) Private() error { return d.file.Chmod(0700) }

// OpenFile never follows the final name and inspects the opened inode, rather
// than trusting a preceding path check. O_NONBLOCK avoids hanging on a FIFO.
// No truncation or chmod is allowed until these checks have succeeded.
func (d *Dir) OpenFile(name string, flags int) (*os.File, error) {
	if filepath.Base(name) != name || name == "." || name == ".." || flags&(os.O_TRUNC|os.O_APPEND) != 0 {
		return nil, errors.New("invalid state open")
	}
	fd, err := unix.Openat(int(d.file.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, fmt.Errorf("state target %s: %w", name, err)
	}
	f := os.NewFile(uintptr(fd), name)
	var info unix.Stat_t
	if err = unix.Fstat(fd, &info); err == nil && (info.Mode&unix.S_IFMT != unix.S_IFREG || info.Nlink != 1) {
		err = errors.New("must be a regular file with one link")
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("unsafe state target %s: %w", name, err)
	}
	return f, nil
}

// Check also covers SQLite-owned sidecars before the driver is allowed to open
// the database. These private paths must not alias files outside selected home.
func (d *Dir) Check() error {
	for _, name := range targets {
		f, err := d.OpenFile(name, os.O_RDONLY)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dir) Remove(name string) error { return unix.Unlinkat(int(d.file.Fd()), name, 0) }
func (d *Dir) Rename(old, new string) error {
	return unix.Renameat(int(d.file.Fd()), old, int(d.file.Fd()), new)
}
