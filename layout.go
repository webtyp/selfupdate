package selfupdate

// The server layout every self-hosted webtyp app shares (docs/ARCHITECTURE.md).
// deploy and gorelease import these names; they never retype them.
const (
	DirRoot       = "/opt" // one folder per app: /opt/<app>
	FileCompose   = "compose.yaml"
	FileEnv       = ".env"
	FilePublicKey = "update.pub"
	FileState     = "state.json"
	DirSecrets    = "secrets"
	DirBackups    = "backups"

	ServiceDB         = "db"
	ServiceApp        = "app"
	ServiceTunnel     = "cloudflared"
	ServiceSupervisor = "selfupdate"
	SupervisorPort    = "8080"

	// EnvAppImage is the .env key the compose reads as the app's image (image@digest).
	EnvAppImage = "APP_IMAGE"

	SecretGitHubToken     = "github_token"
	SecretSupervisorToken = "supervisor_token"
	SecretTunnelToken     = "tunnel_token"
	SecretDNSToken        = "dns_token"
	SecretDBOwner         = "db_owner_password"
	SecretDBApp           = "db_app_password"
)

// AppDir is the folder of one app on the server.
// Example: selfupdate.AppDir("shop") == "/opt/shop"
func AppDir(app string) string { return DirRoot + "/" + app }
