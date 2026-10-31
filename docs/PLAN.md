---
PLAN: "feat: install provisions /opt/<app> with secret owners/modes, serve runs the supervisor and 24h backup, Dockerfile and release Action"
TAG: v0.0.9
EXECUTOR: jules
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# S4c-2b — `selfupdate install`, `selfupdate serve`, `Dockerfile` and release Action

## Context

`selfupdate` (v0.0.8) already verifies releases (`Checker`), applies updates with rollback (`Applier`), serves the HTTP
API (`Supervisor.Handler`), takes daily database dumps (`Supervisor.Backup`) and generates the canonical `compose.yaml`
(`ComposeFile`). This plan adds the executable entrypoint and its two commands:

1. `selfupdate install <owner/repo>`: downloads and verifies the latest signed Release (`manifest.txt`, `manifest.sig`,
   `compose.yaml`), extracts `<app>` and `<domain>` from the verified compose file, creates `/opt/<app>/{secrets,backups}`,
   generates the database passwords, `initdb.sql` and `supervisor_token`, sets the owner UID (`0`, `PostgresUID = 999`,
   `NonRootUID = 65532`) and mode (`0400`) on every secret file as required by `docs/ARCHITECTURE.md`, writes
   `update.pub`, `compose.yaml` and `.env`, pulls the image, starts `db`, runs `migrate`, brings the stack up, waits for
   `/health`, and records `state.json`.
2. `selfupdate serve <owner/repo> <app>`: loads `update.pub`, `secrets/github_token` and `secrets/supervisor_token` from
   `/opt/<app>`, starts the 24-hour `Supervisor.Backup()` loop, and serves `Supervisor.Handler(token)` on `:8080`.
3. `Dockerfile` and `.github/workflows/release.yml`: builds and publishes `ghcr.io/webtyp/selfupdate:<tag>` on every
   tag push.

Delivery rules: commit after every stage. Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash`
on your own work. Stuck for 30 minutes: open the PR anyway, listing what fails. A PR that does not compile, or whose
tests could not run, is a failed delivery.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
```

Environment: `GOPRIVATE=webtyp.com/*`, `GOFLAGS=-mod=mod`. No test may touch the network, Docker or `/opt`.

## Ecosystem search

- Signature verification and manifest parsing: `Checker.Check` (`check.go`) and `update.VerifyManifest` — **reuse**.
- Health wait and atomic file writes: `Applier` (`apply.go`) and `Compose` (`compose.go`) — **reuse**.
- Server layout constants (`DirRoot`, `DirSecrets`, `FilePublicKey`, `PostgresUID`, `NonRootUID`, etc.): `layout.go` —
  **reuse**.

## Design gate

1. **Prior art.** Kamal `kamal setup` bootstraps the server on first deploy and starts the containers
   (https://kamal-deploy.org/docs/commands/setup/); K3s installs with a single command that writes credentials with
   restricted permissions and starts the service (https://docs.k3s.io/quick-start); Docker Compose mounts file secrets
   preserving host UID and mode (https://docs.docker.com/compose/how-tos/use-secrets/). Same here: one `install`
   command sets up `/opt/<app>` with exact secret ownership and starts Compose; `serve` runs inside the supervisor
   container.
2. **Novice names.** `DecodePublicKey`, `InitDBSQL`, `ParseComposeApp`, `InstallConfig`, `Install`, `BackupInterval`,
   `OpenSupervisor`, `RunBackupLoop`, CLI `selfupdate install <owner/repo>` and `selfupdate serve <owner/repo> <app>`.
3. **Complexity ledger.** Concepts +2 (`Install`, `OpenSupervisor`) · ways to bootstrap a server: 1 (`selfupdate install`)
   · ways to run the supervisor daemon: 1 (`selfupdate serve`).
4. **Where it belongs.** `webtyp/selfupdate`.
5. **Deletes.** Nothing.

## APIs used

| Symbol | Signature | File |
|---|---|---|
| `Checker.Check` | `func (c Checker) Check(s State) (u Update, ok bool, err error)` | `check.go` |
| `ComposeFile`, `App` | `func ComposeFile(app App) ([]byte, error)` | `composefile.go` |
| `Supervisor`, `NewSupervisor`, `(*Supervisor).Backup`, `(*Supervisor).Handler` | as in `supervisor.go` | `supervisor.go` |
| Layout constants | `DirRoot`, `FileCompose`, `FileEnv`, `FilePublicKey`, `FileState`, `FileInitDB`, `DirSecrets`, `DirBackups`, `ServiceDB`, `ServiceApp`, `SupervisorPort`, `EnvAppDomain`, `EnvAppImage`, `EnvSupervisorImage`, `EnvLANAddress`, `SecretGitHubToken`, `SecretSupervisorToken`, `SecretTunnelToken`, `SecretDNSToken`, `SecretDBOwner`, `SecretDBApp`, `SecretDatabaseURL`, `SecretDatabaseOwnerURL`, `NonRootUID`, `PostgresUID` | `layout.go` |

If a signature differs, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 1 — diagrams `docs/diagrams/INSTALL_FLOW.md` and `docs/diagrams/SERVE_FLOW.md` (new)

Create `docs/diagrams/INSTALL_FLOW.md`:

````markdown
# Install flow

What `selfupdate.Install(cfg)` does on a server. Every branch is tested in `tests/install_test.go`.

```mermaid
flowchart TD
    A["Install(InstallConfig)"] --> V{"Repo, PublicKey, GitHubToken,<br/>TunnelToken, DNSToken valid?"}
    V -->|no| EV["ErrAppRepo / ErrPublicKey / ErrNoGitHubToken /<br/>ErrNoTunnelToken / ErrNoDNSToken"]
    V -->|yes| C["Checker.Check(State{})"]
    C -->|error or !ok| EC["error or ErrNoRelease"]
    C -->|verified Update{Manifest, Compose}| P["ParseComposeApp(Compose) → app, domain"]
    P -->|invalid| EP["ErrAppName / ErrAppDomain"]
    P -->|ok| S["Write /opt/<app>/secrets/* (mode 0400, chown 0 / 999 / 65532)<br/>Write update.pub, compose.yaml, .env"]
    S --> D["Engine.Pull(ref) → Up(db) → Engine.Migrate(ref) → Up(all)"]
    D --> H{"Engine.Healthy() within Wait?"}
    H -->|no| EH["ErrUnhealthy"]
    H -->|yes| W["Save state.json (Version, Image)"]
```
````

Create `docs/diagrams/SERVE_FLOW.md`:

````markdown
# Serve flow

What `selfupdate serve <owner/repo> <app>` runs inside the supervisor container. Tested in `tests/serve_test.go`.

```mermaid
flowchart TD
    A["OpenSupervisor(dir, repo, rel, eng)"] --> K{"Read and decode update.pub"}
    K -->|missing or invalid| EK["ErrPublicKey"]
    K -->|ok| G{"Read secrets/github_token"}
    G -->|missing or empty| EG["ErrNoGitHubToken"]
    G -->|ok| T{"Read secrets/supervisor_token"}
    T -->|missing or empty| ET["ErrNoSupervisorToken"]
    T -->|ok| S["NewSupervisor + RunBackupLoop(24h) + Handler(token) on :8080"]
```
````

Commit.

## Stage 2 — `install.go` and `serve.go` (new)

Create `install.go`:

```go
package selfupdate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	// ErrPublicKey: update.pub is missing or is not a P-256 PKIX public key.
	ErrPublicKey = errors.New("selfupdate: update.pub is not a valid P-256 public key")
	// ErrNoGitHubToken: secrets/github_token is missing or empty.
	ErrNoGitHubToken = errors.New("selfupdate: github token is required")
	// ErrNoSupervisorToken: secrets/supervisor_token is missing or empty.
	ErrNoSupervisorToken = errors.New("selfupdate: supervisor token is required")
	// ErrNoTunnelToken: tunnel token is missing or empty.
	ErrNoTunnelToken = errors.New("selfupdate: tunnel token is required")
	// ErrNoDNSToken: DNS token is missing or empty.
	ErrNoDNSToken = errors.New("selfupdate: DNS token is required")
	// ErrNoRelease: the repository has no release to install.
	ErrNoRelease = errors.New("selfupdate: no release found to install")
)

const (
	// DefaultSupervisorImage is the image written to .env when InstallConfig.SupervisorImage is empty.
	DefaultSupervisorImage = "ghcr.io/webtyp/selfupdate:latest"
	// DefaultLANAddress is the host bind address written to .env when InstallConfig.LANAddress is empty.
	DefaultLANAddress = "127.0.0.1"

	secretFileMode = 0o400
	dirMode        = 0o700
	rootUID        = 0
	randomBytes    = 32
	composeNamePfx = "name:"
	composeDomPfx  = "APP_DOMAIN:"
)

// DecodePublicKey accepts a P-256 PKIX public key either as raw DER bytes or base64 text,
// validates it, and returns the raw DER bytes that update.VerifyManifest expects.
// Example: pub, err := selfupdate.DecodePublicKey(data)
func DecodePublicKey(data []byte) ([]byte, error) {
	if isValidP256PublicKey(data) {
		return data, nil
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, ErrPublicKey
	}
	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil || !isValidP256PublicKey(raw) {
		return nil, ErrPublicKey
	}
	return raw, nil
}

func isValidP256PublicKey(der []byte) bool {
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return false
	}
	ec, ok := pub.(*ecdsa.PublicKey)
	return ok && ec.Curve == elliptic.P256()
}

// ParseComposeApp reads the app Name and APP_DOMAIN from a compose.yaml generated by ComposeFile.
// Example: name, domain, err := selfupdate.ParseComposeApp(composeBytes)
func ParseComposeApp(compose []byte) (name, domain string, err error) {
	for _, line := range strings.Split(string(compose), lineEnd) {
		trimmed := strings.TrimSpace(line)
		if name == "" && strings.HasPrefix(line, composeNamePfx) {
			name = strings.TrimSpace(strings.TrimPrefix(line, composeNamePfx))
		}
		if domain == "" && strings.HasPrefix(trimmed, composeDomPfx) {
			domain = strings.TrimSpace(strings.TrimPrefix(trimmed, composeDomPfx))
		}
	}
	if !validName(name) {
		return "", "", ErrAppName
	}
	if !validDomain(domain) {
		return "", "", ErrAppDomain
	}
	return name, domain, nil
}

// InitDBSQL returns the SQL script for secrets/initdb.sql that creates the rows-only <db>_app role
// and grants it DML on tables and USAGE/SELECT on sequences created by <db>_owner.
// Example: sql := selfupdate.InitDBSQL("acme_shop", appPassword)
func InitDBSQL(db, appPassword string) string {
	appRole := db + "_app"
	ownerRole := db + "_owner"
	escaped := strings.ReplaceAll(appPassword, "'", "''")
	return fmt.Sprintf(`CREATE ROLE %s WITH LOGIN PASSWORD '%s';
GRANT CONNECT ON DATABASE %s TO %s;
GRANT USAGE ON SCHEMA public TO %s;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %s;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %s;
ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s;
ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO %s;
`, appRole, escaped, db, appRole, appRole, appRole, appRole, ownerRole, appRole, ownerRole, appRole)
}

// InstallConfig holds the inputs and injectable seams for first-time installation on a server.
type InstallConfig struct {
	Repo            string
	Root            string // "" = DirRoot ("/opt")
	SupervisorImage string // "" = DefaultSupervisorImage
	LANAddress      string // "" = DefaultLANAddress
	PublicKey       string // base64 or raw P-256 public key
	GitHubToken     string
	TunnelToken     string
	DNSToken        string

	Releases Releases
	Engine   func(dir string) Engine
	Up       func(dir string, services ...string) error
	Chown    func(path string, uid, gid int) error
	Wait     time.Duration
	Every    time.Duration
	Now      func() time.Time
	Sleep    func(time.Duration)
}

// Install provisions /opt/<app>, writes secret files with their container UIDs and 0400 mode,
// starts db, runs migrate, brings the stack up, waits for health, and writes state.json.
// Example: state, err := selfupdate.Install(cfg)
func Install(cfg InstallConfig) (State, error) {
	if !validRepo(cfg.Repo) {
		return State{}, ErrAppRepo
	}
	pub, err := DecodePublicKey([]byte(cfg.PublicKey))
	if err != nil {
		return State{}, err
	}
	ghToken := strings.TrimSpace(cfg.GitHubToken)
	if ghToken == "" {
		return State{}, ErrNoGitHubToken
	}
	tunnelToken := strings.TrimSpace(cfg.TunnelToken)
	if tunnelToken == "" {
		return State{}, ErrNoTunnelToken
	}
	dnsToken := strings.TrimSpace(cfg.DNSToken)
	if dnsToken == "" {
		return State{}, ErrNoDNSToken
	}

	rel := cfg.Releases
	if rel == nil {
		rel = GitHub{Repo: cfg.Repo, Token: ghToken}
	}
	u, ok, err := (Checker{Releases: rel, PublicKey: pub}).Check(State{})
	if err != nil {
		return State{}, err
	}
	if !ok {
		return State{}, ErrNoRelease
	}
	appName, domain, err := ParseComposeApp(u.Compose)
	if err != nil {
		return State{}, err
	}

	root := cfg.Root
	if root == "" {
		root = DirRoot
	}
	dir := filepath.Join(root, appName)
	secretsDir := filepath.Join(dir, DirSecrets)
	backupsDir := filepath.Join(dir, DirBackups)
	for _, d := range []string{dir, secretsDir, backupsDir} {
		if err := os.MkdirAll(d, dirMode); err != nil {
			return State{}, err
		}
	}

	ownerPass, err := existingOrRandomSecret(filepath.Join(secretsDir, SecretDBOwner))
	if err != nil {
		return State{}, err
	}
	appPass, err := existingOrRandomSecret(filepath.Join(secretsDir, SecretDBApp))
	if err != nil {
		return State{}, err
	}
	supToken, err := existingOrRandomSecret(filepath.Join(secretsDir, SecretSupervisorToken))
	if err != nil {
		return State{}, err
	}

	db := databaseName(appName)
	dbURL := fmt.Sprintf("postgres://%s_app:%s@%s:5432/%s?sslmode=disable", db, appPass, ServiceDB, db)
	dbOwnerURL := fmt.Sprintf("postgres://%s_owner:%s@%s:5432/%s?sslmode=disable", db, ownerPass, ServiceDB, db)

	chown := cfg.Chown
	if chown == nil {
		chown = os.Chown
	}
	secrets := []struct {
		name string
		data string
		uid  int
	}{
		{SecretDBOwner, ownerPass, rootUID},
		{SecretDBApp, appPass, rootUID},
		{FileInitDB, InitDBSQL(db, appPass), PostgresUID},
		{SecretDatabaseURL, dbURL, NonRootUID},
		{SecretDatabaseOwnerURL, dbOwnerURL, NonRootUID},
		{SecretDNSToken, dnsToken, NonRootUID},
		{SecretSupervisorToken, supToken, NonRootUID},
		{SecretTunnelToken, tunnelToken, NonRootUID},
		{SecretGitHubToken, ghToken, rootUID},
	}
	for _, s := range secrets {
		p := filepath.Join(secretsDir, s.name)
		if err := writeSecret(p, []byte(s.data), s.uid, chown); err != nil {
			return State{}, err
		}
	}

	if err := writeAtomic(filepath.Join(dir, FilePublicKey), pub); err != nil {
		return State{}, err
	}
	if err := writeAtomic(filepath.Join(dir, FileCompose), u.Compose); err != nil {
		return State{}, err
	}

	supImage := cfg.SupervisorImage
	if supImage == "" {
		supImage = DefaultSupervisorImage
	}
	lanAddr := cfg.LANAddress
	if lanAddr == "" {
		lanAddr = DefaultLANAddress
	}
	ref := u.Manifest.Reference()
	envData := strings.Join([]string{
		EnvAppImage + envSeparator + ref,
		EnvAppDomain + envSeparator + domain,
		EnvSupervisorImage + envSeparator + supImage,
		EnvLANAddress + envSeparator + lanAddr,
	}, lineEnd) + lineEnd
	if err := writeAtomic(filepath.Join(dir, FileEnv), []byte(envData)); err != nil {
		return State{}, err
	}

	var eng Engine = Compose{Dir: dir}
	if cfg.Engine != nil {
		eng = cfg.Engine(dir)
	}
	up := cfg.Up
	if up == nil {
		up = defaultUp
	}

	if err := eng.Pull(ref); err != nil {
		return State{}, err
	}
	if err := up(dir, ServiceDB); err != nil {
		return State{}, err
	}
	if err := eng.Migrate(ref); err != nil {
		return State{}, fmt.Errorf("%w: %v", ErrMigrate, err)
	}
	if err := up(dir); err != nil {
		return State{}, err
	}
	applier := Applier{
		Engine: eng,
		Dir:    dir,
		Wait:   cfg.Wait,
		Every:  cfg.Every,
		Now:    cfg.Now,
		Sleep:  cfg.Sleep,
	}
	if !applier.waitHealthy() {
		return State{}, ErrUnhealthy
	}
	st := State{Version: u.Manifest.Version, Image: ref}
	if err := st.Save(filepath.Join(dir, FileState)); err != nil {
		return State{}, err
	}
	return st, nil
}

func existingOrRandomSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, nil
		}
	}
	buf := make([]byte, randomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeSecret(path string, data []byte, uid int, chown func(string, int, int) error) error {
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, data, secretFileMode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, secretFileMode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := chown(tmp, uid, uid); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func defaultUp(dir string, services ...string) error {
	args := append([]string{argCompose, argUp, argDetach}, services...)
	cmd := exec.Command(cmdDocker, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", cmdDocker, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

Create `serve.go`:

```go
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
```

Commit.

## Stage 3 — tests `tests/install_test.go` and `tests/serve_test.go` (new)

Create `tests/install_test.go`:
- `TestDecodePublicKey_RawAndBase64`: generates a P-256 key pair with `asym.GenerateKeyPair()`, verifies `DecodePublicKey(pub)` and `DecodePublicKey([]byte(base64.StdEncoding.EncodeToString(pub)+"\n"))` both return `pub`, and invalid bytes return `ErrPublicKey`.
- `TestInstall_ValidationErrors`: checks `ErrAppRepo`, `ErrPublicKey`, `ErrNoGitHubToken`, `ErrNoTunnelToken`, `ErrNoDNSToken`.
- `TestInstall_ProvisionsAndStarts`:
  - Uses `ComposeFile(App{Name: "acme-shop", Domain: "app.example.com", Repo: "acme/shop"})`, signs a manifest for `v1.0.0`, passes a fake `Releases`, fake `Engine`, fake `Up` (recording calls `["db"]` then `[]`) and fake `Chown` (recording `map[string]int` UIDs).
  - Asserts `state.Version == "v1.0.0"`.
  - Asserts every file in `secrets/` has mode `0400` and the exact UID recorded by `Chown`:
    - `db_owner_password`, `db_app_password`, `github_token`: `0`
    - `initdb.sql`: `selfupdate.PostgresUID` (`999`)
    - `database_url`, `database_owner_url`, `dns_token`, `supervisor_token`, `tunnel_token`: `selfupdate.NonRootUID` (`65532`)
  - Asserts `initdb.sql` contains `CREATE ROLE acme_shop_app` and `ALTER DEFAULT PRIVILEGES FOR ROLE acme_shop_owner`.
  - Asserts `.env` contains `APP_IMAGE=`, `APP_DOMAIN=app.example.com`, `SUPERVISOR_IMAGE=`, `LAN_ADDRESS=127.0.0.1`.
  - Re-runs `Install` on the same directory and asserts `db_owner_password`, `db_app_password` and `supervisor_token` are preserved (idempotent).
- `TestInstall_Unhealthy`: when `Engine` is unhealthy, returns `ErrUnhealthy` and does not write `state.json`.

Create `tests/serve_test.go`:
- `TestOpenSupervisor_MissingFiles`: checks `ErrAppRepo`, `ErrPublicKey`, `ErrNoGitHubToken`, `ErrNoSupervisorToken`.
- `TestOpenSupervisor_AndBackupLoop`: provisions a temp dir via `Install`, opens supervisor via `OpenSupervisor`, runs `RunBackupLoop(ctx, sup, 5*time.Millisecond)` until a `.sql` dump appears in `backups/`, cancels `ctx`, and verifies `sup.Handler(token)` answers `200` on `GET /status` with version `v1.0.0`.

Run `gotest`: green. Commit.

## Stage 4 — CLI `cmd/selfupdate/main.go`, `Dockerfile` and `.github/workflows/release.yml` (new)

Create `cmd/selfupdate/main.go`:
- Subcommand `serve <owner/repo> <app>`:
  - Calls `sup, token, err := selfupdate.OpenSupervisor(selfupdate.AppDir(app), repo, nil, nil)`.
  - Starts `go selfupdate.RunBackupLoop(context.Background(), sup, selfupdate.BackupInterval)`.
  - Runs `http.ListenAndServe(":"+selfupdate.SupervisorPort, sup.Handler(token))`.
- Subcommand `install <owner/repo>`:
  - Reads `PUBLIC_KEY`, `GITHUB_TOKEN`, `TUNNEL_TOKEN`, `DNS_TOKEN`, `LAN_ADDRESS`, `SUPERVISOR_IMAGE` from environment variables (or prompts on `os.Stdin` line-by-line when an env var is empty), calls `selfupdate.Install(cfg)`, prints the installed version to stdout and exits 0 (or prints error to stderr and exits 1).

Create `Dockerfile`:

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/selfupdate ./cmd/selfupdate

FROM alpine:3.21
RUN apk add --no-cache ca-certificates docker-cli docker-cli-compose
COPY --from=build /out/selfupdate /usr/local/bin/selfupdate
ENTRYPOINT ["/usr/local/bin/selfupdate"]
```

Create `.github/workflows/release.yml`:

```yaml
name: release
on:
  push:
    tags:
      - "v*"
permissions:
  contents: read
  packages: write
jobs:
  image:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Log in to ghcr.io
        run: echo "${{ secrets.GITHUB_TOKEN }}" | docker login ghcr.io -u "${{ github.actor }}" --password-stdin
      - name: Build and push image
        run: |
          TAG="${GITHUB_REF_NAME}"
          docker build --platform linux/amd64 \
            -t "ghcr.io/webtyp/selfupdate:${TAG}" \
            -t "ghcr.io/webtyp/selfupdate:latest" .
          docker push "ghcr.io/webtyp/selfupdate:${TAG}"
          docker push "ghcr.io/webtyp/selfupdate:latest"
```

Run `gotest`: green. Commit.

## Stage 5 — docs

Update `README.md` and `docs/ARCHITECTURE.md`:
- Document `selfupdate install <owner/repo>` and `selfupdate serve <owner/repo> <app>`.
- Link `docs/diagrams/INSTALL_FLOW.md` and `docs/diagrams/SERVE_FLOW.md`.

Commit.

## Delivery check

1. `gotest` passes (including `-race`).
2. `git diff --stat main` lists only `install.go`, `serve.go`, `cmd/selfupdate/main.go`, `Dockerfile`,
   `.github/workflows/release.yml`, `tests/install_test.go`, `tests/serve_test.go`, `docs/diagrams/INSTALL_FLOW.md`,
   `docs/diagrams/SERVE_FLOW.md`, `docs/ARCHITECTURE.md`, `README.md`.
3. `git diff --stat main --diff-filter=D` → empty.

## Stages

| # | Files | Result |
|---|---|---|
| 1 | `docs/diagrams/INSTALL_FLOW.md`, `docs/diagrams/SERVE_FLOW.md` | Install and serve flow diagrams |
| 2 | `install.go`, `serve.go` | `DecodePublicKey`, `InitDBSQL`, `ParseComposeApp`, `Install`, `OpenSupervisor`, `RunBackupLoop` |
| 3 | `tests/install_test.go`, `tests/serve_test.go` | Full test suite for install and serve |
| 4 | `cmd/selfupdate/main.go`, `Dockerfile`, `.github/workflows/release.yml` | CLI, container image and GitHub Action |
| 5 | `README.md`, `docs/ARCHITECTURE.md` | Documented |
