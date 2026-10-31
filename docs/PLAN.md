---
PLAN: "feat!: every version brings its signed compose.yaml — verified on check, installed on apply, restored on failure"
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 14039643631457085738
PR: https://github.com/webtyp/selfupdate/pull/5
REVIEWED_COMMIT: 86bd82efdfe79b6ca0a6532eb810b119f93921d9
---

> Read AGENTS.md at the repo root before any change. Design: `docs/ARCHITECTURE.md`; flows: `docs/diagrams/`.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# The compose file travels with each version

## Context

Each Release carries `compose.yaml` next to the image, and `webtyp.com/update` v0.0.6 signs its digest inside the
manifest (`Manifest.ComposeDigest`, `update.Digest`, `update.CheckCompose`, `update.ComposeAsset`). Today the
supervisor verifies only the image and keeps whatever compose the server has. This plan makes the compose part of the
version: `Check` downloads `compose.yaml` and refuses it when it does not match the signed digest; `Apply` installs it
right after pulling the image (before migrating, because the `migrate` service is defined there) and puts the previous
one back whenever it returns to the previous version.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go get webtyp.com/update@v0.0.6
go mod tidy
```

## APIs used

| Symbol | Exact signature | Tag |
|---|---|---|
| `update.Manifest` | `struct { Version, Image, Digest, ComposeDigest string }`; `SignManifest` now requires a valid `ComposeDigest` | `webtyp.com/update` v0.0.6 |
| `update.Digest` | `func Digest(data []byte) string` — `"sha256:" + hex` | v0.0.6 |
| `(Manifest).CheckCompose` | `func (m Manifest) CheckCompose(data []byte) error` — error text `update.ErrComposeMismatch` | v0.0.6 |
| `update.ComposeAsset` | `"compose.yaml"` | v0.0.6 |

If a signature differs, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 1 — diagrams

- `docs/diagrams/CHECK_FLOW.md`: after «Manifest version equals the tag?» (yes), add
  `G -->|yes| C{compose.yaml in the Release<br/>and equal to the signed digest?}`, `C -->|no asset| X2` (reuse ErrNoAsset),
  `C -->|different| X5[ErrComposeMismatch]`, `C -->|yes| H[Available: image and compose]`.
- `docs/diagrams/APPLY_FLOW.md`: after «Image pulled at its digest?» (yes) add «Keep the current compose.yaml as
  compose.yaml.prev, write the new one»; every arrow that goes back to the previous version (migration failed,
  unhealthy) first «Restore compose.yaml from compose.yaml.prev».

Commit.

## Stage 2 — tests first

Shared fixture: `testCompose := []byte("name: shop\n")`; every `update.Manifest{…}` in `tests/` gains
`ComposeDigest: update.Digest(testCompose)` and every `fakeReleases` that serves a release also serves
`update.ComposeAsset: testCompose`.

- `tests/check_test.go`: `Check` now returns `(selfupdate.Update, bool, error)`; existing assertions read `u.Manifest`.
  Add `TestCheck_ComposeMissing` (no `compose.yaml` asset → `errors.Is(err, ErrNoAsset)`, message names `compose.yaml`),
  `TestCheck_ComposeAltered` (asset `name: evil\n` → `err.Error() == update.ErrComposeMismatch`), and in
  `TestCheck_Available` assert `string(u.Compose) == "name: shop\n"`.
- `tests/apply_test.go`: `Apply(state, selfupdate.Update{Manifest: m, Compose: []byte("name: new\n")})`; `dir/compose.yaml`
  starts as `name: old\n`. Add assertions: after `TestApply_Healthy`, `compose.yaml` is `name: new\n` and
  `compose.yaml.prev` is `name: old\n`; after `TestApply_Unhealthy` and `TestApply_MigrateFails`, `compose.yaml` is back
  to `name: old\n`; `TestApply_PullFails` and `TestApply_BackupFails` leave `compose.yaml` untouched; `TestApply_FirstInstallUnhealthy`
  (no `compose.yaml` before): the new one stays (there is nothing to restore) — assert it exists.
- `tests/supervisor_test.go` and `tests/handler_test.go`: only the fixture changes above.

Commit (red).

## Stage 3 — code

### `check.go`

```go
// Update is a verified version ready to apply: its signed manifest and the
// compose file whose digest the manifest carries.
type Update struct {
	Manifest update.Manifest
	Compose  []byte
}
```

`Check(s State) (u Update, ok bool, err error)`: after the version check (`m.Version != r.Tag`), add

```go
	compose, err := c.asset(r, update.ComposeAsset)
	if err != nil {
		return Update{}, false, err
	}
	if err := m.CheckCompose(compose); err != nil {
		return Update{}, false, err
	}
	return Update{Manifest: m, Compose: compose}, true, nil
```

and every other `return update.Manifest{}, false, …` becomes `return Update{}, false, …`. Update its comment.

### `apply.go`

`Apply(s State, u Update) (State, error)`; `m := u.Manifest` at the top. Add the constant `composePrevSuffix = ".prev"`
and two helpers:

```go
// installCompose keeps the running compose as .prev and writes the new one (owner-only, atomic).
func (a Applier) installCompose(data []byte) error {
	path := filepath.Join(a.Dir, FileCompose)
	if old, err := os.ReadFile(path); err == nil {
		if err := writeAtomic(path+composePrevSuffix, old); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomic(path, data)
}

// restoreCompose puts the previous compose back; with no .prev (first install) there is nothing to restore.
func (a Applier) restoreCompose() error {
	path := filepath.Join(a.Dir, FileCompose)
	old, err := os.ReadFile(path + composePrevSuffix)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return writeAtomic(path, old)
}

func writeAtomic(path string, data []byte) error {
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, data, stateFileMode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

In `Apply`: right after a successful `Pull`, `if err := a.installCompose(u.Compose); err != nil { return s, err }`.
In the migration-failure branch and in the unhealthy branch, call `restoreCompose()` **before** switching back (and
before returning in the migration branch); join its error into the returned error like the others
(`errors.Join(result, err)`). `setEnv` in `compose.go` may now reuse `writeAtomic`.

### `supervisor.go`

`available *update.Manifest` becomes `available *Update`; `Status.Available` reads `s.available.Manifest.Version`;
`Apply` passes `*s.available` to `s.applier.Apply`.

Run `gotest`: green. Commit.

## Stage 4 — docs

`docs/ARCHITECTURE.md` «Checking for a version» and «Switching»: the compose is verified against the signed digest and
installed with the image; the previous one comes back with the previous image. `README.md`: the `Check`/`Apply` example
uses `Update`. Commit.

## Delivery check

1. `gotest` passes. A PR that does not compile, or whose tests could not run, is a failed delivery.
2. `grep -rn "update.Manifest, ok bool\|s State, m update.Manifest" --include=*.go .` → empty.
3. `git diff --stat main` lists only `check.go`, `apply.go`, `compose.go`, `supervisor.go`, `tests/*.go`,
   `docs/diagrams/CHECK_FLOW.md`, `docs/diagrams/APPLY_FLOW.md`, `docs/ARCHITECTURE.md`, `README.md`, `go.mod`, `go.sum`.

## Delivery rules

- Commit after every stage. Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own work.
- Stuck for 30 minutes: open the PR anyway, listing what fails.
- No test reaches the network or Docker.

## Stages

| # | Files | Result |
|---|---|---|
| 1 | `docs/diagrams/CHECK_FLOW.md`, `docs/diagrams/APPLY_FLOW.md` | The compose in both flows |
| 2 | `tests/*.go` | Compose verified, installed, restored; red |
| 3 | `check.go`, `apply.go`, `compose.go`, `supervisor.go` | `Update`; green |
| 4 | `docs/ARCHITECTURE.md`, `README.md` | Documented |
