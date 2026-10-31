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

	// DBImage and TunnelImage are pinned: a new version of either is a change to this file.
	DBImage     = "postgres:17"
	TunnelImage = "cloudflare/cloudflared:2026.9.3"

	// EnvSupervisorImage is the .env key with the supervisor's image, written by install;
	// an app update never changes it.
	EnvSupervisorImage = "SUPERVISOR_IMAGE"
	// EnvLANAddress is the .env key with the host address the office door is published on (127.0.0.1 on a dev PC).
	EnvLANAddress = "LAN_ADDRESS"

	// Environment of the app container (the app reads secrets as <KEY>_FILE with webtyp/env).
	EnvPort            = "PORT"
	EnvInternetPort    = "INTERNET_PORT"
	EnvDatabaseURL     = "DATABASE_URL"
	EnvDNSToken        = "CLOUDFLARE_DNS_API_TOKEN"
	EnvSupervisorURL   = "SUPERVISOR_URL"
	EnvSupervisorToken = "SUPERVISOR_TOKEN"
	EnvTunnelToken     = "TUNNEL_TOKEN"

	SecretDatabaseURL      = "database_url"       // postgres://<db>_app:…@db:5432/<db>
	SecretDatabaseOwnerURL = "database_owner_url" // postgres://<db>_owner:…@db:5432/<db>
	// FileInitDB is in secrets/: the SQL that creates the rows-only user on the database's first start.
	FileInitDB = "initdb.sql"

	// NonRootUID is the user of the app and cloudflared images: the secret files they read belong to it.
	NonRootUID = 65532
	// PostgresUID is the postgres user of DBImage: it reads FileInitDB.
	PostgresUID = 999
)

// AppDir is the folder of one app on the server.
// Example: selfupdate.AppDir("shop") == "/opt/shop"
func AppDir(app string) string { return DirRoot + "/" + app }
