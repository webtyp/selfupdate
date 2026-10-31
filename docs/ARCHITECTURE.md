# selfupdate — architecture

## What this is

`selfupdate` is the **supervisor** of a self-hosted webtyp app: a small container that runs next to the app in its
Docker Compose and is the **only** one allowed to talk to Docker. It finds out whether a newer version was published,
checks that the publisher really signed it, switches the app to the new container image and goes back to the previous
one when the new image does not come up healthy.

**When you run into it:** you install a webtyp app on a server you own (one command, `selfupdate install`), or the
app's «Updates» screen tells you a new version is waiting. Why every self-hosted app works this way:
[SELFHOST_DOCKER_DESIGN.md](https://github.com/webtyp/docs/blob/main/SELFHOST_DOCKER_DESIGN.md).

See [Install flow](diagrams/INSTALL_FLOW.md) and [Serve flow](diagrams/SERVE_FLOW.md).

**Who decides what.** The **app** decides *when*: it knows whether someone is using it, so it never interrupts a person
in the middle of their work. The **supervisor** decides *how*: it verifies, pulls, switches and rolls back. The app
never touches Docker — access to the Docker socket equals root on the machine.

## The server, on disk

Everything of one app lives in `/opt/<app>/`, readable only by root (`<app>` is `App.Name`):

| Path | What it is | Written by |
|---|---|---|
| `compose.yaml` | The services below; `app` uses `image: ${APP_IMAGE}` | the Release (written by `ComposeFile`, uploaded by `gorelease`), copied at install and at each update |
| `.env` | `APP_IMAGE=<image>@sha256:<digest>`, `SUPERVISOR_IMAGE`, `LAN_ADDRESS` — no secrets | install; `APP_IMAGE` by the supervisor on every switch |
| `update.pub` | The publisher's public key: the only thing that decides which versions are genuine | install (the operator pastes it; it is not secret) |
| `secrets/` | One file per secret, mode 0600: database passwords, tunnel token, DNS token, GitHub read token, supervisor token | install; generated on the server or typed once, never shown again |
| `state.json` | The running version, the previous one and the versions that failed (never retried) | the supervisor |
| `backups/` | A database dump taken before every switch (the last 7 are kept) | the supervisor |

## The services

`db` (PostgreSQL, data in a named volume, no host port, a health check that `migrate` and `app` wait for) · `migrate` (run only by the supervisor) · `app` · `cloudflared` (the tunnel, pinned in `TunnelImage`) · `selfupdate` (this supervisor, `SUPERVISOR_IMAGE` from `.env`: the image install ran; an app update never changes it; mounts `/var/run/docker.sock` and `/opt/<app>` at the same path, so the relative paths of the compose resolve the same inside and outside).

## Talking to the supervisor — [diagram](diagrams/PHASES.md)

Only the app talks to it, over the Compose internal network, with `Authorization: Bearer <secrets/supervisor_token>`:

| Request | Answer |
|---|---|
| `GET /status` | running version, available version (if any), phase (`idle`, `checking`, `available`, `applying`, `rolled_back`), last message (200, 401 without the token, 405, 404) |
| `POST /check` | looks for a newer verified version; answers the new status (200, 409 busy, 502 when the check fails) |
| `POST /apply` | starts switching to the available version; answers at once, progress is read with `GET /status` (202 for apply, 409 busy or nothing to apply) |

## Checking for a version — [diagram](diagrams/CHECK_FLOW.md)

The latest GitHub Release of the app's repository (read with the GitHub read token) must be newer than the running
version, must not be marked as failed, and must carry `manifest.txt` and `manifest.sig` (`webtyp.com/update`). The
signature is verified with `update.pub` **before** the manifest is read, and the manifest must name the same version
as the Release tag — a manifest signed for another version is refused. The compose file is also verified against the signed digest.

## Switching — [diagram](diagrams/APPLY_FLOW.md)

Dump the database → pull `image@digest` → install the verified `compose.yaml` (keeping the old one as `.prev`) → **migrate (owner user, the new image's `migrate` service)** → write `APP_IMAGE` in `.env` → recreate only the `app` service → wait for
`/health`. The app listens on `AppPort` (8443, office door) and `AppInternetPort` (8444, internet door) inside its
container, and the health probe checks the certificate against `APP_DOMAIN`. Healthy: the new version is current.
Not healthy within the limit: put the previous `compose.yaml` back, write the previous `APP_IMAGE` back, recreate `app`, and mark the version as failed.
The supervisor migrates; the app runs with the user that only reads and writes rows.

## Daily backup

`Supervisor.Backup()` writes the same dump as the copy before a switch, into `backups/` with the running version in its name, and keeps the newest 7 of both kinds together. While it runs the phase is `backing_up` and a check or an apply answers busy. The `serve` command calls it once a day.

## The compose file — contract

`selfupdate.ComposeFile(selfupdate.App{Name, Domain, Repo})` writes it ([diagram](diagrams/COMPOSE_FILE.md)); `gorelease` calls it with `Name` and `Domain` from the app's `config/deploy.go`, and the supervisor installs it. Its digest is in the signed manifest, so a Release whose compose was altered is refused like an altered image. `<app>` is `App.Name` (`<db>` is the same name with `-` as `_`, because Postgres names have no dashes), `<domain>` is `App.Domain`; every other name comes from `layout.go`. Secrets are files (`secrets/`, mode 0600) handed to containers as `/run/secrets/<name>`; programs read them with the `<KEY>_FILE` convention (`webtyp/env`, lego, cloudflared).

The exact file, for an app called `acme-shop`: [tests/testdata/compose.golden.yaml](../tests/testdata/compose.golden.yaml).

The internet door (8444) is published nowhere: only `cloudflared`, on the Compose network, reaches `app:8444`.

### Who reads each secret file

| File in `secrets/` | Read by | Owner, mode |
|---|---|---|
| `db_owner_password` | `db` entrypoint (as root) | root, 0400 |
| `initdb.sql` | `db` init scripts (as `postgres`, uid 999 = `PostgresUID`) | 999, 0400 |
| `database_url`, `database_owner_url`, `dns_token`, `supervisor_token` | `app` and `migrate` (uid 65532 = `NonRootUID`) | 65532, 0400 |
| `tunnel_token` | `cloudflared` (uid 65532) | 65532, 0400 |
| `github_token` | the supervisor (root), never mounted in another container | root, 0400 |

Docker Compose (outside Swarm) mounts a secret file with its owner and mode on the host, so `install` sets them; a file the container's user cannot read stops that container at start.

**The app image** (built by `gorelease`): `/app/server` (entrypoint), `/app/migrate`, `/app/web/public`, working
directory `/app` (so `web/public` and `certs/` resolve there), a non-root user, `linux/amd64`.

The deploy target: an app's `config/deploy.go` declares `Target: selfupdate.Server`.
