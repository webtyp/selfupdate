package tests

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtyp.com/crypto/asym"
	"webtyp.com/selfupdate"
	"webtyp.com/update"
)

func buildInstallRelease(t *testing.T, pub, priv []byte) fakeReleases {
	t.Helper()
	compose, err := selfupdate.ComposeFile(selfupdate.App{
		Name:   "acme-shop",
		Domain: "app.example.com",
		Repo:   "acme/shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := update.Manifest{
		Version:       "v1.0.0",
		Image:         testImage,
		Digest:        testDigest,
		ComposeDigest: update.Digest(compose),
	}
	text, sig, err := update.SignManifest(m, priv)
	if err != nil {
		t.Fatal(err)
	}
	return fakeReleases{
		tag: "v1.0.0",
		assets: map[string][]byte{
			update.ManifestAsset:  text,
			update.SignatureAsset: sig,
			update.ComposeAsset:   compose,
		},
	}
}

func TestDecodePublicKey_RawAndBase64(t *testing.T) {
	pub, _, err := asym.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	gotRaw, err := selfupdate.DecodePublicKey(pub)
	if err != nil || string(gotRaw) != string(pub) {
		t.Fatalf("DecodePublicKey(raw) err = %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(pub) + "\n"
	gotB64, err := selfupdate.DecodePublicKey([]byte(b64))
	if err != nil || string(gotB64) != string(pub) {
		t.Fatalf("DecodePublicKey(b64) err = %v", err)
	}
	if _, err := selfupdate.DecodePublicKey([]byte("not-a-key")); !errors.Is(err, selfupdate.ErrPublicKey) {
		t.Fatalf("expected ErrPublicKey, got %v", err)
	}
}

func TestInstall_ValidationErrors(t *testing.T) {
	pub, _, _ := asym.GenerateKeyPair()
	b64 := base64.StdEncoding.EncodeToString(pub)

	cases := []struct {
		cfg  selfupdate.InstallConfig
		want error
	}{
		{selfupdate.InstallConfig{Repo: "bad"}, selfupdate.ErrAppRepo},
		{selfupdate.InstallConfig{Repo: "acme/shop", PublicKey: "bad"}, selfupdate.ErrPublicKey},
		{selfupdate.InstallConfig{Repo: "acme/shop", PublicKey: b64}, selfupdate.ErrNoGitHubToken},
		{selfupdate.InstallConfig{Repo: "acme/shop", PublicKey: b64, GitHubToken: "gh"}, selfupdate.ErrNoTunnelToken},
		{selfupdate.InstallConfig{Repo: "acme/shop", PublicKey: b64, GitHubToken: "gh", TunnelToken: "tun"}, selfupdate.ErrNoDNSToken},
	}
	for _, tc := range cases {
		_, err := selfupdate.Install(tc.cfg)
		if !errors.Is(err, tc.want) {
			t.Errorf("Install(%+v) = %v, want %v", tc.cfg, err, tc.want)
		}
	}
}

func TestInstall_ProvisionsAndStarts(t *testing.T) {
	pub, priv, err := asym.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	rel := buildInstallRelease(t, pub, priv)
	root := t.TempDir()
	eng := &fakeEngine{healthy: true}
	var upCalls [][]string
	uidByFile := make(map[string]int)

	cfg := selfupdate.InstallConfig{
		Repo:        "acme/shop",
		Root:        root,
		PublicKey:   base64.StdEncoding.EncodeToString(pub),
		GitHubToken: "gh-secret",
		TunnelToken: "tun-secret",
		DNSToken:    "dns-secret",
		Releases:    rel,
		Engine:      func(string) selfupdate.Engine { return eng },
		Up: func(_ string, services ...string) error {
			upCalls = append(upCalls, services)
			return nil
		},
		Chown: func(path string, uid, _ int) error {
			name := strings.TrimSuffix(filepath.Base(path), ".tmp")
			uidByFile[name] = uid
			return nil
		},
		Wait:  10 * time.Second,
		Every: time.Second,
		Sleep: func(time.Duration) {},
	}

	st, err := selfupdate.Install(cfg)
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if st.Version != "v1.0.0" {
		t.Fatalf("Version = %q, want v1.0.0", st.Version)
	}
	if len(upCalls) != 2 || len(upCalls[0]) != 1 || upCalls[0][0] != selfupdate.ServiceDB || len(upCalls[1]) != 0 {
		t.Fatalf("unexpected Up calls: %+v", upCalls)
	}

	appDir := filepath.Join(root, "acme-shop")
	secretsDir := filepath.Join(appDir, selfupdate.DirSecrets)
	wantUID := map[string]int{
		selfupdate.SecretDBOwner:          0,
		selfupdate.SecretDBApp:            0,
		selfupdate.FileInitDB:             selfupdate.PostgresUID,
		selfupdate.SecretDatabaseURL:      selfupdate.NonRootUID,
		selfupdate.SecretDatabaseOwnerURL: selfupdate.NonRootUID,
		selfupdate.SecretDNSToken:         selfupdate.NonRootUID,
		selfupdate.SecretSupervisorToken:  selfupdate.NonRootUID,
		selfupdate.SecretTunnelToken:      selfupdate.NonRootUID,
		selfupdate.SecretGitHubToken:      0,
	}
	for name, want := range wantUID {
		info, err := os.Stat(filepath.Join(secretsDir, name))
		if err != nil {
			t.Fatalf("missing secret %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o400 {
			t.Errorf("%s mode = %o, want 0400", name, info.Mode().Perm())
		}
		if uidByFile[name] != want {
			t.Errorf("%s uid = %d, want %d", name, uidByFile[name], want)
		}
	}

	sqlBytes, err := os.ReadFile(filepath.Join(secretsDir, selfupdate.FileInitDB))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(sqlBytes)
	if !strings.Contains(sql, "CREATE ROLE acme_shop_app") || !strings.Contains(sql, "ALTER DEFAULT PRIVILEGES FOR ROLE acme_shop_owner") {
		t.Fatalf("unexpected initdb.sql: %s", sql)
	}

	envBytes, err := os.ReadFile(filepath.Join(appDir, selfupdate.FileEnv))
	if err != nil {
		t.Fatal(err)
	}
	envText := string(envBytes)
	for _, wantLine := range []string{
		"APP_IMAGE=" + testImage + "@" + testDigest,
		"APP_DOMAIN=app.example.com",
		"SUPERVISOR_IMAGE=" + selfupdate.DefaultSupervisorImage,
		"LAN_ADDRESS=127.0.0.1",
	} {
		if !strings.Contains(envText, wantLine) {
			t.Errorf(".env missing %q in:\n%s", wantLine, envText)
		}
	}

	owner1, _ := os.ReadFile(filepath.Join(secretsDir, selfupdate.SecretDBOwner))
	sup1, _ := os.ReadFile(filepath.Join(secretsDir, selfupdate.SecretSupervisorToken))
	if _, err := selfupdate.Install(cfg); err != nil {
		t.Fatalf("second Install failed: %v", err)
	}
	owner2, _ := os.ReadFile(filepath.Join(secretsDir, selfupdate.SecretDBOwner))
	sup2, _ := os.ReadFile(filepath.Join(secretsDir, selfupdate.SecretSupervisorToken))
	if string(owner1) != string(owner2) || string(sup1) != string(sup2) {
		t.Fatal("expected second Install to preserve generated database and supervisor secrets")
	}
}

func TestInstall_Unhealthy(t *testing.T) {
	pub, priv, _ := asym.GenerateKeyPair()
	rel := buildInstallRelease(t, pub, priv)
	root := t.TempDir()
	clk := &clock{}
	eng := &fakeEngine{healthy: false}

	cfg := selfupdate.InstallConfig{
		Repo:        "acme/shop",
		Root:        root,
		PublicKey:   base64.StdEncoding.EncodeToString(pub),
		GitHubToken: "gh",
		TunnelToken: "tun",
		DNSToken:    "dns",
		Releases:    rel,
		Engine:      func(string) selfupdate.Engine { return eng },
		Up:          func(string, ...string) error { return nil },
		Chown:       func(string, int, int) error { return nil },
		Wait:        6 * time.Second,
		Every:       3 * time.Second,
		Now:         clk.Now,
		Sleep:       clk.Sleep,
	}
	_, err := selfupdate.Install(cfg)
	if !errors.Is(err, selfupdate.ErrUnhealthy) {
		t.Fatalf("got %v, want ErrUnhealthy", err)
	}
	if _, err := os.Stat(filepath.Join(root, "acme-shop", selfupdate.FileState)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no state.json on unhealthy install, got %v", err)
	}
}
