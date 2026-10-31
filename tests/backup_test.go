package tests

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/selfupdate"
)

// backupSupervisor is a supervisor whose state says v1.1.0 is running, with its folder.
func backupSupervisor(t *testing.T, eng selfupdate.Engine) (*selfupdate.Supervisor, string) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, selfupdate.FileState)
	selfupdate.State{Version: "v1.1.0", Image: testImage + "@" + testDigest}.Save(statePath)
	clk := &clock{}
	a := selfupdate.Applier{Engine: eng, Dir: dir, Now: clk.Now, Sleep: clk.Sleep}
	return selfupdate.NewSupervisor(selfupdate.Checker{}, a, statePath), dir
}

// blockingEngine holds Backup until release is closed.
type blockingEngine struct {
	fakeEngine
	started, release chan struct{}
}

func (e *blockingEngine) Backup(w io.Writer) error {
	close(e.started)
	<-e.release
	return e.fakeEngine.Backup(w)
}

func TestSupervisor_BackupWritesDump(t *testing.T) {
	s, dir := backupSupervisor(t, &fakeEngine{})
	if err := s.Backup(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, selfupdate.DirBackups, "20230101-120000_v1.1.0.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "dump" {
		t.Errorf("expected dump, got %s", string(b))
	}
	if s.Status().Phase != selfupdate.PhaseIdle {
		t.Errorf("expected idle, got %s", s.Status().Phase)
	}
}

func TestSupervisor_BackupKeepsAvailable(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(); err != nil {
		t.Fatal(err)
	}
	if s.Status().Phase != selfupdate.PhaseAvailable {
		t.Errorf("expected available, got %s", s.Status().Phase)
	}
	if s.Status().Available != "v1.2.0" {
		t.Errorf("expected v1.2.0, got %s", s.Status().Available)
	}
}

func TestSupervisor_BackupFails(t *testing.T) {
	s, dir := backupSupervisor(t, &fakeEngine{backupErr: errors.New("pg_dump failed")})
	err := s.Backup()
	if err == nil || !strings.Contains(err.Error(), "pg_dump failed") {
		t.Errorf("expected pg_dump failed error, got %v", err)
	}
	if s.Status().Message != err.Error() {
		t.Errorf("expected %s, got %s", err.Error(), s.Status().Message)
	}
	if s.Status().Phase != selfupdate.PhaseIdle {
		t.Errorf("expected idle, got %s", s.Status().Phase)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, selfupdate.DirBackups))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			t.Errorf("expected no .sql files, found %s", e.Name())
		}
	}
}

func TestSupervisor_BackupNoState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, selfupdate.FileState)
	clk := &clock{}
	a := selfupdate.Applier{Engine: &fakeEngine{}, Dir: dir, Now: clk.Now, Sleep: clk.Sleep}
	s := selfupdate.NewSupervisor(selfupdate.Checker{}, a, statePath)

	if err := s.Backup(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, selfupdate.DirBackups, "20230101-120000_none.sql")); err != nil {
		t.Errorf("expected 20230101-120000_none.sql to exist: %v", err)
	}
}

func TestSupervisor_BackupPrunes(t *testing.T) {
	s, dir := backupSupervisor(t, &fakeEngine{})
	backupDir := filepath.Join(dir, selfupdate.DirBackups)
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"0", "1", "2", "3", "4", "5", "6"} {
		if err := os.WriteFile(filepath.Join(backupDir, "20000101-00000"+n+"_v1.0.0.sql"), []byte(""), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Backup(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	var sqlFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			sqlFiles = append(sqlFiles, e.Name())
		}
	}
	if len(sqlFiles) != 7 {
		t.Errorf("expected exactly 7 .sql files, got %d", len(sqlFiles))
	}
	for _, e := range sqlFiles {
		if e == "20000101-000000_v1.0.0.sql" {
			t.Errorf("expected 20000101-000000_v1.0.0.sql to be pruned")
		}
	}
	if _, err := os.Stat(filepath.Join(backupDir, "20230101-120000_v1.1.0.sql")); err != nil {
		t.Errorf("expected 20230101-120000_v1.1.0.sql to exist: %v", err)
	}
}

func TestSupervisor_BackupWhileBusy(t *testing.T) {
	eng := &blockingEngine{started: make(chan struct{}), release: make(chan struct{})}
	s, _ := backupSupervisor(t, eng)

	done := make(chan error)
	go func() { done <- s.Backup() }()

	<-eng.started

	if s.Status().Phase != selfupdate.PhaseBackingUp {
		t.Errorf("expected backing_up, got %s", s.Status().Phase)
	}
	if err := s.Check(); !errors.Is(err, selfupdate.ErrBusy) {
		t.Errorf("expected ErrBusy, got %v", err)
	}
	if err := s.Backup(); !errors.Is(err, selfupdate.ErrBusy) {
		t.Errorf("expected ErrBusy, got %v", err)
	}
	if err := s.Apply(); !errors.Is(err, selfupdate.ErrBusy) {
		t.Errorf("expected ErrBusy, got %v", err)
	}

	close(eng.release)
	if err := <-done; err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if s.Status().Phase != selfupdate.PhaseIdle {
		t.Errorf("expected idle, got %s", s.Status().Phase)
	}
}
