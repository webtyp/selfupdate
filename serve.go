package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupInterval is the time between daily database copies in serve mode.
const BackupInterval = 24 * time.Hour

// OpenSupervisor loads update.pub, secrets/github_token and secrets/supervisor_token from dir
// and returns the configured Supervisor and its bearer token.
// Example: sup, token, err := selfupdate.OpenSupervisor(selfupdate.AppDir("shop"), "acme/shop", nil, nil)
func OpenSupervisor(dir, repo string, rel Releases, eng Engine) (*Supervisor, string, error) {
	if !validRepo(repo) {
		return nil, "", ErrAppRepo
	}
	pubRaw, err := os.ReadFile(filepath.Join(dir, FilePublicKey))
	if err != nil {
		return nil, "", ErrPublicKey
	}
	pub, err := DecodePublicKey(pubRaw)
	if err != nil {
		return nil, "", err
	}
	ghBytes, err := os.ReadFile(filepath.Join(dir, DirSecrets, SecretGitHubToken))
	if err != nil || strings.TrimSpace(string(ghBytes)) == "" {
		return nil, "", ErrNoGitHubToken
	}
	supBytes, err := os.ReadFile(filepath.Join(dir, DirSecrets, SecretSupervisorToken))
	if err != nil || strings.TrimSpace(string(supBytes)) == "" {
		return nil, "", ErrNoSupervisorToken
	}
	ghToken := strings.TrimSpace(string(ghBytes))
	supToken := strings.TrimSpace(string(supBytes))

	if rel == nil {
		rel = GitHub{Repo: repo, Token: ghToken}
	}
	if eng == nil {
		eng = Compose{Dir: dir}
	}
	checker := Checker{Releases: rel, PublicKey: pub}
	applier := Applier{Engine: eng, Dir: dir}
	return NewSupervisor(checker, applier, filepath.Join(dir, FileState)), supToken, nil
}

// RunBackupLoop calls s.Backup() on every tick of every (BackupInterval when <= 0) until ctx is done.
// Example: go selfupdate.RunBackupLoop(ctx, sup, selfupdate.BackupInterval)
func RunBackupLoop(ctx context.Context, s *Supervisor, every time.Duration) {
	if every <= 0 {
		every = BackupInterval
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Backup()
		}
	}
}
