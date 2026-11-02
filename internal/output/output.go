// Package output validates destinations and atomically publishes complete files.
package output

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var reserved = []string{"config.json", "feeder.db", "feeder.lock", "feeder.db-journal", "feeder.db-wal", "feeder.db-shm"}

type destination struct {
	dir   *os.File
	home  *os.File
	name  string
	force bool
}

func openDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func selectDestination(home, path string, force bool) (*destination, error) {
	if path == "" || path == "-" {
		return nil, errors.New("output file path must not be empty or -")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir, err := openDirectory(filepath.Dir(abs))
	if err != nil {
		return nil, err
	}
	d := &destination{dir: dir, name: filepath.Base(abs), force: force}
	d.home, err = openDirectory(home)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		d.close()
		return nil, err
	}
	if err = d.check(); err != nil {
		d.close()
		return nil, err
	}
	return d, nil
}
func (d *destination) close() {
	if d.home != nil {
		d.home.Close()
	}
	d.dir.Close()
}
func sameFile(a, b unix.Stat_t) bool { return a.Dev == b.Dev && a.Ino == b.Ino }

// check resolves only names relative to the selected directory descriptors.
func (d *destination) check() error {
	var target unix.Stat_t
	err := unix.Fstatat(int(d.dir.Fd()), d.name, &target, unix.AT_SYMLINK_NOFOLLOW)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if exists && target.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("output must be a regular file")
	}
	if d.home != nil {
		var dirInfo, homeInfo unix.Stat_t
		if err = unix.Fstat(int(d.dir.Fd()), &dirInfo); err != nil {
			return err
		}
		if err = unix.Fstat(int(d.home.Fd()), &homeInfo); err != nil {
			return err
		}
		for _, name := range reserved {
			if sameFile(dirInfo, homeInfo) && d.name == name {
				return errors.New("output cannot replace feeder state")
			}
			if exists {
				var stateInfo unix.Stat_t
				err = unix.Fstatat(int(d.home.Fd()), name, &stateInfo, unix.AT_SYMLINK_NOFOLLOW)
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				if err == nil && sameFile(target, stateInfo) {
					return errors.New("output aliases feeder state")
				}
			}
		}
	}
	if exists && !d.force {
		return errors.New("output exists; use --force to overwrite")
	}
	return nil
}

// Validate checks a destination without creating files; Write selects and checks
// its own pinned directory before performing any output operations.
func Validate(home, path string, force bool) error {
	if path == "-" {
		return nil
	}
	d, err := selectDestination(home, path, force)
	if err != nil {
		return err
	}
	d.close()
	return nil
}
func Write(home, path string, force bool, data []byte) error {
	return write(home, path, force, data, nil)
}

// afterSelect is a deterministic test seam after pinning and before creation.
func write(home, path string, force bool, data []byte, afterSelect func() error) error {
	d, err := selectDestination(home, path, force)
	if err != nil {
		return err
	}
	defer d.close()
	if afterSelect != nil {
		if err = afterSelect(); err != nil {
			return err
		}
	}
	dirFD := int(d.dir.Fd())
	temp := ".feeder-output-" + rand.Text()
	fd, err := unix.Openat(dirFD, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(dirFD, temp, 0)
	f := os.NewFile(uintptr(fd), temp)
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
	if err = d.check(); err != nil {
		return err
	}
	if force {
		return unix.Renameat(dirFD, temp, dirFD, d.name)
	}
	// Link publishes without replacing a destination created since validation.
	return unix.Linkat(dirFD, temp, dirFD, d.name, 0)
}
