package selfupdate

import "webtyp.com/deploy"

// Server is the deploy target of a self-hosted app: a server you own running
// Docker Compose, kept up to date by this supervisor.
// Example (config/deploy.go): return deploy.Plan{Name: "shop", Domain: "app.example.com", Target: selfupdate.Server}
const Server deploy.TargetID = "webtyp.com/selfupdate.Server"
