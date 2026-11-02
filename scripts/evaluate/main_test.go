package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectIncompleteOrUnsafeRuns(t *testing.T) {
	for _, extra := range [][]string{nil, {"--provider", "bad"}, {"--provider", "ollama"}, {"--provider", "fixture", "--model", "real"}, {"--provider", "openai", "--model", "model", "--endpoint", "http://user:secret@example.org"}} {
		out := filepath.Join(t.TempDir(), "evidence")
		args := append([]string{"--binary", "/unused/feeder", "--out", out}, extra...)
		if err := run(args); err == nil {
			t.Fatal("accepted", extra)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatal("invalid run created output", err)
		}
	}
	dir := t.TempDir()
	if err := run([]string{"--binary", "/unused/feeder", "--out", dir, "--provider", "fixture"}); err == nil {
		t.Fatal("accepted existing output")
	}
}
