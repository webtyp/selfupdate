package tests

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"webtyp.com/update"

	"webtyp.com/selfupdate"
)

type fakeEngine struct {
	calls        []string
	backupErr    error
	pullErr      error
	switchErrFor string
	switchErr    error
	healthy      bool
}

func (e *fakeEngine) Backup(w io.Writer) error {
	e.calls = append(e.calls, "backup")
	if e.backupErr != nil {
		return e.backupErr
	}
	_, err := w.Write([]byte("dump"))
	return err
}

func (e *fakeEngine) Pull(ref string) error {
	e.calls = append(e.calls, "pull "+ref)
	return e.pullErr
}

func (e *fakeEngine) Switch(ref string) error {
	e.calls = append(e.calls, "switch "+ref)
	if e.switchErrFor == ref {
		return e.switchErr
	}
	return nil
}

func (e *fakeEngine) Healthy() bool {
	e.calls = append(e.calls, "healthy")
	return e.healthy
}

func defaultManifest() update.Manifest {
	return update.Manifest{
		Version: "v1.2.0",
		Image:   "ghcr.io/acme/shop",
		Digest:  "sha256:1111111111111111111111111111111111111111111111111111111111111111",
	}
}

func defaultState() selfupdate.State {
	return selfupdate.State{
		Version: "v1.1.0",
		Image:   "ghcr.io/acme/shop@sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}
}

type clock struct {
	now time.Time
}

func (c *clock) Now() time.Time {
	if c.now.IsZero() {
		c.now = time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	}
	return c.now
}

func (c *clock) Sleep(d time.Duration) {
	c.now = c.Now().Add(d)
}

func TestApply_Healthy(t *testing.T) {
	dir := t.TempDir()
	eng := &fakeEngine{healthy: true}
	clk := &clock{}
	applier := selfupdate.Applier{
		Engine: eng,
		Dir:    dir,
		Wait:   10 * time.Second,
		Every:  time.Second,
		Now:    clk.Now,
		Sleep:  clk.Sleep,
	}
	m := defaultManifest()
	s := defaultState()
	next, err := applier.Apply(s, m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newRef := m.Reference()
	expectedCalls := []string{"backup", "pull " + newRef, "switch " + newRef, "healthy"}
	if !reflect.DeepEqual(eng.calls, expectedCalls) {
		t.Errorf("expected calls %v, got %v", expectedCalls, eng.calls)
	}

	expectedNext := selfupdate.State{
		Version:         "v1.2.0",
		Image:           newRef,
		PreviousVersion: "v1.1.0",
		PreviousImage:   s.Image,
	}
	if !reflect.DeepEqual(next, expectedNext) {
		t.Errorf("expected state %v, got %v", expectedNext, next)
	}

	saved, err := selfupdate.LoadState(filepath.Join(dir, selfupdate.FileState))
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if !reflect.DeepEqual(saved, expectedNext) {
		t.Errorf("expected saved state %v, got %v", expectedNext, saved)
	}

	backups, _ := os.ReadDir(filepath.Join(dir, selfupdate.DirBackups))
	if len(backups) != 1 || !strings.HasSuffix(backups[0].Name(), ".sql") {
		t.Fatalf("expected one .sql backup, got %v", backups)
	}
	content, _ := os.ReadFile(filepath.Join(dir, selfupdate.DirBackups, backups[0].Name()))
	if string(content) != "dump" {
		t.Errorf("expected backup content 'dump', got %q", content)
	}
}

func TestApply_BackupFails(t *testing.T) {
	dir := t.TempDir()
	expectedErr := errors.New("backup failed")
	eng := &fakeEngine{backupErr: expectedErr}
	applier := selfupdate.Applier{Engine: eng, Dir: dir}
	next, err := applier.Apply(defaultState(), defaultManifest())
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if len(eng.calls) != 1 || eng.calls[0] != "backup" {
		t.Errorf("expected calls [backup], got %v", eng.calls)
	}
	backups, _ := os.ReadDir(filepath.Join(dir, selfupdate.DirBackups))
	if len(backups) != 0 {
		t.Errorf("expected no backups, got %v", backups)
	}
	if _, err := os.Stat(filepath.Join(dir, selfupdate.FileState)); !errors.Is(err, os.ErrNotExist) {
		t.Error("expected no state.json")
	}
	if !reflect.DeepEqual(next, defaultState()) {
		t.Errorf("expected unchanged state, got %v", next)
	}
}

func TestApply_PullFails(t *testing.T) {
	dir := t.TempDir()
	expectedErr := errors.New("pull failed")
	eng := &fakeEngine{pullErr: expectedErr}
	applier := selfupdate.Applier{Engine: eng, Dir: dir}
	next, err := applier.Apply(defaultState(), defaultManifest())
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	for _, call := range eng.calls {
		if strings.HasPrefix(call, "switch") {
			t.Errorf("unexpected switch call: %v", eng.calls)
		}
	}
	if !reflect.DeepEqual(next, defaultState()) {
		t.Errorf("expected unchanged state, got %v", next)
	}
}

func TestApply_Unhealthy(t *testing.T) {
	dir := t.TempDir()
	eng := &fakeEngine{healthy: false}
	clk := &clock{}
	applier := selfupdate.Applier{
		Engine: eng,
		Dir:    dir,
		Wait:   10 * time.Second,
		Every:  time.Second,
		Now:    clk.Now,
		Sleep:  clk.Sleep,
	}
	m := defaultManifest()
	s := defaultState()
	next, err := applier.Apply(s, m)
	if !errors.Is(err, selfupdate.ErrUnhealthy) {
		t.Errorf("expected ErrUnhealthy, got %v", err)
	}

	if len(eng.calls) < 3 || eng.calls[len(eng.calls)-1] != "switch "+s.Image {
		t.Errorf("expected switch <old> at the end, got calls: %v", eng.calls)
	}

	expectedNext := s
	expectedNext.Failed = []string{"v1.2.0"}

	if !reflect.DeepEqual(next, expectedNext) {
		t.Errorf("expected state %v, got %v", expectedNext, next)
	}

	saved, _ := selfupdate.LoadState(filepath.Join(dir, selfupdate.FileState))
	if !reflect.DeepEqual(saved, expectedNext) {
		t.Errorf("expected saved state %v, got %v", expectedNext, saved)
	}
}

func TestApply_SwitchFailsCountsAsUnhealthy(t *testing.T) {
	dir := t.TempDir()
	m := defaultManifest()
	s := defaultState()
	switchErr := errors.New("switch failed")
	eng := &fakeEngine{switchErrFor: m.Reference(), switchErr: switchErr}
	clk := &clock{}
	applier := selfupdate.Applier{
		Engine: eng,
		Dir:    dir,
		Wait:   10 * time.Second,
		Every:  time.Second,
		Now:    clk.Now,
		Sleep:  clk.Sleep,
	}
	_, err := applier.Apply(s, m)
	if !errors.Is(err, selfupdate.ErrUnhealthy) {
		t.Errorf("expected ErrUnhealthy, got %v", err)
	}
	if !errors.Is(err, switchErr) {
		t.Errorf("expected switchErr, got %v", err)
	}
	if eng.calls[len(eng.calls)-1] != "switch "+s.Image {
		t.Errorf("expected rollback to old image, got %v", eng.calls)
	}
}

func TestApply_RollbackFails(t *testing.T) {
	dir := t.TempDir()
	m := defaultManifest()
	s := defaultState()
	eng := &fakeEngine{healthy: false, switchErrFor: s.Image, switchErr: errors.New("rollback failed")}
	clk := &clock{}
	applier := selfupdate.Applier{
		Engine: eng,
		Dir:    dir,
		Wait:   10 * time.Second,
		Every:  time.Second,
		Now:    clk.Now,
		Sleep:  clk.Sleep,
	}
	_, err := applier.Apply(s, m)
	if !errors.Is(err, selfupdate.ErrUnhealthy) {
		t.Errorf("expected ErrUnhealthy, got %v", err)
	}
	if !errors.Is(err, selfupdate.ErrRollback) {
		t.Errorf("expected ErrRollback, got %v", err)
	}
}

func TestApply_FirstInstallUnhealthy(t *testing.T) {
	dir := t.TempDir()
	m := defaultManifest()
	s := selfupdate.State{}
	eng := &fakeEngine{healthy: false}
	clk := &clock{}
	applier := selfupdate.Applier{
		Engine: eng,
		Dir:    dir,
		Wait:   10 * time.Second,
		Every:  time.Second,
		Now:    clk.Now,
		Sleep:  clk.Sleep,
	}
	next, err := applier.Apply(s, m)
	if !errors.Is(err, selfupdate.ErrUnhealthy) {
		t.Errorf("expected ErrUnhealthy, got %v", err)
	}

	switchCount := 0
	for _, call := range eng.calls {
		if strings.HasPrefix(call, "switch") {
			switchCount++
		}
	}
	if switchCount > 1 {
		t.Errorf("expected no second switch, got calls: %v", eng.calls)
	}

	expectedNext := selfupdate.State{Failed: []string{"v1.2.0"}}
	if !reflect.DeepEqual(next, expectedNext) {
		t.Errorf("expected state %v, got %v", expectedNext, next)
	}
}

func TestApply_KeepsLastBackups(t *testing.T) {
	dir := t.TempDir()
	eng := &fakeEngine{healthy: true}
	clk := &clock{}
	applier := selfupdate.Applier{
		Engine: eng,
		Dir:    dir,
		Keep:   2,
		Now:    clk.Now,
		Sleep:  clk.Sleep,
	}
	m := defaultManifest()
	s := defaultState()

	_, _ = applier.Apply(s, m)
	clk.Sleep(time.Hour)
	_, _ = applier.Apply(s, m)
	clk.Sleep(time.Hour)
	_, _ = applier.Apply(s, m)

	backups, _ := os.ReadDir(filepath.Join(dir, selfupdate.DirBackups))
	if len(backups) != 2 {
		t.Errorf("expected 2 backups, got %d", len(backups))
	}
}
