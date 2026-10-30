package tests

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"webtyp.com/crypto/asym"
	"webtyp.com/selfupdate"
	"webtyp.com/update"
)

func defaultSupervisor(t *testing.T, eng selfupdate.Engine) (*selfupdate.Supervisor, *clock) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	selfupdate.State{Version: "v1.1.0", Image: "ghcr.io/acme/shop@sha256:0000000000000000000000000000000000000000000000000000000000000000"}.Save(statePath)

	pub, priv, _ := asym.GenerateKeyPair()
	m := update.Manifest{Version: "v1.2.0", Image: testImage, Digest: testDigest}
	text, sig, _ := update.SignManifest(m, priv)

	rel := fakeReleases{
		tag: "v1.2.0",
		assets: map[string][]byte{
			update.ManifestAsset:  text,
			update.SignatureAsset: sig,
		},
	}
	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}

	clk := &clock{}
	applier := selfupdate.Applier{
		Engine: eng,
		Dir:    dir,
		Wait:   10 * time.Second,
		Every:  time.Second,
		Now:    clk.Now,
		Sleep:  clk.Sleep,
	}
	return selfupdate.NewSupervisor(checker, applier, statePath), clk
}

func TestSupervisor_StartsIdle(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	st := s.Status()
	if st.Phase != "idle" {
		t.Errorf("expected phase idle, got %s", st.Phase)
	}
	if st.Version != "v1.1.0" {
		t.Errorf("expected version v1.1.0, got %s", st.Version)
	}
	if st.Available != "" {
		t.Errorf("expected empty available, got %s", st.Available)
	}
}

func TestSupervisor_CheckFindsVersion(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	if err := s.Check(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	st := s.Status()
	if st.Phase != "available" {
		t.Errorf("expected phase available, got %s", st.Phase)
	}
	if st.Available != "v1.2.0" {
		t.Errorf("expected available v1.2.0, got %s", st.Available)
	}
}

func TestSupervisor_CheckNothingNewer(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	selfupdate.State{Version: "v1.1.0", Image: "ghcr.io/acme/shop@sha256:0000000000000000000000000000000000000000000000000000000000000000"}.Save(statePath)

	pub, _, _ := asym.GenerateKeyPair()
	rel := fakeReleases{
		tag:    "v1.1.0",
		assets: map[string][]byte{},
	}
	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}
	applier := selfupdate.Applier{}
	s := selfupdate.NewSupervisor(checker, applier, statePath)

	if err := s.Check(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	st := s.Status()
	if st.Phase != "idle" {
		t.Errorf("expected phase idle, got %s", st.Phase)
	}
}

func TestSupervisor_CheckError(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	selfupdate.State{Version: "v1.1.0"}.Save(statePath)
	fakeErr := errors.New("fake latest error")
	rel := fakeReleases{err: fakeErr}
	checker := selfupdate.Checker{Releases: rel}
	s := selfupdate.NewSupervisor(checker, selfupdate.Applier{}, statePath)

	err := s.Check()
	if !errors.Is(err, fakeErr) {
		t.Errorf("expected error %v, got %v", fakeErr, err)
	}
	st := s.Status()
	if st.Phase != "idle" {
		t.Errorf("expected phase idle, got %s", st.Phase)
	}
	if st.Message != fakeErr.Error() {
		t.Errorf("expected message %q, got %q", fakeErr.Error(), st.Message)
	}
}

func TestSupervisor_ApplyHealthy(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{healthy: true})
	s.Check()
	if err := s.Apply(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	st := s.Status()
	if st.Phase != "idle" {
		t.Errorf("expected phase idle, got %s", st.Phase)
	}
	if st.Version != "v1.2.0" {
		t.Errorf("expected version v1.2.0, got %s", st.Version)
	}
	if st.Available != "" {
		t.Errorf("expected empty available, got %s", st.Available)
	}
}

func TestSupervisor_ApplyUnhealthy(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{healthy: false})
	s.Check()
	err := s.Apply()
	if !errors.Is(err, selfupdate.ErrUnhealthy) {
		t.Fatalf("expected ErrUnhealthy, got %v", err)
	}
	st := s.Status()
	if st.Phase != "rolled_back" {
		t.Errorf("expected phase rolled_back, got %s", st.Phase)
	}
	if st.Version != "v1.1.0" {
		t.Errorf("expected version v1.1.0, got %s", st.Version)
	}
	if st.Message == "" {
		t.Errorf("expected message to be set, got empty")
	}
}

func TestSupervisor_ApplyNothing(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	err := s.Apply()
	if !errors.Is(err, selfupdate.ErrNothingToApply) {
		t.Fatalf("expected ErrNothingToApply, got %v", err)
	}
}
