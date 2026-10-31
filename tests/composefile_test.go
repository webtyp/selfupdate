package tests

import (
	"errors"
	"os"
	"strings"
	"testing"

	"webtyp.com/selfupdate"
)

func TestComposeFile_Golden(t *testing.T) {
	data, err := selfupdate.ComposeFile(selfupdate.App{Name: "acme-shop", Domain: "app.example.com", Repo: "acme/shop"})
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/compose.golden.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(golden) {
		t.Errorf("output does not match testdata/compose.golden.yaml\nGot:\n%s", string(data))
	}
}

func TestComposeFile_UsesLayoutNames(t *testing.T) {
	data, err := selfupdate.ComposeFile(selfupdate.App{Name: "acme-shop", Domain: "app.example.com", Repo: "acme/shop"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)

	tests := []string{
		"  " + selfupdate.ServiceDB + ":",
		"  " + selfupdate.ServiceMigrate + ":",
		"  " + selfupdate.ServiceApp + ":",
		"  " + selfupdate.ServiceTunnel + ":",
		"  " + selfupdate.ServiceSupervisor + ":",
		"${" + selfupdate.EnvAppImage + "}",
		"${" + selfupdate.EnvSupervisorImage + "}",
		"${" + selfupdate.EnvLANAddress + "}:443:" + selfupdate.AppPort,
		selfupdate.EnvPort + ": \"" + selfupdate.AppPort + "\"",
		selfupdate.EnvInternetPort + ": \"" + selfupdate.AppInternetPort + "\"",
		selfupdate.EnvAppDomain + ":",
		selfupdate.EnvDatabaseURL + "_FILE",
		selfupdate.EnvDNSToken + "_FILE",
		selfupdate.EnvTunnelToken + "_FILE",
		selfupdate.EnvSupervisorToken + "_FILE",
		selfupdate.EnvSupervisorURL + ": http://" + selfupdate.ServiceSupervisor + ":" + selfupdate.SupervisorPort,
		selfupdate.DirSecrets + "/" + selfupdate.FileInitDB,
		selfupdate.DBImage,
		selfupdate.TunnelImage,
		selfupdate.AppDir("acme-shop"),
	}

	for _, s := range []string{selfupdate.SecretDBOwner, selfupdate.SecretDatabaseURL, selfupdate.SecretDatabaseOwnerURL, selfupdate.SecretDNSToken, selfupdate.SecretTunnelToken, selfupdate.SecretSupervisorToken} {
		tests = append(tests, "  "+s+": { file: ./"+selfupdate.DirSecrets+"/"+s+" }")
	}

	for _, x := range tests {
		if !strings.Contains(out, x) {
			t.Errorf("expected output to contain %q", x)
		}
	}
}

func TestComposeFile_DatabaseName(t *testing.T) {
	data, err := selfupdate.ComposeFile(selfupdate.App{Name: "acme-shop", Domain: "app.example.com", Repo: "acme/shop"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "POSTGRES_DB: acme_shop\n") {
		t.Errorf("expected output to contain POSTGRES_DB: acme_shop\\n")
	}
	if !strings.Contains(out, "POSTGRES_USER: acme_shop_owner\n") {
		t.Errorf("expected output to contain POSTGRES_USER: acme_shop_owner\\n")
	}
	if strings.Contains(out, "acme-shop_owner") {
		t.Errorf("expected output to never contain acme-shop_owner")
	}
}

func TestComposeFile_RejectsName(t *testing.T) {
	tests := []string{"", "Acme", "1shop", "shop_x", "shop app", "-shop"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			data, err := selfupdate.ComposeFile(selfupdate.App{Name: name, Domain: "app.example.com", Repo: "acme/shop"})
			if data != nil {
				t.Errorf("expected data to be nil, got %v", data)
			}
			if !errors.Is(err, selfupdate.ErrAppName) {
				t.Errorf("expected ErrAppName, got %v", err)
			}
		})
	}
}

func TestComposeFile_RejectsDomain(t *testing.T) {
	tests := []string{"", "localhost", "app..example.com", "-app.example.com", "app-.example.com", "App.example.com", "app.example.com/"}
	for _, domain := range tests {
		t.Run(domain, func(t *testing.T) {
			data, err := selfupdate.ComposeFile(selfupdate.App{Name: "acme-shop", Domain: domain, Repo: "acme/shop"})
			if data != nil {
				t.Errorf("expected data to be nil, got %v", data)
			}
			if !errors.Is(err, selfupdate.ErrAppDomain) {
				t.Errorf("expected ErrAppDomain, got %v", err)
			}
		})
	}
}

func TestComposeFile_RejectsRepo(t *testing.T) {
	tests := []string{"", "acme", "acme/", "/shop", "acme/shop/x", "acme shop/x"}
	for _, repo := range tests {
		t.Run(repo, func(t *testing.T) {
			data, err := selfupdate.ComposeFile(selfupdate.App{Name: "acme-shop", Domain: "app.example.com", Repo: repo})
			if data != nil {
				t.Errorf("expected data to be nil, got %v", data)
			}
			if !errors.Is(err, selfupdate.ErrAppRepo) {
				t.Errorf("expected ErrAppRepo, got %v", err)
			}
		})
	}
}

func TestComposeFile_NameCheckedFirst(t *testing.T) {
	_, err := selfupdate.ComposeFile(selfupdate.App{})
	if !errors.Is(err, selfupdate.ErrAppName) {
		t.Errorf("expected ErrAppName, got %v", err)
	}
}

func TestComposeFile_AcceptsRepoSymbols(t *testing.T) {
	data, err := selfupdate.ComposeFile(selfupdate.App{Name: "acme-shop", Domain: "app.example.com", Repo: "Acme-Labs/shop_v2.web"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "command: [\"serve\", \"Acme-Labs/shop_v2.web\", \"acme-shop\"]") {
		t.Errorf("expected output to contain serve Acme-Labs/shop_v2.web acme-shop")
	}
}

func TestServer_ID(t *testing.T) {
	if string(selfupdate.Server) != "webtyp.com/selfupdate.Server" {
		t.Errorf("expected webtyp.com/selfupdate.Server, got %s", string(selfupdate.Server))
	}
}
