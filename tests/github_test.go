package tests

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"webtyp.com/selfupdate"
)

func TestGitHub_Latest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Errorf("expected Authorization: Bearer t, got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("expected Accept: application/vnd.github+json, got %q", r.Header.Get("Accept"))
		}
		w.Write([]byte(`{"tag_name":"v1.2.0","assets":[{"name":"manifest.txt","url":"http://` + r.Host + `/assets/1"},{"name":"manifest.sig","url":"http://` + r.Host + `/assets/2"}]}`))
	}))
	defer srv.Close()

	gh := selfupdate.GitHub{Repo: "acme/shop", Token: "t", API: srv.URL}
	rel, err := gh.Latest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rel.Tag != "v1.2.0" {
		t.Errorf("expected tag v1.2.0, got %q", rel.Tag)
	}
	if len(rel.Assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(rel.Assets))
	}
	if rel.Assets[0].Name != "manifest.txt" || rel.Assets[0].URL != "http://"+srv.Listener.Addr().String()+"/assets/1" {
		t.Errorf("unexpected asset 0: %+v", rel.Assets[0])
	}
	if rel.Assets[1].Name != "manifest.sig" || rel.Assets[1].URL != "http://"+srv.Listener.Addr().String()+"/assets/2" {
		t.Errorf("unexpected asset 1: %+v", rel.Assets[1])
	}
}

func TestGitHub_Download(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/octet-stream" {
			t.Errorf("expected Accept: application/octet-stream, got %q", r.Header.Get("Accept"))
		}
		w.Write([]byte("asset bytes"))
	}))
	defer srv.Close()

	gh := selfupdate.GitHub{}
	asset := selfupdate.Asset{URL: srv.URL}
	data, err := gh.Download(asset)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "asset bytes" {
		t.Errorf("expected asset bytes, got %q", data)
	}
}

func TestGitHub_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	gh := selfupdate.GitHub{Repo: "acme/shop", API: srv.URL}
	_, err := gh.Latest()
	if !errors.Is(err, selfupdate.ErrGitHubStatus) {
		t.Errorf("expected ErrGitHubStatus, got %v", err)
	}
	if err != nil && !contains(err.Error(), "404") {
		t.Errorf("expected error to contain 404, got %v", err)
	}
}

func contains(s, substr string) bool {
	// simple contains
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}
