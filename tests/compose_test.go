package tests

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/selfupdate"
)

func fakeRun(cmd selfupdate.Command) error {
	if cmd.Stdout != nil {
		cmd.Stdout.Write([]byte("dump"))
	}
	return nil
}

func fakeRunErr(cmd selfupdate.Command) error {
	return errors.New("run failed")
}

func TestCompose_Backup(t *testing.T) {
	dir := t.TempDir()
	var runDir, runCmd string
	run := func(cmd selfupdate.Command) error {
		runDir = cmd.Dir
		runCmd = cmd.Name + " " + strings.Join(cmd.Args, " ")
		if cmd.Stdout != nil {
			cmd.Stdout.Write([]byte("dump"))
		}
		return nil
	}
	c := selfupdate.Compose{Dir: dir, Run: run, Probe: func() bool { return true }}

	var out strings.Builder
	err := c.Backup(&out)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runDir != dir {
		t.Errorf("expected dir %s, got %s", dir, runDir)
	}
	expectedCmd := `docker compose exec -T db sh -c pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"`
	if runCmd != expectedCmd {
		t.Errorf("expected cmd %q, got %q", expectedCmd, runCmd)
	}
	if out.String() != "dump" {
		t.Errorf("expected output 'dump', got %q", out.String())
	}
}

func TestCompose_Pull(t *testing.T) {
	dir := t.TempDir()
	var runCmd string
	run := func(cmd selfupdate.Command) error {
		runCmd = cmd.Name + " " + strings.Join(cmd.Args, " ")
		return nil
	}
	c := selfupdate.Compose{Dir: dir, Run: run}

	err := c.Pull("ghcr.io/acme/shop:v1.2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedCmd := "docker pull ghcr.io/acme/shop:v1.2.0"
	if runCmd != expectedCmd {
		t.Errorf("expected cmd %q, got %q", expectedCmd, runCmd)
	}
}

func TestCompose_Switch(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, selfupdate.FileEnv)
	initialEnv := "APP_DOMAIN=app.example.com\nAPP_IMAGE=old\n"
	os.WriteFile(envPath, []byte(initialEnv), 0600)

	var runCmd string
	run := func(cmd selfupdate.Command) error {
		runCmd = cmd.Name + " " + strings.Join(cmd.Args, " ")
		return nil
	}
	c := selfupdate.Compose{Dir: dir, Run: run}

	err := c.Switch("new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedCmd := "docker compose up -d --no-deps app"
	if runCmd != expectedCmd {
		t.Errorf("expected cmd %q, got %q", expectedCmd, runCmd)
	}

	envContent, _ := os.ReadFile(envPath)
	expectedEnv := "APP_DOMAIN=app.example.com\nAPP_IMAGE=new\n"
	if string(envContent) != expectedEnv {
		t.Errorf("expected env %q, got %q", expectedEnv, envContent)
	}

	info, _ := os.Stat(envPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected mode 0600, got %v", info.Mode().Perm())
	}
}

func TestCompose_SwitchAddsMissingKey(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, selfupdate.FileEnv)
	initialEnv := "APP_DOMAIN=app.example.com\n"
	os.WriteFile(envPath, []byte(initialEnv), 0600)

	c := selfupdate.Compose{Dir: dir, Run: func(cmd selfupdate.Command) error { return nil }}

	err := c.Switch("new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	envContent, _ := os.ReadFile(envPath)
	expectedEnv := "APP_DOMAIN=app.example.com\nAPP_IMAGE=new\n"
	if string(envContent) != expectedEnv {
		t.Errorf("expected env %q, got %q", expectedEnv, envContent)
	}
}

func TestCompose_Migrate(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, selfupdate.FileEnv)
	initialEnv := "APP_DOMAIN=app.example.com\nAPP_IMAGE=old\n"
	os.WriteFile(envPath, []byte(initialEnv), 0600)

	var runCmd string
	var runDir string
	var runEnv []string
	run := func(cmd selfupdate.Command) error {
		runCmd = cmd.Name + " " + strings.Join(cmd.Args, " ")
		runDir = cmd.Dir
		runEnv = cmd.Env
		return nil
	}
	c := selfupdate.Compose{Dir: dir, Run: run}

	err := c.Migrate("new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedCmd := "docker compose run --rm --no-deps migrate"
	if runCmd != expectedCmd {
		t.Errorf("expected cmd %q, got %q", expectedCmd, runCmd)
	}
	if runDir != dir {
		t.Errorf("expected dir %q, got %q", dir, runDir)
	}
	if len(runEnv) != 1 || runEnv[0] != "APP_IMAGE=new" {
		t.Errorf("expected env %q, got %q", []string{"APP_IMAGE=new"}, runEnv)
	}

	envContent, _ := os.ReadFile(envPath)
	if string(envContent) != initialEnv {
		t.Errorf("expected .env to remain unchanged %q, got %q", initialEnv, envContent)
	}
}

func TestCompose_RunFails(t *testing.T) {
	dir := t.TempDir()
	c := selfupdate.Compose{Dir: dir, Run: fakeRunErr}
	err := c.Pull("ref")
	if err == nil || err.Error() != "run failed" {
		t.Errorf("expected 'run failed' error, got %v", err)
	}
}
