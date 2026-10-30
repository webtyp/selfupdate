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
	// ServiceMigrate is the one-off service that runs the app image's migration program with the owner user.
	ServiceMigrate    = "migrate"
	SupervisorPort    = "8080"

	// AppPort and AppInternetPort are the ports the app listens on inside its
	// container: the office door (published on the host's 443) and the internet
	// door (reached only by cloudflared on the internal network).
	AppPort         = "8443"
	AppInternetPort = "8444"
	// EnvAppDomain is the name the app is reached by; the same key httpd.FromEnv reads.
	EnvAppDomain = "APP_DOMAIN"

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
