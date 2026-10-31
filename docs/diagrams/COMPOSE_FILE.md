# ComposeFile

What `selfupdate.ComposeFile(app)` checks before writing the server's `compose.yaml`. `gorelease` calls it with the
app's `config/deploy.go` values and uploads the result with each Release; the signed manifest carries its digest.

```mermaid
flowchart TD
    A["ComposeFile(App{Name, Domain, Repo})"] --> N{"Name: lowercase letters, digits, dashes,<br/>starts with a letter?"}
    N -->|no| EN["ErrAppName, no bytes"]
    N -->|yes| D{"Domain: two or more labels of<br/>lowercase letters, digits, dashes?"}
    D -->|no| ED["ErrAppDomain, no bytes"]
    D -->|yes| R{"Repo: owner/repo?"}
    R -->|no| ER["ErrAppRepo, no bytes"]
    R -->|yes| W["compose.yaml: db, migrate, app, cloudflared, selfupdate<br/>database name = Name with - as _"]
```
