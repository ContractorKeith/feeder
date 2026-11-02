package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidInputs(t *testing.T) {
	for _, v := range []string{"", "1.0.0", "v1", "v1.2.3/evil", "v01.2.3"} {
		if validVersion(v) {
			t.Fatalf("accepted %q", v)
		}
	}
	for _, v := range []string{"v1.2.3", "v0.1.0-rc.1"} {
		if !validVersion(v) {
			t.Fatal(v)
		}
	}
	dir := t.TempDir()
	if err := validate(dir, "v1.2.3"); err == nil {
		t.Fatal("missing archives accepted")
	}
	if err := newOutput(dir); err == nil {
		t.Fatal("existing directory accepted")
	}
	if err := newOutput("relative"); err == nil {
		t.Fatal("relative output accepted")
	}
}

func TestArchiveValidation(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload")
	if err := os.Mkdir(payload, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range members {
		if err := os.WriteFile(filepath.Join(payload, name), []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	sums := ""
	for _, target := range targets {
		name := asset("v1.2.3", target)
		if err := archive(filepath.Join(dir, name), payload); err != nil {
			t.Fatal(err)
		}
		hash, err := fileHash(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		sums += hash + "  " + name + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if err := validateArchive(filepath.Join(dir, asset("v1.2.3", target))); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, asset("v1.2.3", targets[0])), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validate(dir, "v1.2.3"); err == nil {
		t.Fatal("corrupt archive accepted")
	}
}

func TestReproducibleArchiveAndFormula(t *testing.T) {
	dir := t.TempDir()
	stage := filepath.Join(dir, "stage")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range members {
		if err := os.WriteFile(filepath.Join(stage, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range targets {
		if err := archive(filepath.Join(dir, asset("v1.2.3", target)), stage); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := fileHash(filepath.Join(dir, asset("v1.2.3", targets[0])))
	again := filepath.Join(dir, "again.tar.gz")
	if err := archive(again, stage); err != nil {
		t.Fatal(err)
	}
	second, _ := fileHash(again)
	if first != second {
		t.Fatal("archive is nondeterministic")
	}
	var output strings.Builder
	if err := formula(&output, dir, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		name := asset("v1.2.3", target)
		hash, _ := fileHash(filepath.Join(dir, name))
		if !strings.Contains(output.String(), "https://github.com/ContractorKeith/feeder/releases/download/v1.2.3/"+name) || !strings.Contains(output.String(), hash) {
			t.Fatal("missing real URL/hash", target)
		}
	}
}
