package selfupdate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"webtyp.com/update"
)

var (
	// ErrUnhealthy: the new version did not answer /health in time; the previous one is back.
	ErrUnhealthy = errors.New("selfupdate: the new version did not become healthy; the previous one is back")
	// ErrRollback: going back to the previous version failed too.
	ErrRollback = errors.New("selfupdate: could not return to the previous version")
	// ErrMigrate: the new version's migration failed; nothing was switched.
	ErrMigrate = errors.New("selfupdate: the new version's migration failed; nothing was switched")
)

const (
	defaultWait      = 120 * time.Second
	defaultEvery     = 3 * time.Second
	defaultKeep      = 7
	backupSuffix     = ".sql"
	backupDirMode    = 0o700
	backupTimeLayout = "20060102-150405"
	backupNameSep    = "_"
	noVersion        = "none"
)

// Engine is what applying a version needs from Docker. Compose implements it.
type Engine interface {
	Backup(w io.Writer) error // dump the database into w
	Pull(ref string) error    // fetch image@digest
	Migrate(ref string) error // run the migration program of image ref with the owner user
	Switch(ref string) error  // write APP_IMAGE=ref in .env and recreate only the app service
	Healthy() bool            // one probe of the app's /health
}

// Applier switches the app to a verified version and returns to the previous
// one when the new one does not become healthy.
// Example: next, err := selfupdate.Applier{Engine: selfupdate.Compose{Dir: dir}, Dir: dir}.Apply(state, m)
type Applier struct {
	Engine Engine
	Dir    string                // the app's folder, /opt/<app>
	Wait   time.Duration         // how long to wait for health; 0 = 120s
	Every  time.Duration         // between probes; 0 = 3s
	Keep   int                   // database dumps kept; 0 = 7
	Now    func() time.Time      // nil = time.Now
	Sleep  func(time.Duration)   // nil = time.Sleep
}

// Apply runs docs/diagrams/APPLY_FLOW.md and returns the state to keep (also saved).
func (a Applier) Apply(s State, m update.Manifest) (State, error) {
	if err := a.backup(s.Version); err != nil {
		return s, err
	}
	ref := m.Reference()
	if err := a.Engine.Pull(ref); err != nil {
		return s, err
	}
	if err := a.Engine.Migrate(ref); err != nil {
		failed := s
		failed.Failed = append(append([]string(nil), s.Failed...), m.Version)
		result := fmt.Errorf("%w: %v", ErrMigrate, err)
		if saveErr := failed.Save(a.statePath()); saveErr != nil {
			return failed, errors.Join(result, saveErr)
		}
		return failed, errors.Join(result, err)
	}
	switchErr := a.Engine.Switch(ref)
	if switchErr == nil && a.waitHealthy() {
		next := State{Version: m.Version, Image: ref, PreviousVersion: s.Version, PreviousImage: s.Image, Failed: s.Failed}
		return next, next.Save(a.statePath())
	}
	failed := s
	failed.Failed = append(append([]string(nil), s.Failed...), m.Version)
	result := ErrUnhealthy
	if switchErr != nil {
		result = errors.Join(ErrUnhealthy, switchErr)
	}
	if s.Image != "" {
		if err := a.Engine.Switch(s.Image); err != nil {
			result = errors.Join(result, fmt.Errorf("%w: %v", ErrRollback, err))
		}
	}
	if err := failed.Save(a.statePath()); err != nil {
		result = errors.Join(result, err)
	}
	return failed, result
}

func (a Applier) waitHealthy() bool {
	deadline := a.now().Add(a.wait())
	for {
		if a.Engine.Healthy() {
			return true
		}
		if !a.now().Before(deadline) {
			return false
		}
		a.sleep(a.every())
	}
}

func (a Applier) backup(version string) error {
	dir := filepath.Join(a.Dir, DirBackups)
	if err := os.MkdirAll(dir, backupDirMode); err != nil {
		return err
	}
	if version == "" {
		version = noVersion
	}
	path := filepath.Join(dir, a.now().Format(backupTimeLayout)+backupNameSep+version+backupSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, stateFileMode)
	if err != nil {
		return err
	}
	if err := a.Engine.Backup(f); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return a.prune(dir)
}

// prune keeps the newest dumps; names start with a timestamp, so name order is age order.
func (a Applier) prune(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var dumps []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), backupSuffix) {
			dumps = append(dumps, e.Name())
		}
	}
	sort.Strings(dumps)
	for len(dumps) > a.keep() {
		if err := os.Remove(filepath.Join(dir, dumps[0])); err != nil {
			return err
		}
		dumps = dumps[1:]
	}
	return nil
}

func (a Applier) statePath() string { return filepath.Join(a.Dir, FileState) }

func (a Applier) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a Applier) sleep(d time.Duration) {
	if a.Sleep != nil {
		a.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (a Applier) wait() time.Duration {
	if a.Wait > 0 {
		return a.Wait
	}
	return defaultWait
}

func (a Applier) every() time.Duration {
	if a.Every > 0 {
		return a.Every
	}
	return defaultEvery
}

func (a Applier) keep() int {
	if a.Keep > 0 {
		return a.Keep
	}
	return defaultKeep
}
