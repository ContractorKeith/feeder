// Package config owns non-secret settings and their validation.
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/ContractorKeith/feeder/internal/state"
)

type Config map[string]any

func Defaults() Config {
	return Config{"timezone": "UTC", "summarize": false, "provider": "ollama", "model": "", "endpoint": "", "api_key_file": "", "limit": 20, "max_input_chars": 12000, "max_output_tokens": 256, "feed_timeout": "30s", "model_timeout": "1m0s", "lock_timeout": "10s"}
}

// Set parses one setting without including untrusted values in errors.
func (c Config) Set(key, value string) error {
	invalid := func() error { return fmt.Errorf("invalid setting %s (see config help)", key) }
	switch key {
	case "timezone":
		if value == "Local" || value == "" {
			return invalid()
		}
		if _, err := time.LoadLocation(value); err != nil {
			return invalid()
		}
		c[key] = value
	case "summarize":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return invalid()
		}
		c[key] = v
	case "provider":
		if value != "ollama" && value != "openai" {
			return invalid()
		}
		c[key] = value
	case "endpoint":
		if value != "" {
			u, err := url.Parse(value)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
				return invalid()
			}
			if port := u.Port(); port != "" {
				n, err := strconv.ParseUint(port, 10, 16)
				if err != nil || n == 0 {
					return invalid()
				}
			} else if strings.HasSuffix(u.Host, ":") {
				return invalid()
			}
		}
		c[key] = value
	case "model", "api_key_file":
		if strings.ContainsAny(value, "\x00\r\n\x1b") {
			return invalid()
		}
		c[key] = value
	case "limit", "max_input_chars", "max_output_tokens":
		v, err := strconv.Atoi(value)
		if err != nil || v <= 0 || (key == "limit" && v > 200) {
			return invalid()
		}
		c[key] = v
	case "feed_timeout", "model_timeout", "lock_timeout":
		v, err := time.ParseDuration(value)
		if err != nil || v <= 0 {
			return invalid()
		}
		c[key] = v.String()
	default:
		return errors.New("unknown setting (see config help)")
	}
	return nil
}

func Load(home string) (Config, error) {
	c := Defaults()
	dir, err := state.Open(home, false)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	f, err := dir.OpenFile("config.json", os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const maxConfigBytes = 1 << 20
	b, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxConfigBytes {
		return nil, errors.New("config.json exceeds 1 MiB limit")
	}
	var values map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&values); err != nil {
		return nil, errors.New("invalid config.json; restore valid settings")
	}
	if values == nil {
		return nil, errors.New("config.json must contain a settings object")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid config.json trailing data")
	}
	for key, raw := range values {
		var value any
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if err := d.Decode(&value); err != nil {
			return nil, errors.New("invalid config.json")
		}
		expected, ok := c[key]
		if !ok {
			return nil, errors.New("unknown saved setting")
		}
		validType := false
		switch expected.(type) {
		case string:
			_, validType = value.(string)
		case bool:
			_, validType = value.(bool)
		case int:
			_, validType = value.(json.Number)
		}
		if !validType {
			return nil, fmt.Errorf("invalid type for setting %s", key)
		}
		if err := c.Set(key, fmt.Sprint(value)); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c Config) Effective(getenv func(string) string, overrides map[string]string) (Config, error) {
	out := Defaults()
	for k, v := range c {
		out[k] = v
	}
	for k := range out {
		if _, overridden := overrides[k]; overridden {
			continue
		}
		if v := getenv("FEEDER_" + strings.ToUpper(k)); v != "" {
			if err := out.Set(k, v); err != nil {
				return nil, err
			}
		}
	}
	for k, v := range overrides {
		if err := out.Set(k, v); err != nil {
			return nil, err
		}
	}
	// OpenAI must never inherit Ollama's default destination.
	if out["provider"] == "openai" && out["endpoint"] == "" {
		return nil, errors.New("openai requires an explicit endpoint")
	}
	if out["provider"] == "ollama" && out["endpoint"] == "" {
		out["endpoint"] = "http://localhost:11434"
	}
	if out["summarize"] == true && strings.TrimSpace(out["model"].(string)) == "" {
		return nil, errors.New("summarize requires a model")
	}
	return out, nil
}

func Home(explicit string, getenv func(string) string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if h := getenv("FEEDER_HOME"); h != "" {
		return filepath.Abs(h)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(h, "Library", "Application Support", "feeder"), nil
	}
	if x := getenv("XDG_DATA_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "feeder"), nil
	}
	return filepath.Join(h, ".local", "share", "feeder"), nil
}

// MachineTimezone never persists a fixed-offset abbreviation.
func MachineTimezone(getenv func(string) string) (string, bool) {
	candidates := []string{strings.TrimPrefix(getenv("TZ"), ":")}
	if path, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, after, ok := strings.Cut(path, "zoneinfo/"); ok {
			candidates = append(candidates, after)
		}
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		candidates = append(candidates, strings.TrimSpace(string(b)))
	}
	for _, s := range candidates {
		if s == "UTC" || strings.Contains(s, "/") {
			c := Defaults()
			if c.Set("timezone", s) == nil {
				return s, false
			}
		}
	}
	return "UTC", true
}

// Save atomically replaces only the settings file, retaining mode 0600.
func Save(home string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir, err := state.Open(home, false)
	if err != nil {
		return err
	}
	defer dir.Close()
	// An unpredictable, exclusive descriptor-relative file cannot be redirected
	// through an attacker-prepared temp symlink or hardlink.
	name := ".config-" + rand.Text()
	f, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	defer dir.Remove(name)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
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
	return dir.Rename(name, "config.json")
}
