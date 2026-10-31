# selfupdate
<img src="docs/img/badges.svg">

## Why This Exists

`selfupdate` is the Go library and CLI that installs and updates a self-hosted [WebTyp](https://github.com/webtyp) application on a Linux server running [Docker Compose](https://docs.docker.com/compose/). You interact with it in two moments: once when provisioning a server (`selfupdate install`), and continuously inside the [supervisor container](docs/ARCHITECTURE.md#1-overview-in-one-minute) (`selfupdate serve`), which is the **only** container in the stack allowed to talk to the Docker daemon (`/var/run/docker.sock`).

- **Without `selfupdate`:** Updating an application on an office or clinic server requires someone to SSH in and run `docker compose pull && docker compose up -d` (or use an unverified poller like Watchtower). If a user is saving a form at that moment, their session is cut off; if the `:latest` tag was overwritten with a broken build or a failing migration, the server stays down until an administrator manually restores a backup.
- **With `selfupdate`:**
  1. **Signed releases and immutable digests:** Every release published with [`webtyp/gorelease`](https://github.com/webtyp/gorelease) attaches a [signed manifest](https://github.com/webtyp/update) (`manifest.txt` + `manifest.sig`). `selfupdate` verifies the [ECDSA P-256](https://datatracker.ietf.org/doc/html/rfc5480) signature against `/opt/<app>/update.pub` ([check flow](docs/diagrams/CHECK_FLOW.md)) and pulls the image by its immutable [`@sha256` digest](https://docs.docker.com/dhi/core-concepts/digests/)—never by a mutable tag.
  2. **The app chooses *when*; the supervisor executes *how*:** The web app knows when users have active sessions, so it calls the supervisor over an internal bearer-authenticated [HTTP API](docs/ARCHITECTURE.md#34-supervisor-phases--http-protocol-diagram) only when safe.
  3. **Automatic backup, migration, and rollback:** Before switching, `selfupdate` dumps PostgreSQL to `/opt/<app>/backups/`, runs migrations with the [database owner role](docs/ARCHITECTURE.md#42-least-privilege-database-separation-db_owner-vs-db_app), switches the `app` container, and probes `HTTPS /health` ([apply flow](docs/diagrams/APPLY_FLOW.md)). If the new version fails to become healthy within 120 seconds, `selfupdate` automatically restores the previous `compose.yaml` and image.

---

## Before You Start

1. **On the publisher's machine:**
   - Declare `Target: selfupdate.Server` ([target.go](target.go)) in your app's `config/deploy.go` ([`webtyp/deploy`](https://github.com/webtyp/deploy)).
   - Setup the target using `selfupdate.NewTarget(selfupdate.TargetConfig{RootDir: "."})` to validate the environment and deployment configuration.
   - Publish at least one signed GitHub Release with [`gorelease`](https://github.com/webtyp/gorelease) and export the base64 P-256 public key by running `gorelease key`.
2. **On the target Linux server:**
   - Root access (`sudo`) and [Docker Compose v2](https://docs.docker.com/compose/install/) (`docker compose version`).
   - Three tokens ready (defined in [install.go](install.go) and [layout.go](layout.go)):
     - `GITHUB_TOKEN`: read-only [GitHub personal access token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens) for Releases and Packages.
     - `TUNNEL_TOKEN`: [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) connector token routing your domain to `https://app:8444`.
     - `DNS_TOKEN`: [Cloudflare API token](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/) (`Zone.DNS:Edit`) for TLS certificate renewal.

---

## Step-by-Step Guide

### Step 1: Install the `selfupdate` CLI on the server

Install the CLI binary on the host machine ([cmd/selfupdate/main.go](cmd/selfupdate/main.go)):

```bash
go install webtyp.com/selfupdate/cmd/selfupdate@latest
selfupdate
```

Expected output (`stderr`, exit code `1`):

```text
Usage:
  selfupdate install <owner/repo>
  selfupdate serve <owner/repo> <app>
```

### Step 2: Provision the application (`selfupdate install`)

Run `selfupdate install <owner/repo>` as `root` ([install flow](docs/diagrams/INSTALL_FLOW.md)). Pass the public key and tokens via environment variables (or let it prompt interactively):

```bash
sudo PUBLIC_KEY="MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE..." \
     GITHUB_TOKEN="ghp_exampleReadOnlyToken" \
     TUNNEL_TOKEN="eyJhIjoi..." \
     DNS_TOKEN="cf_dns_token_example" \
     selfupdate install acme/shop
```

`selfupdate install` ([install.go](install.go)) verifies the latest Release, creates `/opt/shop` ([on-disk layout](docs/ARCHITECTURE.md#22-server-directory-layout-optapp)), writes `/opt/shop/secrets/*` with container UID ownership ([secret contract](docs/ARCHITECTURE.md#43-secret-file-ownership-contract)), runs migrations, starts all 5 services (`db`, `migrate`, `app`, `cloudflared`, `selfupdate`), waits for `https://app:8443/health`, and prints the installed version:

```text
v1.0.0
```

### Step 3: Check and apply updates over the Supervisor HTTP API

Inside the Compose network, `selfupdate serve <owner/repo> <app>` ([serve.go](serve.go), [supervisor.go](supervisor.go)) listens on `http://selfupdate:8080` authenticated by `/opt/<app>/secrets/supervisor_token` ([phases diagram](docs/diagrams/PHASES.md)):

```bash
TOKEN=$(sudo cat /opt/shop/secrets/supervisor_token)

# 1. Inspect current state (GET /status -> 200 OK)
sudo docker compose -f /opt/shop/compose.yaml exec selfupdate \
  wget -qO- --header="Authorization: Bearer $TOKEN" http://127.0.0.1:8080/status
# -> {"version":"v1.0.0","phase":"idle"}

# 2. Check GitHub for a newer signed release (POST /check -> 200 OK)
sudo docker compose -f /opt/shop/compose.yaml exec selfupdate \
  wget -qO- --post-data="" --header="Authorization: Bearer $TOKEN" http://127.0.0.1:8080/check
# -> {"version":"v1.0.0","available":"v1.1.0","phase":"available"}

# 3. Apply the waiting release in the background (POST /apply -> 202 Accepted)
sudo docker compose -f /opt/shop/compose.yaml exec selfupdate \
  wget -qO- --post-data="" --header="Authorization: Bearer $TOKEN" http://127.0.0.1:8080/apply
# -> {"version":"v1.0.0","available":"v1.1.0","phase":"applying"}
```

### Step 4: Generate `compose.yaml` from Go

To generate or inspect the canonical `compose.yaml` ([composefile.go](composefile.go)) in Go tooling:

```go
package main

import (
	"webtyp.com/fmt"
	"webtyp.com/selfupdate"
)

func main() {
	composeBytes, err := selfupdate.ComposeFile(selfupdate.App{
		Name:   "shop",
		Domain: "app.example.com",
		Repo:   "acme/shop",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	name, domain, err := selfupdate.ParseComposeApp(composeBytes)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("app=%s domain=%s dir=%s\n", name, domain, selfupdate.AppDir(name))
}
```

Expected output:

```text
app=shop domain=app.example.com dir=/opt/shop
```

For all exported path/env constants and sentinel errors, see [layout.go](layout.go), [composefile.go](composefile.go), [install.go](install.go), [check.go](check.go), [apply.go](apply.go), and [supervisor.go](supervisor.go).

---

## Documentation Map

| Order | Document | What question it answers & when to read it |
|---|---|---|
| 1 | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | **How does the system work inside?** On-disk layout in `/opt/<app>`, the 5 Compose services, secret UID ownership, and security boundaries. |
| 2 | [docs/diagrams/COMPOSE_FILE.md](docs/diagrams/COMPOSE_FILE.md) | **How is `compose.yaml` validated and generated?** Input rules in `ComposeFile(App)` called by `gorelease`. |
| 3 | [docs/diagrams/INSTALL_FLOW.md](docs/diagrams/INSTALL_FLOW.md) | **What happens during `selfupdate install`?** First-time provisioning from credential check to initial health probe. |
| 4 | [docs/diagrams/SERVE_FLOW.md](docs/diagrams/SERVE_FLOW.md) | **How does the `selfupdate` container start?** Secret loading, 24h backup loop, and HTTP listener startup. |
| 5 | [docs/diagrams/PHASES.md](docs/diagrams/PHASES.md) | **How do HTTP calls move the supervisor state?** Transitions between `idle`, `checking`, `available`, `applying`, `rolled_back`, and `backing_up`. |
| 6 | [docs/diagrams/CHECK_FLOW.md](docs/diagrams/CHECK_FLOW.md) | **How is a GitHub Release verified?** Tag comparison, failed blocklist, P-256 signature verification, and `compose.yaml` digest check. |
| 7 | [docs/diagrams/APPLY_FLOW.md](docs/diagrams/APPLY_FLOW.md) | **How does an update switch containers and roll back?** Database backup, digest pull, migration, health polling, and automatic rollback. |
