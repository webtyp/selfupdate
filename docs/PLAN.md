---
PLAN: "feat!: migrate the new version before switching (owner user), Command for docker runs"
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 8441968537191518885
PR: https://github.com/webtyp/selfupdate/pull/4
REVIEWED_COMMIT: 4ba082375598db6e00fd5d24325e1d6a6619cd6d
---

> Read AGENTS.md at the repo root before any change. Design: `docs/ARCHITECTURE.md`; flow: `docs/diagrams/APPLY_FLOW.md`.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# Migrate before switching

## Context

Decision D23 (SELFHOST): the app runs with a database user that only reads and writes rows, so it cannot add columns.
The **supervisor** migrates: after pulling the new image and before switching, it runs that image's migration program
with the owner user, as the compose service `migrate` (`docker compose run --rm --no-deps migrate`). If the migration
fails, nothing is switched and the version is marked as failed (never retried). Migrations only add (`ddl.Sync`), so the
version still running keeps working on the migrated database.

The compose file says `image: ${APP_IMAGE}` for both `app` and `migrate`, and `.env` still names the running image.
Docker Compose gives a variable from the process environment priority over `.env` (Docker Compose docs, «Ways to set
variables»; unverified: not read in this session), so the migration runs the new image by setting `APP_IMAGE=<new>` in
the environment of that one command only. To pass that environment, the injectable runner changes from positional
arguments to a `Command` value. There are no consumers of `Compose.Run` outside this repo yet.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
```

Environment: `GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`.

## Stage 1 — diagram

`docs/diagrams/APPLY_FLOW.md`: between «Image pulled at its digest?» (yes) and «Write APP_IMAGE in .env, recreate the
app service», add:

```
    C -->|yes| M{New version's migration<br/>ran with the owner user?}
    M -->|no| X3[Stop: nothing switched<br/>version marked as failed]
    M -->|yes| D[Write APP_IMAGE in .env<br/>recreate the app service]
```

(remove the old `C -->|yes| D[...]` edge). In `docs/ARCHITECTURE.md` «Switching», the sequence becomes
«Dump → pull → **migrate (owner user, the new image's `migrate` service)** → write `APP_IMAGE` → recreate `app` → wait
for `/health`», and the paragraph about the app migrating at start is replaced by: the supervisor migrates; the app runs
with the user that only reads and writes rows. Commit.

## Stage 2 — tests first

- `tests/apply_test.go`: `fakeEngine` gains `migrateErr error` and
  `func (e *fakeEngine) Migrate(ref string) error { e.calls = append(e.calls, "migrate "+ref); return e.migrateErr }`.
  `TestApply_Healthy` now expects the calls `backup, pull <new>, migrate <new>, switch <new>, healthy`. Add
  `TestApply_MigrateFails`: `migrateErr` set → `errors.Is(err, selfupdate.ErrMigrate)` and the fake error is inside
  it; no `switch` call; saved state keeps `Version v1.1.0`, `Image <old>`, and `Failed` contains `v1.2.0`.
- `tests/compose_test.go`: `fakeRun` becomes `func(c selfupdate.Command) error` recording `c.Dir`,
  `c.Name+" "+strings.Join(c.Args, " ")` and `c.Env`, and writing `"dump"` to `c.Stdout` when it is not nil; update
  every `Compose{Run: …}` in the file to the new type. Add `TestCompose_Migrate`: `c.Migrate(ref)` runs
  `docker compose run --rm --no-deps migrate` in `dir` with `c.Env == []string{"APP_IMAGE=" + ref}`, and `.env` is
  **not** modified.
- `tests/supervisor_test.go`, `tests/handler_test.go`: `fakeEngine` already used there gets `Migrate` from the shared
  type; no other change.

Commit (red).

## Stage 3 — code

### `layout.go`

Add `ServiceMigrate = "migrate"` next to the other services, with the comment: the one-off service that runs the app
image's migration program with the owner user.

### `apply.go`

- `Engine` gains `Migrate(ref string) error // run the migration program of image ref with the owner user`.
- Add `ErrMigrate = errors.New("selfupdate: the new version's migration failed; nothing was switched")`.
- In `Apply`, right after a successful `Pull` and before `Switch`:

```go
	if err := a.Engine.Migrate(ref); err != nil {
		failed := s
		failed.Failed = append(append([]string(nil), s.Failed...), m.Version)
		result := fmt.Errorf("%w: %v", ErrMigrate, err)
		if saveErr := failed.Save(a.statePath()); saveErr != nil {
			return failed, errors.Join(result, saveErr)
		}
		return failed, errors.Join(result, err)
	}
```

  (`errors.Join(result, err)` keeps both `errors.Is(err, ErrMigrate)` and the engine's own error reachable.)

### `compose.go`

Replace the `Run` field and `run` helper:

```go
// Command is one docker invocation the supervisor makes.
type Command struct {
	Dir    string
	Env    []string  // extra KEY=VALUE pairs on top of the process environment
	Stdout io.Writer // nil = discarded
	Name   string
	Args   []string
}
```

- `Compose.Run` becomes `Run func(c Command) error // nil = os/exec`.
- `run(stdout io.Writer, env []string, name string, args ...string) error` builds a `Command{Dir: c.Dir, Env: env, Stdout: stdout, Name: name, Args: args}`
  and calls `c.Run` when set; otherwise `exec.Command` with `cmd.Dir = c.Dir`, `cmd.Env = append(os.Environ(), env...)`,
  `cmd.Stdout = stdout` (or `io.Discard` when nil) and the same stderr-in-the-error handling as today.
- `Backup`, `Pull` and `Switch` pass `nil` env. Add:

```go
const argRun = "run"
const argRemove = "--rm"

// Migrate runs the migration program of image ref (the compose service "migrate", owner user)
// without touching .env: APP_IMAGE in the process environment wins over .env for this one command.
func (c Compose) Migrate(ref string) error {
	return c.run(io.Discard, []string{EnvAppImage + envSeparator + ref}, cmdDocker, argCompose, argRun, argRemove, argNoDeps, ServiceMigrate)
}
```

Run `gotest`: green. Commit.

## Stage 4 — README

The example of `Applier`/`Compose` stays valid; add one line: the compose must declare a `migrate` service with
`image: ${APP_IMAGE}` that runs the migration with the owner user (generated by `webtyp/deploy`). Commit.

## Delivery check

1. `gotest` passes. A PR that does not compile, or whose tests could not run, is a failed delivery.
2. `grep -n "func(dir string, stdout io.Writer" --include=*.go -r .` → empty (old runner signature gone).
3. `git diff --stat main` lists only `layout.go`, `apply.go`, `compose.go`, `tests/apply_test.go`,
   `tests/compose_test.go`, `docs/diagrams/APPLY_FLOW.md`, `docs/ARCHITECTURE.md`, `README.md`.

## Delivery rules

- Commit after every stage. Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own work.
- Stuck for 30 minutes: open the PR anyway, listing what fails.
- No test runs `docker` or reaches the network.

## Stages

| # | Files | Result |
|---|---|---|
| 1 | `docs/diagrams/APPLY_FLOW.md`, `docs/ARCHITECTURE.md` | The migrate step in the flow |
| 2 | `tests/apply_test.go`, `tests/compose_test.go` | Order with migrate, migrate fails, docker command and env; red |
| 3 | `layout.go`, `apply.go`, `compose.go` | `Migrate`, `ErrMigrate`, `Command`; green |
| 4 | `README.md` | Documented |
