package config_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ContractorKeith/feeder/internal/config"
)

func TestPlatformHomeSelection(t *testing.T) {
	base := t.TempDir()
	env := map[string]string{"XDG_DATA_HOME": filepath.Join(base, "xdg"), "FEEDER_HOME": filepath.Join(base, "env")}
	get := func(k string) string { return env[k] }
	h, err := config.Home(filepath.Join(base, "flag"), get)
	if err != nil || h != filepath.Join(base, "flag") {
		t.Fatal(h, err)
	}
	h, err = config.Home("", get)
	if err != nil || h != env["FEEDER_HOME"] {
		t.Fatal(h, err)
	}
	delete(env, "FEEDER_HOME")
	h, err = config.Home("", get)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" && h != filepath.Join(env["XDG_DATA_HOME"], "feeder") {
		t.Fatal(h)
	}
	if runtime.GOOS == "darwin" && !filepath.IsAbs(h) {
		t.Fatal(h)
	}
}
