package tests

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtyp.com/crypto/asym"
	"webtyp.com/selfupdate"
)

func TestOpenSupervisor_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := selfupdate.OpenSupervisor(dir, "bad", nil, nil); !errors.Is(err, selfupdate.ErrAppRepo) {
		t.Fatalf("got %v, want ErrAppRepo", err)
	}
	if _, _, err := selfupdate.OpenSupervisor(dir, "acme/shop", nil, nil); !errors.Is(err, selfupdate.ErrPublicKey) {
		t.Fatalf("got %v, want ErrPublicKey", err)
	}
	pub, _, _ := asym.GenerateKeyPair()
	os.WriteFile(filepath.Join(dir, selfupdate.FilePublicKey), pub, 0o600)
	if _, _, err := selfupdate.OpenSupervisor(dir, "acme/shop", nil, nil); !errors.Is(err, selfupdate.ErrNoGitHubToken) {
		t.Fatalf("got %v, want ErrNoGitHubToken", err)
	}
	secretsDir := filepath.Join(dir, selfupdate.DirSecrets)
	os.MkdirAll(secretsDir, 0o700)
	os.WriteFile(filepath.Join(secretsDir, selfupdate.SecretGitHubToken), []byte("gh"), 0o600)
	if _, _, err := selfupdate.OpenSupervisor(dir, "acme/shop", nil, nil); !errors.Is(err, selfupdate.ErrNoSupervisorToken) {
		t.Fatalf("got %v, want ErrNoSupervisorToken", err)
	}
}

func TestOpenSupervisor_AndBackupLoop(t *testing.T) {
	pub, priv, _ := asym.GenerateKeyPair()
	rel := buildInstallRelease(t, pub, priv)
	root := t.TempDir()
	eng := &fakeEngine{healthy: true}

	cfg := selfupdate.InstallConfig{
		Repo:        "acme/shop",
		Root:        root,
		PublicKey:   base64.StdEncoding.EncodeToString(pub),
		GitHubToken: "gh-secret",
		TunnelToken: "tun-secret",
		DNSToken:    "dns-secret",
		Releases:    rel,
		Engine:      func(string) selfupdate.Engine { return eng },
		Up:          func(string, ...string) error { return nil },
		Chown:       func(string, int, int) error { return nil },
		Wait:        5 * time.Second,
		Every:       time.Second,
		Sleep:       func(time.Duration) {},
	}
	if _, err := selfupdate.Install(cfg); err != nil {
		t.Fatal(err)
	}

	appDir := filepath.Join(root, "acme-shop")
	sup, token, err := selfupdate.OpenSupervisor(appDir, "acme/shop", rel, eng)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go selfupdate.RunBackupLoop(ctx, sup, 5*time.Millisecond)

	backupsDir := filepath.Join(appDir, selfupdate.DirBackups)
	deadline := time.Now().Add(2 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(backupsDir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".sql") {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if !found {
		t.Fatal("expected RunBackupLoop to write a .sql dump into backups/")
	}

	req := httptest.NewRequest(http.MethodGet, selfupdate.PathStatus, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	sup.Handler(token).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "v1.0.0") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
