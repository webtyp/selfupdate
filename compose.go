package selfupdate

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	cmdDocker    = "docker"
	argCompose   = "compose"
	argExec      = "exec"
	argNoTTY     = "-T"
	argPull      = "pull"
	argUp        = "up"
	argDetach    = "-d"
	argNoDeps    = "--no-deps"
	argShell     = "sh"
	argShellC    = "-c"
	// dumpScript runs inside the db container, with the user and database the
	// official image already knows from its environment.
	dumpScript   = `pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"`
	envSeparator = "="
	lineEnd      = "\n"
	healthScheme = "https://"
	healthPath   = "/health"
	healthTimeout = 5 * time.Second
)

// ErrNoDomain: .env has no APP_DOMAIN, so the health probe cannot check the certificate.
var ErrNoDomain = errors.New("selfupdate: .env has no APP_DOMAIN")

// Compose is the Engine over the docker CLI, run in the app's folder.
// Example: selfupdate.Compose{Dir: selfupdate.AppDir("shop")}
type Compose struct {
	Dir   string
	Run   func(dir string, stdout io.Writer, name string, args ...string) error // nil = os/exec
	Probe func() bool                                                       // nil = HTTPS GET of the app's /health
}

var _ Engine = Compose{}

func (c Compose) Backup(w io.Writer) error {
	return c.run(w, cmdDocker, argCompose, argExec, argNoTTY, ServiceDB, argShell, argShellC, dumpScript)
}

func (c Compose) Pull(ref string) error { return c.run(io.Discard, cmdDocker, argPull, ref) }

func (c Compose) Switch(ref string) error {
	if err := setEnv(filepath.Join(c.Dir, FileEnv), EnvAppImage, ref); err != nil {
		return err
	}
	return c.run(io.Discard, cmdDocker, argCompose, argUp, argDetach, argNoDeps, ServiceApp)
}

// Healthy asks the app's office door for /health over the internal network and
// verifies its certificate against APP_DOMAIN (the name the certificate is for).
func (c Compose) Healthy() bool {
	if c.Probe != nil {
		return c.Probe()
	}
	domain, err := readEnv(filepath.Join(c.Dir, FileEnv), EnvAppDomain)
	if err != nil {
		return false
	}
	client := &http.Client{
		Timeout:   healthTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{ServerName: domain}},
	}
	resp, err := client.Get(healthScheme + ServiceApp + ":" + AppPort + healthPath)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c Compose) run(stdout io.Writer, name string, args ...string) error {
	if c.Run != nil {
		return c.Run(c.Dir, stdout, name, args...)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Dir = c.Dir
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// setEnv writes key=value in the .env file, keeping every other line; atomic, owner-only.
func setEnv(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var out []string
	found := false
	for _, line := range strings.Split(string(data), lineEnd) {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, key+envSeparator) {
			out = append(out, key+envSeparator+value)
			found = true
			continue
		}
		out = append(out, line)
	}
	if !found {
		out = append(out, key+envSeparator+value)
	}
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, []byte(strings.Join(out, lineEnd)+lineEnd), stateFileMode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readEnv(path, key string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), lineEnd) {
		if v, ok := strings.CutPrefix(line, key+envSeparator); ok && v != "" {
			return v, nil
		}
	}
	return "", ErrNoDomain
}
