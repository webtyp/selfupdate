---
PLAN: "feat: server layout, state file and the verified version check"
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 4449526814928660747
PR: https://github.com/webtyp/selfupdate/pull/1
REVIEWED_COMMIT: 0f57ba0c621013e5b8b3e81e10b6ec2d7af78423
---

> Read AGENTS.md at the repo root before any change. The design is `docs/ARCHITECTURE.md`; the flow this plan builds
> is `docs/diagrams/CHECK_FLOW.md`.

# Layout, state and check

## Context

`selfupdate` is the supervisor of a self-hosted webtyp app (read `docs/ARCHITECTURE.md` first). This first plan builds
the parts that touch neither Docker nor HTTP serving: the names of the server layout that `deploy` and `gorelease`
will import (`layout.go`), the supervisor's state file (`state.go`), reading Releases from GitHub (`github.go`) and the
check that finds a newer **verified** version (`check.go`). Switching images, the HTTP API, the install command and the
app's screen come in later plans. The repo is new: delete the `gonew` skeleton.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go get webtyp.com/update@v0.0.5
go get webtyp.com/crypto@v0.0.30
go mod tidy
```

Environment: `GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`. (`webtyp.com/crypto` is only used
by the tests, to make a key pair and sign test manifests.)

## Ecosystem search

- `webtyp.com/update` v0.0.5: `SignManifest`, `VerifyManifest`, `Manifest`, `ManifestAsset`, `SignatureAsset`,
  `IsOutdated` — **reuse**. Its `ResolveLatestVersion`/`DefaultDownload` read public repositories by URL and read the
  token from the process environment; the supervisor reads a **private** repository with a token from a secret file, so
  it calls the GitHub REST API itself (`github.go`).
- Nothing in `webtyp/` or `veltylabs/` keeps a supervisor state or names the server layout: **new**.

## Design gate

1. **Prior art.** Home Assistant's supervisor keeps the running and available versions and updates when the UI asks;
   Syncthing's upgrade checks a signed release before installing (unverified: pages not read in this session). GitHub's
   REST API serves private release assets at the asset `url` with `Accept: application/octet-stream`
   (unverified).
2. **Novice names.** `selfupdate.Checker{Releases, PublicKey}.Check(state)`, `selfupdate.GitHub{Repo, Token}`,
   `selfupdate.State`, `LoadState`, `State.Save`, `State.HasFailed`, `selfupdate.AppDir("shop")`.
3. **Complexity ledger.** New repo; concepts: Checker, Releases, State, layout constants.
4. **Where it belongs.** `selfupdate`, the supervisor; format and signature stay in `update`.
5. **Deletes.** The `gonew` skeleton `selfupdate.go` (`Selfupdate`, `New`).

## APIs used

| Symbol | Exact signature | Tag |
|---|---|---|
| `update.VerifyManifest` | `func VerifyManifest(text, signature, publicKey []byte) (Manifest, error)` — error text `update.ErrManifestSignature` when the signature does not match | `webtyp.com/update` v0.0.5 |
| `update.SignManifest` (tests) | `func SignManifest(m Manifest, privateKey []byte) (text, signature []byte, err error)` | v0.0.5 |
| `update.Manifest` | `struct { Version, Image, Digest string }` | v0.0.5 |
| `update.ManifestAsset`, `update.SignatureAsset` | `"manifest.txt"`, `"manifest.sig"` | v0.0.5 |
| `update.IsOutdated` | `func IsOutdated(current, latest string) bool` — false when either does not parse (so an empty current is false: handled below) | v0.0.5 |
| `asym.GenerateKeyPair` (tests) | `func GenerateKeyPair() (publicKey []byte, privateKey []byte, err error)` | `webtyp.com/crypto` v0.0.30 |

If a signature differs, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 1 — tests first (`tests/`, `package tests`)

Red after this stage (nothing exists yet). Fixtures: `testImage = "ghcr.io/acme/shop"`,
`testDigest = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`; a key pair from
`asym.GenerateKeyPair()`; manifests signed with `update.SignManifest`.

### `tests/github_test.go` — `GitHub` against an `httptest.Server`

The fake serves `GET /repos/acme/shop/releases/latest` → `{"tag_name":"v1.2.0","assets":[{"name":"manifest.txt","url":"<srv>/assets/1"},{"name":"manifest.sig","url":"<srv>/assets/2"}]}`
and `GET /assets/1`, `/assets/2` → bytes; it records the `Authorization` and `Accept` headers it received.

| Test | Expected |
|---|---|
| `TestGitHub_Latest` | `GitHub{Repo: "acme/shop", Token: "t", API: srv.URL}.Latest()` → tag `v1.2.0`, 2 assets; request carried `Authorization: Bearer t` and `Accept: application/vnd.github+json` |
| `TestGitHub_Download` | `Download(asset)` returns the bytes; request carried `Accept: application/octet-stream` |
| `TestGitHub_NotFound` | fake answers 404 → `errors.Is(err, selfupdate.ErrGitHubStatus)` and the message contains `404` |

### `tests/state_test.go`

| Test | Expected |
|---|---|
| `TestState_MissingFile` | `LoadState(dir+"/state.json")` on a new `t.TempDir()` → zero `State`, `nil` |
| `TestState_RoundTrip` | `Save` then `LoadState` → equal; the file mode is `0600` |
| `TestState_Corrupt` | file containing `not json` → `errors.Is(err, selfupdate.ErrStateCorrupt)` |
| `TestState_HasFailed` | `State{Failed: []string{"v1.1.0"}}.HasFailed("v1.1.0") == true`, `"v1.2.0"` false |

### `tests/check_test.go` — one test per branch of `docs/diagrams/CHECK_FLOW.md`

Use a fake `Releases` in the test (a struct with a `Release` and a `[]byte` per asset name, and an `error` to return
from `Latest`). Running state `State{Version: "v1.1.0"}` unless the row says otherwise.

| Test | Fake | Expected `(m, ok, err)` |
|---|---|---|
| `TestCheck_Available` | tag `v1.2.0`, manifest for `v1.2.0` signed with the test key | `ok`, `m.Version == "v1.2.0"`, `m.Reference() == testImage+"@"+testDigest`, `err == nil` |
| `TestCheck_FirstInstall` | same, state `State{}` (no version yet) | `ok` (an empty running version means any release is newer) |
| `TestCheck_NotNewer` | tag `v1.1.0` | `!ok`, `err == nil` |
| `TestCheck_Failed` | tag `v1.2.0`, state `Failed: ["v1.2.0"]` | `!ok`, `err == nil` |
| `TestCheck_LatestFails` | `Latest` returns an error | that error |
| `TestCheck_NoSignature` | no `manifest.sig` asset | `errors.Is(err, selfupdate.ErrNoAsset)`, message names `manifest.sig` |
| `TestCheck_WrongKey` | signed with another key pair | `err.Error() == update.ErrManifestSignature` |
| `TestCheck_VersionMismatch` | tag `v1.2.0`, manifest signed for `v1.0.9` | `errors.Is(err, selfupdate.ErrVersionMismatch)` |

Commit after this stage.

## Stage 2 — code

Delete `selfupdate.go`. Create these four files (package `selfupdate`).

### `layout.go`

```go
package selfupdate

// The server layout every self-hosted webtyp app shares (docs/ARCHITECTURE.md).
// deploy and gorelease import these names; they never retype them.
const (
	DirRoot       = "/opt" // one folder per app: /opt/<app>
	FileCompose   = "compose.yaml"
	FileEnv       = ".env"
	FilePublicKey = "update.pub"
	FileState     = "state.json"
	DirSecrets    = "secrets"
	DirBackups    = "backups"

	ServiceDB         = "db"
	ServiceApp        = "app"
	ServiceTunnel     = "cloudflared"
	ServiceSupervisor = "selfupdate"
	SupervisorPort    = "8080"

	// EnvAppImage is the .env key the compose reads as the app's image (image@digest).
	EnvAppImage = "APP_IMAGE"

	SecretGitHubToken     = "github_token"
	SecretSupervisorToken = "supervisor_token"
	SecretTunnelToken     = "tunnel_token"
	SecretDNSToken        = "dns_token"
	SecretDBOwner         = "db_owner_password"
	SecretDBApp           = "db_app_password"
)

// AppDir is the folder of one app on the server.
// Example: selfupdate.AppDir("shop") == "/opt/shop"
func AppDir(app string) string { return DirRoot + "/" + app }
```

### `state.go`

```go
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
```

### `github.go`

```go
package selfupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrGitHubStatus: GitHub answered something other than 200.
var ErrGitHubStatus = errors.New("selfupdate: GitHub answered")

const (
	defaultAPI    = "https://api.github.com"
	latestPath    = "/repos/%s/releases/latest"
	headerAccept  = "Accept"
	headerAuth    = "Authorization"
	bearerPrefix  = "Bearer "
	acceptJSON    = "application/vnd.github+json"
	acceptBinary  = "application/octet-stream"
)

// Release is one published version and its files.
type Release struct {
	Tag    string
	Assets []Asset
}

// Asset is one file of a Release; URL is the API address that serves its bytes.
type Asset struct {
	Name string
	URL  string
}

// Releases reads the published versions of the app.
type Releases interface {
	Latest() (Release, error)
	Download(a Asset) ([]byte, error)
}

// GitHub reads Releases through the GitHub REST API. Token (read-only) is
// required for a private repository.
// Example: gh := selfupdate.GitHub{Repo: "acme/shop", Token: token}
type GitHub struct {
	Repo   string       // "owner/name"
	Token  string
	API    string       // "" = https://api.github.com; tests point it at an httptest.Server
	Client *http.Client // nil = http.DefaultClient
}

var _ Releases = GitHub{}

func (g GitHub) Latest() (Release, error) {
	body, err := g.get(g.api()+fmt.Sprintf(latestPath, g.Repo), acceptJSON)
	if err != nil {
		return Release{}, err
	}
	var raw struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Release{}, err
	}
	r := Release{Tag: raw.TagName}
	for _, a := range raw.Assets {
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL})
	}
	return r, nil
}

// Download returns an asset's bytes. GitHub redirects to its file storage; the
// HTTP client drops the Authorization header on that cross-host redirect.
func (g GitHub) Download(a Asset) ([]byte, error) { return g.get(a.URL, acceptBinary) }

func (g GitHub) api() string {
	if g.API != "" {
		return g.API
	}
	return defaultAPI
}

func (g GitHub) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

func (g GitHub) get(url, accept string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(headerAccept, accept)
	if g.Token != "" {
		req.Header.Set(headerAuth, bearerPrefix+g.Token)
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w %d for %s", ErrGitHubStatus, resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
```

### `check.go`

```go
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

// Checker finds a newer version whose manifest the publisher signed.
// Example: m, ok, err := selfupdate.Checker{Releases: gh, PublicKey: pub}.Check(state)
type Checker struct {
	Releases  Releases
	PublicKey []byte // the publisher's key, from update.pub
}

// Check returns the latest version when it is newer than the running one, was
// never marked as failed, and its manifest verifies; ok is false when there is
// nothing to install. With no running version (first install) any release is newer.
func (c Checker) Check(s State) (m update.Manifest, ok bool, err error) {
	r, err := c.Releases.Latest()
	if err != nil {
		return update.Manifest{}, false, err
	}
	newer := s.Version == "" || update.IsOutdated(s.Version, r.Tag)
	if !newer || s.HasFailed(r.Tag) {
		return update.Manifest{}, false, nil
	}
	text, err := c.asset(r, update.ManifestAsset)
	if err != nil {
		return update.Manifest{}, false, err
	}
	sig, err := c.asset(r, update.SignatureAsset)
	if err != nil {
		return update.Manifest{}, false, err
	}
	m, err = update.VerifyManifest(text, sig, c.PublicKey)
	if err != nil {
		return update.Manifest{}, false, err
	}
	if m.Version != r.Tag {
		return update.Manifest{}, false, fmt.Errorf("%w: manifest %s, tag %s", ErrVersionMismatch, m.Version, r.Tag)
	}
	return m, true, nil
}

func (c Checker) asset(r Release, name string) ([]byte, error) {
	for _, a := range r.Assets {
		if a.Name == name {
			return c.Releases.Download(a)
		}
	}
	return nil, fmt.Errorf("%w %s: %s", ErrNoAsset, r.Tag, name)
}
```

Run `gotest`: all green. Commit after this stage.

## Stage 3 — docs

`README.md`: replace the one-line description with: what the supervisor is (one paragraph, from
`docs/ARCHITECTURE.md` «What this is»), a link to `docs/ARCHITECTURE.md` and the two diagrams, and a short example of
`Checker{Releases: GitHub{...}, PublicKey: pub}.Check(state)`. Commit.

## Delivery check

1. `gotest` passes. A PR that does not compile, or whose tests could not run, is a failed delivery.
2. `test ! -e selfupdate.go` and `grep -rn "func New()" *.go` → empty.
3. `git diff --stat main` lists only `layout.go`, `state.go`, `github.go`, `check.go`, `selfupdate.go` (deleted),
   `tests/github_test.go`, `tests/state_test.go`, `tests/check_test.go`, `README.md`, `go.mod`, `go.sum`.

## Delivery rules

- Never pause to ask permission or report progress: when the delivery check passes, commit, push and open the PR. Stuck
  for 30 minutes: open the PR anyway, listing what fails.
- Commit after every stage. Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own work.
- No test may reach the network, GitHub or Docker.

## Stages

| # | Files | Result |
|---|---|---|
| 1 | `tests/github_test.go`, `tests/state_test.go`, `tests/check_test.go` | Every branch of the check flow, red |
| 2 | `layout.go`, `state.go`, `github.go`, `check.go`; delete `selfupdate.go` | Green |
| 3 | `README.md` | Documented |
