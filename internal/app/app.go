// Package app orchestrates state operations without CLI output or SQL.
package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ContractorKeith/feeder/internal/config"
	"github.com/ContractorKeith/feeder/internal/storage"
)

// Dependencies is the command/application test seam. HTTP is reserved for M1;
// no request is made by the foundation commands.
type Dependencies struct {
	Now    func() time.Time
	HTTP   *http.Client
	Getenv func(string) string
}

func (d Dependencies) WithDefaults() Dependencies {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if d.Getenv == nil {
		d.Getenv = os.Getenv
	}
	return d
}

type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }
func Invalid(err error) error            { return &ValidationError{err} }

func Read(home string, d Dependencies, overrides map[string]string) (config.Config, error) {
	saved, err := config.Load(home)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, err
		}
		return nil, Invalid(err)
	}
	effective, err := saved.Effective(d.Getenv, overrides)
	if err != nil {
		return nil, Invalid(err)
	}
	return effective, nil
}

// Write validates before creating state, then reloads under the cross-process
// lock so competing config updates cannot overwrite one another.
func Write(ctx context.Context, home string, d Dependencies, overrides map[string]string, key, value string, init bool) (config.Config, []string, error) {
	// Validate the prospective saved settings before Lock can create a home.
	candidate, err := config.Load(home)
	if err != nil {
		return nil, nil, Invalid(err)
	}
	if !init {
		if err := candidate.Set(key, value); err != nil {
			return nil, nil, Invalid(err)
		}
	}
	if _, err := candidate.Effective(func(string) string { return "" }, nil); err != nil {
		return nil, nil, Invalid(err)
	}
	effective, err := candidate.Effective(d.Getenv, overrides)
	if err != nil {
		return nil, nil, Invalid(err)
	}
	wait, _ := time.ParseDuration(effective["lock_timeout"].(string))
	release, err := storage.Lock(ctx, home, wait)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	saved, err := config.Load(home)
	if err != nil {
		return nil, nil, Invalid(err)
	}
	warnings := []string{}
	if init {
		if _, err := os.Stat(filepath.Join(home, "config.json")); errors.Is(err, os.ErrNotExist) {
			zone, fallback := config.MachineTimezone(d.Getenv)
			// Persist the effective initialization timezone, without saving other overrides.
			if d.Getenv("FEEDER_TIMEZONE") != "" {
				zone = d.Getenv("FEEDER_TIMEZONE")
				fallback = false
			}
			if v, ok := overrides["timezone"]; ok {
				zone = v
				fallback = false
			}
			saved["timezone"] = zone
			if fallback {
				warnings = append(warnings, "Could not derive an IANA timezone; saved UTC. Use config set timezone ZONE to change it.")
			}
		}
	} else {
		if err = saved.Set(key, value); err != nil {
			return nil, nil, Invalid(err)
		}
	}
	if _, err := saved.Effective(func(string) string { return "" }, nil); err != nil {
		return nil, nil, Invalid(err)
	}
	effective, err = saved.Effective(d.Getenv, overrides)
	if err != nil {
		return nil, nil, Invalid(err)
	}
	db, err := storage.Open(ctx, home, d.Now())
	if err != nil {
		return nil, nil, err
	}
	if err = db.Close(); err != nil {
		return nil, nil, err
	}
	if err = config.Save(home, saved); err != nil {
		return nil, nil, err
	}
	return effective, warnings, nil
}
