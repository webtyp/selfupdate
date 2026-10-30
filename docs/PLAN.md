---
PLAN: "feat: apply a verified version — backup, pull, switch, health wait, rollback"
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 5508372715093224670
---

> Read AGENTS.md at the repo root before any change. Design: `docs/ARCHITECTURE.md`; the flow this plan builds is
> `docs/diagrams/APPLY_FLOW.md`.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# Apply a version

## Context

The previous plan (v0.0.2) gave the supervisor its layout names, its state file and `Checker.Check`, which returns a
verified `update.Manifest`. This plan switches the app to that version: dump the database, pull `image@digest`, write
it as `APP_IMAGE` in the app's `.env`, recreate only the `app` service, wait for `/health`; if the app does not become
healthy, write the previous image back, recreate again and mark the version as failed (never retried). Docker is
reached through the `docker` CLI behind an interface, so every branch is tested without Docker.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go mod tidy
```

Environment: `GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`. The repo already requires
`webtyp.com/update` v0.0.5.

## Design gate

1. **Prior art.** `docker compose up -d --no-deps <service>` recreates one service with a new image and leaves the
   others running (Docker Compose docs, unverified: not read in this session). Kubernetes' rollout waits for readiness
   and `kubectl rollout undo` returns to the previous revision (unverified). Same: switch one service, wait for health,
   undo on failure.
2. **Novice names.** `selfupdate.Applier{Engine, Dir}.Apply(state, manifest)`, `selfupdate.Engine` (`Backup`, `Pull`,
   `Switch`, `Healthy`), `selfupdate.Compose{Dir}` (the real engine), `ErrUnhealthy`, `ErrRollback`.
3. **Complexity ledger.** Concepts +2 (Applier, Engine) · one way to switch (APP_IMAGE in `.env` + recreate `app`).
4. **Where it belongs.** `selfupdate`, the only piece with Docker access.
5. **Deletes.** Nothing: new capability.

## APIs used

| Symbol | Exact signature | Where |
|---|---|---|
| `update.Manifest`, `(Manifest).Reference()` | `struct { Version, Image, Digest string }`; `Reference() string` = `Image + "@" + Digest` | `webtyp.com/update` v0.0.5 |
| `State`, `(State).Save`, `(State).HasFailed`, `LoadState` | as in `state.go` (fields `Version, Image, PreviousVersion, PreviousImage string; Failed []string`) | this repo |
| `DirBackups`, `FileEnv`, `FileState`, `EnvAppImage`, `ServiceApp`, `ServiceDB`, `stateFileMode`, `tmpSuffix` | `layout.go`, `state.go` | this repo |

If a signature differs, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 1 — tests first (`tests/`, `package tests`)

Red after this stage.

### `tests/apply_test.go` — one test per branch of `docs/diagrams/APPLY_FLOW.md`

A fake engine in the test:

```go
type fakeEngine struct {
	calls        []string // "backup", "pull <ref>", "switch <ref>", "healthy"
	backupErr    error
	pullErr      error
	switchErrFor string // Switch(ref) returns switchErr only when ref == switchErrFor
	switchErr    error
	healthy      bool
}
```

(Use a pointer receiver so `calls` accumulates. `Backup(w io.Writer) error` writes `"dump"` to `w` unless
`backupErr`; every method appends to `calls`.) Clock: `Now` returns a time that `Sleep` advances, so waiting
never sleeps for real. Manifest: `update.Manifest{Version: "v1.2.0", Image: "ghcr.io/acme/shop", Digest: <64 hex>}`;
running state `State{Version: "v1.1.0", Image: "ghcr.io/acme/shop@sha256:<old>"}`; `Dir: t.TempDir()`,
`Wait: 10*time.Second`, `Every: time.Second`.

| Test | Fake | Expected |
|---|---|---|
| `TestApply_Healthy` | healthy | `err == nil`; calls `backup, pull <new>, switch <new>, healthy`; returned and saved state (`LoadState(dir/state.json)`): `Version v1.2.0`, `Image <new>`, `PreviousVersion v1.1.0`, `PreviousImage <old>`; one `.sql` file in `dir/backups` containing `dump` |
| `TestApply_BackupFails` | `backupErr` | that error; calls only `backup`; no `.sql` left in `backups/`; state unchanged and `state.json` not written |
| `TestApply_PullFails` | `pullErr` | that error; no `switch` call; state unchanged |
| `TestApply_Unhealthy` | never healthy | `errors.Is(err, ErrUnhealthy)`; calls end with `switch <new>`, several `healthy`, `switch <old>`; saved state keeps `Version v1.1.0`, `Image <old>`, `Failed` contains `v1.2.0` |
| `TestApply_SwitchFailsCountsAsUnhealthy` | `switchErrFor = <new>` | `errors.Is(err, ErrUnhealthy)` and the switch error is inside it (`errors.Is(err, switchErr)`); back to `<old>`; `v1.2.0` failed |
| `TestApply_RollbackFails` | never healthy, `switchErrFor = <old>` | `errors.Is(err, ErrUnhealthy)` and `errors.Is(err, ErrRollback)` |
| `TestApply_FirstInstallUnhealthy` | state `State{}`, never healthy | `ErrUnhealthy`; no second `switch` (nothing to return to); `Failed` contains `v1.2.0` |
| `TestApply_KeepsLastBackups` | `Keep: 2`, three successful applies in a row (advance the clock between them) | exactly 2 `.sql` files, the two newest |

### `tests/compose_test.go` — the real engine with a fake command runner

`Compose{Dir: dir, Run: fakeRun, Probe: func() bool { return true }}` where `fakeRun(dir string, stdout io.Writer, name string, args ...string) error`
records `dir` and `name+" "+strings.Join(args, " ")`, writes `"dump"` to `stdout`, and can return an error.

| Test | Expected |
|---|---|
| `TestCompose_Backup` | runs in `dir`: `docker compose exec -T db sh -c pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"`; the writer receives `dump` |
| `TestCompose_Pull` | `docker pull <ref>` |
| `TestCompose_Switch` | `.env` with `APP_DOMAIN=app.example.com\nAPP_IMAGE=old\n` becomes `APP_DOMAIN=app.example.com\nAPP_IMAGE=<ref>\n` (other lines kept, mode 0600); then `docker compose up -d --no-deps app` |
| `TestCompose_SwitchAddsMissingKey` | `.env` without `APP_IMAGE` → the line is appended |
| `TestCompose_RunFails` | `fakeRun` returns an error → `Pull` returns it |

Commit after this stage.

## Stage 2 — code

### `layout.go` — add to the constant block

```go
	// AppPort and AppInternetPort are the ports the app listens on inside its
	// container: the office door (published on the host's 443) and the internet
	// door (reached only by cloudflared on the internal network).
	AppPort         = "8443"
	AppInternetPort = "8444"
	// EnvAppDomain is the name the app is reached by; the same key httpd.FromEnv reads.
	EnvAppDomain = "APP_DOMAIN"
```

### `apply.go` (new)

```go
package selfupdate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"webtyp.com/update"
)

var (
	// ErrUnhealthy: the new version did not answer /health in time; the previous one is back.
	ErrUnhealthy = errors.New("selfupdate: the new version did not become healthy; the previous one is back")
	// ErrRollback: going back to the previous version failed too.
	ErrRollback = errors.New("selfupdate: could not return to the previous version")
)

const (
	defaultWait      = 120 * time.Second
	defaultEvery     = 3 * time.Second
	defaultKeep      = 7
	backupSuffix     = ".sql"
	backupDirMode    = 0o700
	backupTimeLayout = "20060102-150405"
	backupNameSep    = "_"
	noVersion        = "none"
)

// Engine is what applying a version needs from Docker. Compose implements it.
type Engine interface {
	Backup(w io.Writer) error // dump the database into w
	Pull(ref string) error    // fetch image@digest
	Switch(ref string) error  // write APP_IMAGE=ref in .env and recreate only the app service
	Healthy() bool            // one probe of the app's /health
}

// Applier switches the app to a verified version and returns to the previous
// one when the new one does not become healthy.
// Example: next, err := selfupdate.Applier{Engine: selfupdate.Compose{Dir: dir}, Dir: dir}.Apply(state, m)
type Applier struct {
	Engine Engine
	Dir    string                // the app's folder, /opt/<app>
	Wait   time.Duration         // how long to wait for health; 0 = 120s
	Every  time.Duration         // between probes; 0 = 3s
	Keep   int                   // database dumps kept; 0 = 7
	Now    func() time.Time      // nil = time.Now
	Sleep  func(time.Duration)   // nil = time.Sleep
}

// Apply runs docs/diagrams/APPLY_FLOW.md and returns the state to keep (also saved).
func (a Applier) Apply(s State, m update.Manifest) (State, error) {
	if err := a.backup(s.Version); err != nil {
		return s, err
	}
	ref := m.Reference()
	if err := a.Engine.Pull(ref); err != nil {
		return s, err
	}
	switchErr := a.Engine.Switch(ref)
	if switchErr == nil && a.waitHealthy() {
		next := State{Version: m.Version, Image: ref, PreviousVersion: s.Version, PreviousImage: s.Image, Failed: s.Failed}
		return next, next.Save(a.statePath())
	}
	failed := s
	failed.Failed = append(append([]string(nil), s.Failed...), m.Version)
	result := ErrUnhealthy
	if switchErr != nil {
		result = errors.Join(ErrUnhealthy, switchErr)
	}
	if s.Image != "" {
		if err := a.Engine.Switch(s.Image); err != nil {
			result = errors.Join(result, fmt.Errorf("%w: %v", ErrRollback, err))
		}
	}
	if err := failed.Save(a.statePath()); err != nil {
		result = errors.Join(result, err)
	}
	return failed, result
}

func (a Applier) waitHealthy() bool {
	deadline := a.now().Add(a.wait())
	for {
		if a.Engine.Healthy() {
			return true
		}
		if !a.now().Before(deadline) {
			return false
		}
		a.sleep(a.every())
	}
}

func (a Applier) backup(version string) error {
	dir := filepath.Join(a.Dir, DirBackups)
	if err := os.MkdirAll(dir, backupDirMode); err != nil {
		return err
	}
	if version == "" {
		version = noVersion
	}
	path := filepath.Join(dir, a.now().Format(backupTimeLayout)+backupNameSep+version+backupSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, stateFileMode)
	if err != nil {
		return err
	}
	if err := a.Engine.Backup(f); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return a.prune(dir)
}

// prune keeps the newest dumps; names start with a timestamp, so name order is age order.
func (a Applier) prune(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var dumps []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), backupSuffix) {
			dumps = append(dumps, e.Name())
		}
	}
	sort.Strings(dumps)
	for len(dumps) > a.keep() {
		if err := os.Remove(filepath.Join(dir, dumps[0])); err != nil {
			return err
		}
		dumps = dumps[1:]
	}
	return nil
}

func (a Applier) statePath() string { return filepath.Join(a.Dir, FileState) }

func (a Applier) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a Applier) sleep(d time.Duration) {
	if a.Sleep != nil {
		a.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (a Applier) wait() time.Duration {
	if a.Wait > 0 {
		return a.Wait
	}
	return defaultWait
}

func (a Applier) every() time.Duration {
	if a.Every > 0 {
		return a.Every
	}
	return defaultEvery
}

func (a Applier) keep() int {
	if a.Keep > 0 {
		return a.Keep
	}
	return defaultKeep
}
```

### `compose.go` (new)

```go
package selfupdate

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	cmdDocker    = "docker"
	argCompose   = "compose"
	argExec      = "exec"
	argNoTTY     = "-T"
	argPull      = "pull"
	argUp        = "up"
	argDetach    = "-d"
	argNoDeps    = "--no-deps"
	argShell     = "sh"
	argShellC    = "-c"
	// dumpScript runs inside the db container, with the user and database the
	// official image already knows from its environment.
	dumpScript   = `pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"`
	envSeparator = "="
	lineEnd      = "\n"
	healthScheme = "https://"
	healthPath   = "/health"
	healthTimeout = 5 * time.Second
)

// ErrNoDomain: .env has no APP_DOMAIN, so the health probe cannot check the certificate.
var ErrNoDomain = errors.New("selfupdate: .env has no APP_DOMAIN")

// Compose is the Engine over the docker CLI, run in the app's folder.
// Example: selfupdate.Compose{Dir: selfupdate.AppDir("shop")}
type Compose struct {
	Dir   string
	Run   func(dir string, stdout io.Writer, name string, args ...string) error // nil = os/exec
	Probe func() bool                                                       // nil = HTTPS GET of the app's /health
}

var _ Engine = Compose{}

func (c Compose) Backup(w io.Writer) error {
	return c.run(w, cmdDocker, argCompose, argExec, argNoTTY, ServiceDB, argShell, argShellC, dumpScript)
}

func (c Compose) Pull(ref string) error { return c.run(io.Discard, cmdDocker, argPull, ref) }

func (c Compose) Switch(ref string) error {
	if err := setEnv(filepath.Join(c.Dir, FileEnv), EnvAppImage, ref); err != nil {
		return err
	}
	return c.run(io.Discard, cmdDocker, argCompose, argUp, argDetach, argNoDeps, ServiceApp)
}

// Healthy asks the app's office door for /health over the internal network and
// verifies its certificate against APP_DOMAIN (the name the certificate is for).
func (c Compose) Healthy() bool {
	if c.Probe != nil {
		return c.Probe()
	}
	domain, err := readEnv(filepath.Join(c.Dir, FileEnv), EnvAppDomain)
	if err != nil {
		return false
	}
	client := &http.Client{
		Timeout:   healthTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: domain}},
	}
	resp, err := client.Get(healthScheme + ServiceApp + ":" + AppPort + healthPath)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c Compose) run(stdout io.Writer, name string, args ...string) error {
	if c.Run != nil {
		return c.Run(c.Dir, stdout, name, args...)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Dir = c.Dir
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// setEnv writes key=value in the .env file, keeping every other line; atomic, owner-only.
func setEnv(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var out []string
	found := false
	for _, line := range strings.Split(string(data), lineEnd) {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, key+envSeparator) {
			out = append(out, key+envSeparator+value)
			found = true
			continue
		}
		out = append(out, line)
	}
	if !found {
		out = append(out, key+envSeparator+value)
	}
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, []byte(strings.Join(out, lineEnd)+lineEnd), stateFileMode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readEnv(path, key string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), lineEnd) {
		if v, ok := strings.CutPrefix(line, key+envSeparator); ok && v != "" {
			return v, nil
		}
	}
	return "", ErrNoDomain
}
```

Run `gotest`: all green. Commit after this stage.

## Stage 3 — docs

- `docs/ARCHITECTURE.md`, section «Switching»: add that the app listens on `AppPort` (8443, office door) and
  `AppInternetPort` (8444, internet door) inside its container, and that the health probe checks the certificate
  against `APP_DOMAIN`; the apply diagram's caption says «Built by a later plan» — change it to say every branch has a
  test in `tests/apply_test.go`.
- `README.md`: a short example of `Applier{Engine: Compose{Dir: dir}, Dir: dir}.Apply(state, m)`.

Commit after this stage.

## Delivery check

1. `gotest` passes; no test runs `docker` or reaches the network. A PR that does not compile, or whose tests could not run, is a failed delivery.
2. `grep -rn "InsecureSkipVerify" --include=*.go .` → empty.
3. `git diff --stat main` lists only `layout.go`, `apply.go`, `compose.go`, `tests/apply_test.go`, `tests/compose_test.go`,
   `docs/ARCHITECTURE.md`, `docs/diagrams/APPLY_FLOW.md`, `README.md`.

## Delivery rules

- Commit after every stage. Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own work.
- Stuck for 30 minutes: open the PR anyway, listing what fails.
- No `map` is needed anywhere; the fake engine uses plain fields.

## Stages

| # | Files | Result |
|---|---|---|
| 1 | `tests/apply_test.go`, `tests/compose_test.go` | Every branch of the apply flow and the docker commands, red |
| 2 | `layout.go`, `apply.go`, `compose.go` | Green |
| 3 | `docs/ARCHITECTURE.md`, `docs/diagrams/APPLY_FLOW.md`, `README.md` | Documented |
