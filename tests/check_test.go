package tests

import (
	"errors"
	"strings"
	"testing"

	"webtyp.com/crypto/asym"
	"webtyp.com/selfupdate"
	"webtyp.com/update"
)

var (
	testImage   = "ghcr.io/acme/shop"
	testDigest  = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	testCompose = []byte("name: shop\n")
)

type fakeReleases struct {
	tag     string
	assets  map[string][]byte
	err     error
	noAsset bool
}

func (f fakeReleases) Latest() (selfupdate.Release, error) {
	if f.err != nil {
		return selfupdate.Release{}, f.err
	}
	r := selfupdate.Release{Tag: f.tag}
	if !f.noAsset {
		for name := range f.assets {
			r.Assets = append(r.Assets, selfupdate.Asset{Name: name, URL: "url"})
		}
	}
	return r, nil
}

func (f fakeReleases) Download(a selfupdate.Asset) ([]byte, error) {
	return f.assets[a.Name], nil
}

func TestCheck_Available(t *testing.T) {
	pub, priv, _ := asym.GenerateKeyPair()
	m := update.Manifest{Version: "v1.2.0", Image: testImage, Digest: testDigest, ComposeDigest: update.Digest(testCompose)}
	text, sig, _ := update.SignManifest(m, priv)

	rel := fakeReleases{
		tag: "v1.2.0",
		assets: map[string][]byte{
			update.ManifestAsset:  text,
			update.SignatureAsset: sig,
			update.ComposeAsset:   testCompose,
		},
	}

	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}
	state := selfupdate.State{Version: "v1.1.0"}

	out, ok, err := checker.Check(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if out.Manifest.Version != "v1.2.0" {
		t.Errorf("expected version v1.2.0, got %q", out.Manifest.Version)
	}
	if out.Manifest.Reference() != testImage+"@"+testDigest {
		t.Errorf("expected reference %q, got %q", testImage+"@"+testDigest, out.Manifest.Reference())
	}
	if string(out.Compose) != "name: shop\n" {
		t.Errorf("expected compose data %q, got %q", "name: shop\n", string(out.Compose))
	}
}

func TestCheck_ComposeMissing(t *testing.T) {
	pub, priv, _ := asym.GenerateKeyPair()
	m := update.Manifest{Version: "v1.2.0", Image: testImage, Digest: testDigest, ComposeDigest: update.Digest(testCompose)}
	text, sig, _ := update.SignManifest(m, priv)

	rel := fakeReleases{
		tag: "v1.2.0",
		assets: map[string][]byte{
			update.ManifestAsset:  text,
			update.SignatureAsset: sig,
		},
	}
	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}

	_, _, err := checker.Check(selfupdate.State{Version: "v1.1.0"})
	if !errors.Is(err, selfupdate.ErrNoAsset) {
		t.Errorf("expected ErrNoAsset, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "compose.yaml") {
		t.Errorf("expected error to name compose.yaml, got %v", err)
	}
}

func TestCheck_ComposeAltered(t *testing.T) {
	pub, priv, _ := asym.GenerateKeyPair()
	m := update.Manifest{Version: "v1.2.0", Image: testImage, Digest: testDigest, ComposeDigest: update.Digest(testCompose)}
	text, sig, _ := update.SignManifest(m, priv)

	rel := fakeReleases{
		tag: "v1.2.0",
		assets: map[string][]byte{
			update.ManifestAsset:  text,
			update.SignatureAsset: sig,
			update.ComposeAsset:   []byte("name: evil\n"),
		},
	}
	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}

	_, _, err := checker.Check(selfupdate.State{Version: "v1.1.0"})
	if err == nil || err.Error() != update.ErrComposeMismatch {
		t.Errorf("expected ErrComposeMismatch, got %v", err)
	}
}

func TestCheck_FirstInstall(t *testing.T) {
	pub, priv, _ := asym.GenerateKeyPair()
	m := update.Manifest{Version: "v1.2.0", Image: testImage, Digest: testDigest, ComposeDigest: update.Digest(testCompose)}
	text, sig, _ := update.SignManifest(m, priv)

	rel := fakeReleases{
		tag: "v1.2.0",
		assets: map[string][]byte{
			update.ManifestAsset:  text,
			update.SignatureAsset: sig,
			update.ComposeAsset:   testCompose,
		},
	}

	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}
	state := selfupdate.State{}

	_, ok, err := checker.Check(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
}

func TestCheck_NotNewer(t *testing.T) {
	pub, _, _ := asym.GenerateKeyPair()
	rel := fakeReleases{
		tag:    "v1.1.0",
		assets: map[string][]byte{},
	}
	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}
	state := selfupdate.State{Version: "v1.1.0"}

	_, ok, err := checker.Check(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false")
	}
}

func TestCheck_Failed(t *testing.T) {
	pub, _, _ := asym.GenerateKeyPair()
	rel := fakeReleases{
		tag:    "v1.2.0",
		assets: map[string][]byte{},
	}
	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}
	state := selfupdate.State{Version: "v1.1.0", Failed: []string{"v1.2.0"}}

	_, ok, err := checker.Check(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false")
	}
}

func TestCheck_LatestFails(t *testing.T) {
	fakeErr := errors.New("latest failed")
	rel := fakeReleases{err: fakeErr}
	checker := selfupdate.Checker{Releases: rel}

	_, _, err := checker.Check(selfupdate.State{Version: "v1.1.0"})
	if !errors.Is(err, fakeErr) {
		t.Errorf("expected %v, got %v", fakeErr, err)
	}
}

func TestCheck_NoSignature(t *testing.T) {
	pub, priv, _ := asym.GenerateKeyPair()
	m := update.Manifest{Version: "v1.2.0", Image: testImage, Digest: testDigest, ComposeDigest: update.Digest(testCompose)}
	text, _, _ := update.SignManifest(m, priv)

	rel := fakeReleases{
		tag: "v1.2.0",
		assets: map[string][]byte{
			update.ManifestAsset: text,
		},
	}
	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}

	_, _, err := checker.Check(selfupdate.State{Version: "v1.1.0"})
	if !errors.Is(err, selfupdate.ErrNoAsset) {
		t.Errorf("expected ErrNoAsset, got %v", err)
	}
}

func TestCheck_WrongKey(t *testing.T) {
	pub, _, _ := asym.GenerateKeyPair()
	_, priv2, _ := asym.GenerateKeyPair()

	m := update.Manifest{Version: "v1.2.0", Image: testImage, Digest: testDigest, ComposeDigest: update.Digest(testCompose)}
	text, sig, _ := update.SignManifest(m, priv2)

	rel := fakeReleases{
		tag: "v1.2.0",
		assets: map[string][]byte{
			update.ManifestAsset:  text,
			update.SignatureAsset: sig,
			update.ComposeAsset:   testCompose,
		},
	}
	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}

	_, _, err := checker.Check(selfupdate.State{Version: "v1.1.0"})
	if err == nil || err.Error() != update.ErrManifestSignature {
		t.Errorf("expected %v, got %v", update.ErrManifestSignature, err)
	}
}

func TestCheck_VersionMismatch(t *testing.T) {
	pub, priv, _ := asym.GenerateKeyPair()
	m := update.Manifest{Version: "v1.0.9", Image: testImage, Digest: testDigest, ComposeDigest: update.Digest(testCompose)}
	text, sig, _ := update.SignManifest(m, priv)

	rel := fakeReleases{
		tag: "v1.2.0",
		assets: map[string][]byte{
			update.ManifestAsset:  text,
			update.SignatureAsset: sig,
			update.ComposeAsset:   testCompose,
		},
	}
	checker := selfupdate.Checker{Releases: rel, PublicKey: pub}

	_, _, err := checker.Check(selfupdate.State{Version: "v1.0.0"})
	if !errors.Is(err, selfupdate.ErrVersionMismatch) {
		t.Errorf("expected ErrVersionMismatch, got %v", err)
	}
}
