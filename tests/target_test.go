package tests

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"webtyp.com/deploy"
	"webtyp.com/lang"
	"webtyp.com/selfupdate"
)

type mapStore map[string]string

func (m mapStore) Has(key string) (bool, error) {
	_, ok := m[key]
	return ok, nil
}

func (m mapStore) Put(key, value string) error {
	m[key] = value
	return nil
}

func TestTarget_Validation(t *testing.T) {
	_, err := selfupdate.NewTarget(selfupdate.TargetConfig{RootDir: ""})
	if err == nil {
		t.Fatal("expected error on empty RootDir")
	}

	tmp := t.TempDir()
	tgt, err := selfupdate.NewTarget(selfupdate.TargetConfig{RootDir: tmp})
	if err != nil {
		t.Fatal(err)
	}

	plan := deploy.Plan{Name: "INVALID NAME", Domain: "app.example.com", Target: selfupdate.Server}
	if _, err := tgt.Check(plan, nil); err == nil {
		t.Fatal("expected ErrAppName")
	}

	plan = deploy.Plan{Name: "shop", Domain: "invalid", Target: selfupdate.Server}
	if _, err := tgt.Check(plan, nil); err == nil {
		t.Fatal("expected ErrAppDomain")
	}

	plan = deploy.Plan{Name: "shop", Domain: "app.example.com", Target: selfupdate.Server}
	if _, err := tgt.Check(plan, nil); err == nil {
		t.Fatal("expected error for missing web/server/main.go")
	}
}

func TestTarget_Check_HealthyAndUnhealthy(t *testing.T) {
	tmp := t.TempDir()
	serverDir := filepath.Join(tmp, "web", "server")
	if err := os.MkdirAll(serverDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serverDir, "main.go"), []byte("package main\nfunc main(){}"), 0644); err != nil {
		t.Fatal(err)
	}

	plan := deploy.Plan{
		Name:   "shop",
		Domain: "app.example.com",
		Target: selfupdate.Server,
	}

	// 1. Unhealthy probe
	tgtUnhealthy, _ := selfupdate.NewTarget(selfupdate.TargetConfig{
		RootDir: tmp,
		Probe: func(url string) (int, error) {
			return http.StatusBadGateway, nil
		},
	})
	missing, err := tgtUnhealthy.Check(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].Fix != deploy.FixManual {
		t.Fatalf("expected 1 manual missing item, got %+v", missing)
	}

	// 2. Healthy probe
	tgtHealthy, _ := selfupdate.NewTarget(selfupdate.TargetConfig{
		RootDir: tmp,
		Probe: func(url string) (int, error) {
			return http.StatusOK, nil
		},
	})
	missing, err = tgtHealthy.Check(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected 0 missing items, got %+v", missing)
	}
}

func TestTarget_Check_Secrets(t *testing.T) {
	tmp := t.TempDir()
	serverDir := filepath.Join(tmp, "web", "server")
	if err := os.MkdirAll(serverDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serverDir, "main.go"), []byte("package main\nfunc main(){}"), 0644); err != nil {
		t.Fatal(err)
	}

	store := make(mapStore)
	tgt, _ := selfupdate.NewTarget(selfupdate.TargetConfig{
		RootDir: tmp,
		Probe:   func(url string) (int, error) { return http.StatusOK, nil },
		Store:   store,
	})

	plan := deploy.Plan{
		Name:   "shop",
		Domain: "app.example.com",
		Target: selfupdate.Server,
		Secrets: []deploy.Secret{
			{
				Key:    "TUNNEL_TOKEN",
				What:   lang.Text("Cloudflare Tunnel token"),
				Source: deploy.Typed,
				Guide:  []lang.Text{lang.Text("https://one.dash.cloudflare.com/")},
				To:     []deploy.Place{deploy.ToRuntime},
			},
		},
	}

	missing, err := tgt.Check(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].Fix != deploy.FixInput {
		t.Fatalf("expected 1 input missing item, got %+v", missing)
	}

	// Accept typed value
	if err := missing[0].Accept("secret123"); err != nil {
		t.Fatal(err)
	}
	if store["TUNNEL_TOKEN"] != "secret123" {
		t.Fatalf("store token mismatch: %s", store["TUNNEL_TOKEN"])
	}

	// Check again: should be resolved
	missing, err = tgt.Check(plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected 0 missing items after accept, got %+v", missing)
	}
}
