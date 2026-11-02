package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ContractorKeith/feeder/internal/state"
)

// Exercise replacement after the initial validation, so a path check alone
// cannot satisfy the test: the actual open must inspect its own descriptor.
func TestOpenRejectsAliasesAddedAfterValidation(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			h := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			dir, err := state.Open(h, false)
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			target := filepath.Join(h, "config.json")
			if kind == "symlink" {
				err = os.Symlink(outside, target)
			} else {
				err = os.Link(outside, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			f, err := dir.OpenFile("config.json", os.O_RDWR|os.O_CREATE)
			if err == nil {
				f.Close()
				t.Fatal("opened alias installed after validation")
			}
			b, err := os.ReadFile(outside)
			if err != nil || string(b) != "keep" {
				t.Fatal("modified outside file", err)
			}
			info, err := os.Stat(outside)
			if err != nil || info.Mode().Perm() != 0644 {
				t.Fatal("changed outside permissions", err)
			}
		})
	}
}
