package selfupdate

import (
	"errors"
	"fmt"

	"webtyp.com/update"
)

var (
	// ErrNoAsset: the Release lacks manifest.txt or manifest.sig.
	ErrNoAsset = errors.New("selfupdate: release has no asset")
	// ErrVersionMismatch: the manifest was signed for another version than the Release tag.
	ErrVersionMismatch = errors.New("selfupdate: manifest version differs from the release tag")
)

// Update is a verified version ready to apply: its signed manifest and the
// compose file whose digest the manifest carries.
type Update struct {
	Manifest update.Manifest
	Compose  []byte
}

// Checker finds a newer version whose manifest the publisher signed.
// Example: m, ok, err := selfupdate.Checker{Releases: gh, PublicKey: pub}.Check(state)
type Checker struct {
	Releases  Releases
	PublicKey []byte // the publisher's key, from update.pub
}

// Check returns the latest version when it is newer than the running one, was
// never marked as failed, and its manifest and compose file verify; ok is false when there is
// nothing to install. With no running version (first install) any release is newer.
func (c Checker) Check(s State) (u Update, ok bool, err error) {
	r, err := c.Releases.Latest()
	if err != nil {
		return Update{}, false, err
	}
	newer := s.Version == "" || update.IsOutdated(s.Version, r.Tag)
	if !newer || s.HasFailed(r.Tag) {
		return Update{}, false, nil
	}
	text, err := c.asset(r, update.ManifestAsset)
	if err != nil {
		return Update{}, false, err
	}
	sig, err := c.asset(r, update.SignatureAsset)
	if err != nil {
		return Update{}, false, err
	}
	m, err := update.VerifyManifest(text, sig, c.PublicKey)
	if err != nil {
		return Update{}, false, err
	}
	if m.Version != r.Tag {
		return Update{}, false, fmt.Errorf("%w: manifest %s, tag %s", ErrVersionMismatch, m.Version, r.Tag)
	}
	compose, err := c.asset(r, update.ComposeAsset)
	if err != nil {
		return Update{}, false, err
	}
	if err := m.CheckCompose(compose); err != nil {
		return Update{}, false, err
	}
	return Update{Manifest: m, Compose: compose}, true, nil
}

func (c Checker) asset(r Release, name string) ([]byte, error) {
	for _, a := range r.Assets {
		if a.Name == name {
			return c.Releases.Download(a)
		}
	}
	return nil, fmt.Errorf("%w %s: %s", ErrNoAsset, r.Tag, name)
}
