package selfupdate

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"webtyp.com/deploy"
	"webtyp.com/lang"
)

// Server is the deploy target of a self-hosted app: a server you own running
// Docker Compose, kept up to date by this supervisor.
// Example (config/deploy.go): return deploy.Plan{Name: "shop", Domain: "app.example.com", Target: selfupdate.Server}
const Server deploy.TargetID = "webtyp.com/selfupdate.Server"

var (
	ErrNoRootDir = errors.New("selfupdate: RootDir is required")
	ErrNoServer  = errors.New("selfupdate: web/server/main.go is missing")
)

const (
	healthTimeoutSec = 3
	defaultHealthPath = "/health"
)

// TargetConfig configures Target.
type TargetConfig struct {
	RootDir string
	Probe   func(url string) (int, error) // nil = HTTPS GET with TLS verification
	Store   deploy.Destination            // runtime secrets store
}

// Target implements deploy.Target for a self-hosted server running Docker Compose.
type Target struct {
	rootDir string
	probe   func(url string) (int, error)
	store   deploy.Destination
}

var _ deploy.Target = (*Target)(nil)

// NewTarget returns a Target for a self-hosted application.
// Example: target, err := selfupdate.NewTarget(selfupdate.TargetConfig{RootDir: "."})
func NewTarget(cfg TargetConfig) (*Target, error) {
	if cfg.RootDir == "" {
		return nil, ErrNoRootDir
	}
	probe := cfg.Probe
	if probe == nil {
		probe = defaultProbe
	}
	return &Target{
		rootDir: cfg.RootDir,
		probe:   probe,
		store:   cfg.Store,
	}, nil
}

func (t *Target) ID() deploy.TargetID { return Server }

func (t *Target) Check(p deploy.Plan, ci deploy.Destination) ([]deploy.Missing, error) {
	if !validName(p.Name) {
		return nil, ErrAppName
	}
	if !validDomain(p.Domain) {
		return nil, ErrAppDomain
	}

	serverPath := filepath.Join(t.rootDir, "web", "server", "main.go")
	if _, err := os.Stat(serverPath); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoServer, serverPath)
	}

	var missing []deploy.Missing

	healthURL := "https://" + p.Domain + defaultHealthPath
	statusCode, _ := t.probe(healthURL)
	if statusCode != http.StatusOK {
		missing = append(missing, deploy.Missing{
			Item: lang.Text("Server installation"),
			Why:  lang.Text("The application server is not responding at " + healthURL),
			Fix:  deploy.FixManual,
			Steps: []lang.Text{
				lang.Text("Generate publisher key: gorelease key"),
				lang.Text("SSH into the host server"),
				lang.Text(fmt.Sprintf("Run: sudo selfupdate install %s", p.Name)),
				lang.Text("Enter the public key and tokens when prompted"),
			},
		})
	}

	if t.store != nil {
		for _, s := range p.Secrets {
			isRuntime := false
			for _, pl := range s.To {
				if pl == deploy.ToRuntime {
					isRuntime = true
					break
				}
			}
			if !isRuntime {
				continue
			}
			has, err := t.store.Has(s.Key)
			if err != nil {
				return nil, err
			}
			if !has {
				key := s.Key
				missing = append(missing, deploy.Missing{
					Item:   s.What,
					Why:    lang.Text("Required by the server environment"),
					Steps:  s.Guide,
					Fix:    deploy.FixInput,
					Secret: true,
					Accept: func(val string) error { return t.store.Put(key, val) },
				})
			}
		}
	}

	return missing, nil
}

func (t *Target) Provision(p deploy.Plan, ci deploy.Destination) error { return nil }

func (t *Target) Rotate(p deploy.Plan, ci deploy.Destination) error { return nil }

func (t *Target) Runtime(p deploy.Plan) deploy.Destination { return t.store }

func (t *Target) Job(p deploy.Plan) deploy.Job { return deploy.Job{} }

func defaultProbe(url string) (int, error) {
	client := &http.Client{
		Timeout: healthTimeoutSec * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
