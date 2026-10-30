package selfupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ErrStateCorrupt: the state file exists but cannot be read.
var ErrStateCorrupt = errors.New("selfupdate: state file is not valid JSON")

const (
	stateIndent   = "  "
	stateFileMode = 0o600
	tmpSuffix     = ".tmp"
)

// State is what the supervisor remembers between restarts.
// Example: s, err := selfupdate.LoadState(selfupdate.AppDir("shop") + "/" + selfupdate.FileState)
type State struct {
	Version         string   `json:"version"`          // running version, "" before the first install
	Image           string   `json:"image"`            // running image@digest
	PreviousVersion string   `json:"previous_version"` // what a rollback returns to
	PreviousImage   string   `json:"previous_image"`
	Failed          []string `json:"failed"` // versions whose health check failed: never retried
}

// LoadState reads the state; a missing file is the zero State (nothing installed yet).
func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("%w: %s: %v", ErrStateCorrupt, path, err)
	}
	return s, nil
}

// Save writes the state atomically (temporary file, then rename), readable only by its owner.
func (s State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", stateIndent)
	if err != nil {
		return err
	}
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, data, stateFileMode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// HasFailed reports whether version already failed its health check.
func (s State) HasFailed(version string) bool {
	for _, v := range s.Failed {
		if v == version {
			return true
		}
	}
	return false
}
