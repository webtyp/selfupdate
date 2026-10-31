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
