# Install Flow

When an operator runs `selfupdate install <owner/repo>` (or calls `selfupdate.Install(cfg)`) on a fresh Linux server, this flow validates the operator's credentials, verifies the latest signed GitHub Release, provisions `/opt/<app>`, runs database migrations, and starts the full Docker Compose stack. Every branch is verified in `tests/install_test.go`.

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

## Step-by-Step Walkthrough

1. **Validate inputs:** Checks `Repo` format (`ErrAppRepo`), decodes the P-256 `PublicKey` (`ErrPublicKey`), and verifies that `GitHubToken` (`ErrNoGitHubToken`), `TunnelToken` (`ErrNoTunnelToken`), and `DNSToken` (`ErrNoDNSToken`) are non-empty before touching the network or disk.
2. **Fetch and verify the latest Release:** Calls `Checker.Check(State{})` against GitHub. If no release exists, returns `ErrNoRelease`; if the signature or digest check fails, returns the verification error.
3. **Extract `app` and `domain`:** Calls `ParseComposeApp` on the verified `compose.yaml` bytes to read `name:` and `APP_DOMAIN:`. Returns `ErrAppName` or `ErrAppDomain` if either is malformed.
4. **Provision `/opt/<app>` and secrets:** Creates `/opt/<app>`, `/opt/<app>/secrets`, and `/opt/<app>/backups` (mode `0700`). Generates random 32-byte hex passwords for `db_owner_password`, `db_app_password`, and `supervisor_token` (or reuses existing files), writes all 9 files in `secrets/` with mode `0400` and their target container UIDs (`0`, `999`, `65532`), and writes `update.pub`, `compose.yaml`, and `.env`.
5. **Bring up the database, migrate, and start services:** Pulls the verified image (`Engine.Pull`), starts PostgreSQL (`Up(db)`), executes the schema migration (`Engine.Migrate`, returning `ErrMigrate` on failure), and starts all services (`Up(all)`).
6. **Verify initial health and save state:** Polls `Engine.Healthy()` until `https://app:8443/health` responds `200 OK` (or returns `ErrUnhealthy` if the deadline expires). Once healthy, writes `/opt/<app>/state.json` with the installed `Version` and `Image`.
