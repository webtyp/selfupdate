# selfupdate
<img src="docs/img/badges.svg">

`selfupdate` is the **supervisor** of a self-hosted webtyp app: a small container that runs next to the app in its Docker Compose and is the **only** one allowed to talk to Docker. It finds out whether a newer version was published, checks that the publisher really signed it, switches the app to the new container image and goes back to the previous one when the new image does not come up healthy.

See [Architecture](docs/ARCHITECTURE.md), [Install flow](docs/diagrams/INSTALL_FLOW.md), [Serve flow](docs/diagrams/SERVE_FLOW.md), [Check flow](docs/diagrams/CHECK_FLOW.md), [Apply flow](docs/diagrams/APPLY_FLOW.md), [Compose file](docs/diagrams/COMPOSE_FILE.md), and [Phases](docs/diagrams/PHASES.md).

Example usage:
```go
u, ok, err := selfupdate.Checker{Releases: selfupdate.GitHub{Repo: "acme/shop", Token: token}, PublicKey: pub}.Check(state)

// The compose comes from selfupdate.ComposeFile(selfupdate.App{Name: "shop", Domain: "app.example.com", Repo: "acme/shop"}).
next, err := selfupdate.Applier{Engine: selfupdate.Compose{Dir: dir}, Dir: dir}.Apply(state, u)

// Serving the HTTP API
s := selfupdate.NewSupervisor(checker, applier, dir+"/"+selfupdate.FileState)
s.Backup() // once a day
http.ListenAndServe(":8080", s.Handler("secret"))
```

## Commands

- `selfupdate install <owner/repo>`: Provisions `/opt/<app>`, pulls images, creates secrets, and starts the database and app.
- `selfupdate serve <owner/repo> <app>`: Runs inside the supervisor container to serve the HTTP API and perform 24h backups.
