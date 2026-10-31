---
PLAN: "feat: selfupdate.NewTarget implements deploy.Target"
TAG: v0.0.11
EXECUTOR: jules
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

## Tools

| Tool | Install |
|---|---|
| `gotest` | `go install webtyp.com/devflow/cmd/gotest@latest` |

## Ecosystem search

1. Existing code: `webtyp.com/selfupdate` exports constant `Server deploy.TargetID = "webtyp.com/selfupdate.Server"` in `target.go`.
2. Sibling prior art: `webtyp.com/goflare` exports `NewTarget(cfg TargetConfig) (*Target, error)` in `goflare/target_native.go`, implementing `deploy.Target`.
3. Decision: **extend** `webtyp.com/selfupdate` with `Target` and `NewTarget(cfg TargetConfig) (*Target, error)`.

## Design gate

1. **Prior art:**
   - [`webtyp.com/goflare.NewTarget`](https://github.com/webtyp/goflare): implements `deploy.Target` for Cloudflare Workers.
   - [Fly.io target / Kamal](https://kamal-deploy.org/): targets a self-hosted Linux server running Docker.
2. **Novice-name test:** `selfupdate.NewTarget(cfg)` is identical to `goflare.NewTarget(cfg)`. A developer recognizes the constructor immediately.
3. **Complexity ledger:** Zero new dependencies (`webtyp.com/deploy` and `webtyp.com/lang` already imported).
4. **Where it belongs:** `webtyp.com/selfupdate` in `target.go`.
5. **What it deletes:** Eliminates the missing target error in `webtyp/app` when a self-hosted project declares `Target: selfupdate.Server`.

## Stages

### Stage 1: Implement `deploy.Target` in `target.go`

In `target.go`:
- Define `TargetConfig`:
  ```go
  type TargetConfig struct {
      RootDir string                     // project root directory
      Probe   func(url string) (int, error) // optional health probe seam; nil = default HTTPS GET
      Store   deploy.Destination         // optional runtime secret destination
  }
  ```
- Define `Target` struct implementing `deploy.Target`:
  - `ID() deploy.TargetID`: returns `Server`.
  - `Check(p deploy.Plan, ci deploy.Destination) ([]deploy.Missing, error)`:
    1. Validates that `p.Target == Server`, and validates `p.Name` and `p.Domain` using `validName(p.Name)` and `validDomain(p.Domain)`.
    2. Checks that `web/server/main.go` exists in `RootDir` (via `os.Stat(filepath.Join(t.rootDir, "web", "server", "main.go"))`). If missing, returns error `fmt.Errorf("selfupdate: missing web/server/main.go in %s", t.rootDir)`.
    3. Health probe check: probes `https://` + `p.Domain` + `/health` (or uses `t.probe`). If status is `http.StatusOK` (200), server is installed and running; otherwise, appends a `deploy.Missing` item:
       - `Item: lang.Text("Server installation")`
       - `Why: lang.Text("The application server is not responding at https://" + p.Domain + "/health")`
       - `Fix: deploy.FixManual`
       - `Steps: []lang.Text{`
         `"Generate publisher key: gorelease key",`
         `"SSH into the host server",`
         `"Run: sudo selfupdate install <owner/repo>",`
         `"Enter the public key and tokens when prompted",`
       `}`
    4. Token guides: for each secret in `p.Secrets` where `To` contains `deploy.ToRuntime`:
       If `t.store != nil`, checks `has, err := t.store.Has(s.Key)`. If not present, appends `deploy.Missing`:
       - `Item: s.What`
       - `Why: lang.Text("Required by the server environment")`
       - `Fix: deploy.FixInput`
       - `Secret: true`
       - `Steps: s.Guide`
       - `Accept: func(val string) error { return t.store.Put(s.Key, val) }`
    5. Returns `missing, nil`.
  - `Provision(p deploy.Plan, ci deploy.Destination) error`: returns nil (idempotent; server provisioning happens via `selfupdate install`).
  - `Rotate(p deploy.Plan, ci deploy.Destination) error`: returns nil.
  - `Runtime(p deploy.Plan) deploy.Destination`: returns `t.store` (or nil if empty).
  - `Job(p deploy.Plan) deploy.Job`: returns empty `deploy.Job{}` (local release via gorelease, no remote CI job).

Verbatim `target.go`:

```go
package selfupdate

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"webtyp.com/deploy"
	"webtyp.com/lang"
)

// Server is the deploy target of a self-hosted app: a server you own running
// Docker Compose, kept up to date by this supervisor.
// Example (config/deploy.go): return deploy.Plan{Name: "shop", Domain: "app.example.com", Target: selfupdate.Server}
const Server deploy.TargetID = "webtyp.com/selfupdate.Server"

var (
	ErrNoRootDir = errors.New("selfupdate: RootDir is required")
	ErrNoServer  = errors.New("selfupdate: web/server/main.go is missing")
)

const (
	healthTimeoutSec = 3
	defaultHealthPath = "/health"
)

// TargetConfig configures Target.
type TargetConfig struct {
	RootDir string
	Probe   func(url string) (int, error) // nil = HTTPS GET with TLS verification
	Store   deploy.Destination            // runtime secrets store
}

// Target implements deploy.Target for a self-hosted server running Docker Compose.
type Target struct {
	rootDir string
	probe   func(url string) (int, error)
	store   deploy.Destination
}

var _ deploy.Target = (*Target)(nil)

// NewTarget returns a Target for a self-hosted application.
// Example: target, err := selfupdate.NewTarget(selfupdate.TargetConfig{RootDir: "."})
func NewTarget(cfg TargetConfig) (*Target, error) {
	if cfg.RootDir == "" {
		return nil, ErrNoRootDir
	}
	probe := cfg.Probe
	if probe == nil {
		probe = defaultProbe
	}
	return &Target{
		rootDir: cfg.RootDir,
		probe:   probe,
		store:   cfg.Store,
	}, nil
}

func (t *Target) ID() deploy.TargetID { return Server }

func (t *Target) Check(p deploy.Plan, ci deploy.Destination) ([]deploy.Missing, error) {
	if !validName(p.Name) {
		return nil, ErrAppName
	}
	if !validDomain(p.Domain) {
		return nil, ErrAppDomain
	}

	serverPath := filepath.Join(t.rootDir, "web", "server", "main.go")
	if _, err := os.Stat(serverPath); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoServer, serverPath)
	}

	var missing []deploy.Missing

	healthURL := "https://" + p.Domain + defaultHealthPath
	statusCode, _ := t.probe(healthURL)
	if statusCode != http.StatusOK {
		missing = append(missing, deploy.Missing{
			Item: lang.Text("Server installation"),
			Why:  lang.Text(fmt.Sprintf("The server is not responding at %s", healthURL)),
			Fix:  deploy.FixManual,
			Steps: []lang.Text{
				lang.Text("Export publisher key: gorelease key"),
				lang.Text(fmt.Sprintf("SSH into the server and run: sudo selfupdate install %s", p.Name)),
				lang.Text("Provide PUBLIC_KEY, GITHUB_TOKEN, TUNNEL_TOKEN, and DNS_TOKEN when prompted"),
			},
		})
	}

	if t.store != nil {
		for _, s := range p.Secrets {
			isRuntime := false
			for _, pl := range s.To {
				if pl == deploy.ToRuntime {
					isRuntime = true
					break
				}
			}
			if !isRuntime {
				continue
			}
			has, err := t.store.Has(s.Key)
			if err != nil {
				return nil, err
			}
			if !has {
				key := s.Key
				missing = append(missing, deploy.Missing{
					Item:   s.What,
					Why:    lang.Text("Required on the server at runtime"),
					Steps:  s.Guide,
					Fix:    deploy.FixInput,
					Secret: true,
					Accept: func(val string) error { return t.store.Put(key, val) },
				})
			}
		}
	}

	return missing, nil
}

func (t *Target) Provision(p deploy.Plan, ci deploy.Destination) error { return nil }

func (t *Target) Rotate(p deploy.Plan, ci deploy.Destination) error { return nil }

func (t *Target) Runtime(p deploy.Plan) deploy.Destination { return t.store }

func (t *Target) Job(p deploy.Plan) deploy.Job { return deploy.Job{} }

func defaultProbe(url string) (int, error) {
	client := &http.Client{
		Timeout: healthTimeoutSec * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
```

### Stage 2: Tests in `tests/target_test.go`

Create `tests/target_test.go` verifying:
1. `TestTarget_Validation`: Invalid rootDir, invalid Plan Name, invalid Domain, missing `web/server/main.go`.
2. `TestTarget_Check_Unhealthy`: Probe returns != 200 -> reports `Server installation` missing item with FixManual.
3. `TestTarget_Check_Healthy`: Probe returns 200 -> no `Server installation` missing item.
4. `TestTarget_Check_Secrets`: When Store is provided, absent `ToRuntime` secrets are returned with `FixInput` and their `Accept` puts the value into `Store`.

Verbatim test code:

```go
package tests

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"webtyp.com/deploy"
	"webtyp.com/lang"
	"webtyp.com/selfupdate"
)

type mapStore map[string]string

func (m mapStore) Has(key string) (bool, error) {
	_, ok := m[key]
	return ok, nil
}

func (m mapStore) Put(key, value string) error {
	m[key] = value
	return nil
}

func TestTarget_Validation(t *testing.T) {
	_, err := selfupdate.NewTarget(selfupdate.TargetConfig{RootDir: ""})
	if err == nil {
		t.Fatal("expected error on empty RootDir")
	}

	tmp := t.TempDir()
	tgt, err := selfupdate.NewTarget(selfupdate.TargetConfig{RootDir: tmp})
	if err != nil {
		t.Fatal(err)
	}

	plan := deploy.Plan{Name: "INVALID NAME", Domain: "app.example.com", Target: selfupdate.Server}
	if _, err := tgt.Check(plan, nil); err == nil {
		t.Fatal("expected ErrAppName")
	}

	plan = deploy.Plan{Name: "shop", Domain: "invalid", Target: selfupdate.Server}
	if _, err := tgt.Check(plan, nil); err == nil {
		t.Fatal("expected ErrAppDomain")
	}

	plan = deploy.Plan{Name: "shop", Domain: "app.example.com", Target: selfupdate.Server}
	if _, err := tgt.Check(plan, nil); err == nil {
		t.Fatal("expected error for missing web/server/main.go")
	}
}

func TestTarget_Check_HealthyAndUnhealthy(t *testing.T) {
	tmp := t.TempDir()
	serverDir := filepath.Join(tmp, "web", "server")
	if err := os.MkdirAll(serverDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serverDir, "main.go"), []byte("package main\nfunc main(){}"), 0644); err != nil {
		t.Fatal(err)
	}

	plan := deploy.Plan{
		Name:   "shop",
		Domain: "app.example.com",
		Target: selfupdate.Server,
	}

	// 1. Unhealthy probe
	tgtUnhealthy, _ := selfupdate.NewTarget(selfupdate.TargetConfig{
		RootDir: tmp,
		Probe: func(url string) (int, error) {
			return http.StatusBadGateway, nil
		},
	})
	missing, err := tgtUnhealthy.Check(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].Fix != deploy.FixManual {
		t.Fatalf("expected 1 manual missing item, got %+v", missing)
	}

	// 2. Healthy probe
	tgtHealthy, _ := selfupdate.NewTarget(selfupdate.TargetConfig{
		RootDir: tmp,
		Probe: func(url string) (int, error) {
			return http.StatusOK, nil
		},
	})
	missing, err = tgtHealthy.Check(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected 0 missing items, got %+v", missing)
	}
}

func TestTarget_Check_Secrets(t *testing.T) {
	tmp := t.TempDir()
	serverDir := filepath.Join(tmp, "web", "server")
	if err := os.MkdirAll(serverDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serverDir, "main.go"), []byte("package main\nfunc main(){}"), 0644); err != nil {
		t.Fatal(err)
	}

	store := make(mapStore)
	tgt, _ := selfupdate.NewTarget(selfupdate.TargetConfig{
		RootDir: tmp,
		Probe:   func(url string) (int, error) { return http.StatusOK, nil },
		Store:   store,
	})

	plan := deploy.Plan{
		Name:   "shop",
		Domain: "app.example.com",
		Target: selfupdate.Server,
		Secrets: []deploy.Secret{
			{
				Key:    "TUNNEL_TOKEN",
				What:   lang.Text("Cloudflare Tunnel token"),
				Source: deploy.Typed,
				Guide:  []lang.Text{lang.Text("https://one.dash.cloudflare.com/")},
				To:     []deploy.Place{deploy.ToRuntime},
			},
		},
	}

	missing, err := tgt.Check(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].Fix != deploy.FixInput {
		t.Fatalf("expected 1 input missing item, got %+v", missing)
	}

	// Accept typed value
	if err := missing[0].Accept("secret123"); err != nil {
		t.Fatal(err)
	}
	if store["TUNNEL_TOKEN"] != "secret123" {
		t.Fatalf("store token mismatch: %s", store["TUNNEL_TOKEN"])
	}

	// Check again: should be resolved
	missing, err = tgt.Check(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected 0 missing items after accept, got %+v", missing)
	}
}
```

### Stage 3: Documentation in `README.md` and `docs/ARCHITECTURE.md`

Update `README.md` and `docs/ARCHITECTURE.md` to document `selfupdate.NewTarget` and `selfupdate.TargetConfig`. Keep both files conforming to the documentation standard (concise, ≤ 150 lines for README.md).

## Delivery check

1. `gotest` passes cleanly:
   ```bash
   GOPRIVATE=webtyp.com/*,github.com/veltylabs/* GOFLAGS=-mod=mod gotest
   ```
2. Commit and open PR.
