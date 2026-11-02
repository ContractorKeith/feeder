package output

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteKeepsSelectedDirectoryWhenAliasChanges(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-overwrite", true: "force"}[force], func(t *testing.T) {
			safe, home := t.TempDir(), t.TempDir()
			const name = "sentinel.txt"
			sentinel := filepath.Join(home, name)
			if err := os.WriteFile(sentinel, []byte("disposable sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			if force {
				if err := os.WriteFile(filepath.Join(safe, name), []byte("previous output"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(t.TempDir(), "selected")
			if err := os.Symlink(safe, alias); err != nil {
				t.Fatal(err)
			}
			err := write(home, filepath.Join(alias, name), force, []byte("complete output"), func() error {
				if err := os.Remove(alias); err != nil {
					return err
				}
				return os.Symlink(home, alias)
			})
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(safe, name))
			if err != nil || string(b) != "complete output" {
				t.Fatal("output left selected directory", string(b), err)
			}
			b, err = os.ReadFile(sentinel)
			if err != nil || string(b) != "disposable sentinel" {
				t.Fatal("changed disposable sentinel", string(b), err)
			}
			for _, dir := range []string{safe, home} {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 1 || entries[0].Name() != name {
					t.Fatal("unexpected output or leaked temporary file", dir, entries, err)
				}
			}
		})
	}
}

func TestReservedStateNamesUseSelectedDirectoryIdentity(t *testing.T) {
	home := t.TempDir()
	alias := filepath.Join(t.TempDir(), "collection-alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "feeder.db", "feeder.lock", "feeder.db-journal", "feeder.db-wal", "feeder.db-shm"} {
		if err := Write(home, filepath.Join(alias, name), true, []byte("output")); err == nil {
			t.Fatal("accepted reserved state target", name)
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("reserved path check created files", entries, err)
	}
}

func TestNoOverwritePreservesFileCreatedAfterSelection(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, "sentinel.txt")
	err := write(home, path, false, []byte("output"), func() error {
		return os.WriteFile(path, []byte("disposable sentinel"), 0600)
	})
	if err == nil {
		t.Fatal("replaced newly created output")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "disposable sentinel" {
		t.Fatal(string(b), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel.txt" {
		t.Fatal("leaked temporary output", entries, err)
	}
}
