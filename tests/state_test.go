package tests

import (
	"errors"
	"os"
	"testing"

	"webtyp.com/selfupdate"
)

func TestState_MissingFile(t *testing.T) {
	dir := t.TempDir()
	s, err := selfupdate.LoadState(dir + "/state.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Version != "" || len(s.Failed) != 0 {
		t.Errorf("expected zero State, got %+v", s)
	}
}

func TestState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/state.json"
	s1 := selfupdate.State{
		Version:         "v1.2.0",
		Image:           "img1",
		PreviousVersion: "v1.1.0",
		PreviousImage:   "img0",
		Failed:          []string{"v1.1.5"},
	}

	if err := s1.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600, got %v", info.Mode().Perm())
	}

	s2, err := selfupdate.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if s1.Version != s2.Version || s1.Image != s2.Image ||
		s1.PreviousVersion != s2.PreviousVersion || s1.PreviousImage != s2.PreviousImage ||
		len(s1.Failed) != len(s2.Failed) || s1.Failed[0] != s2.Failed[0] {
		t.Errorf("expected equal States, got %+v and %+v", s1, s2)
	}
}

func TestState_Corrupt(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/state.json"
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := selfupdate.LoadState(path)
	if !errors.Is(err, selfupdate.ErrStateCorrupt) {
		t.Errorf("expected ErrStateCorrupt, got %v", err)
	}
}

func TestState_HasFailed(t *testing.T) {
	s := selfupdate.State{Failed: []string{"v1.1.0"}}
	if !s.HasFailed("v1.1.0") {
		t.Errorf("expected true for v1.1.0")
	}
	if s.HasFailed("v1.2.0") {
		t.Errorf("expected false for v1.2.0")
	}
}
