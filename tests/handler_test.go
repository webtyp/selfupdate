package tests

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"webtyp.com/selfupdate"
)

func TestHandler_NoToken(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler("secret"))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestHandler_WrongToken(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler("secret"))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
	req.Header.Set("Authorization", "Bearer nope")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestHandler_EmptyTokenDeniesAll(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler(""))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
	req.Header.Set("Authorization", "Bearer ")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestHandler_Status(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler("secret"))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var st selfupdate.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if st.Version != "v1.1.0" || st.Phase != "idle" {
		t.Errorf("expected version v1.1.0 and phase idle, got version %s, phase %s", st.Version, st.Phase)
	}
}

func TestHandler_Check(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler("secret"))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/check", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var st selfupdate.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if st.Phase != "available" {
		t.Errorf("expected phase available, got %s", st.Phase)
	}
}

func TestHandler_CheckFails(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler("secret"))
	defer srv.Close()

	// Modify the supervisor to simulate a failed check.
	// Since defaultSupervisor uses fakeReleases with a valid v1.2.0,
	// we would need to mock the error. For the purpose of this test, we can
	// change the checker's releases field to return an error.
	s.Check() // Clear available state first if needed, though starts idle.

	// In a real scenario we'd inject the error. Let's create a new supervisor for this test
	dir := t.TempDir()
	statePath := dir + "/state.json"
	selfupdate.State{Version: "v1.1.0"}.Save(statePath)
	fakeErr := string("fake latest error")
	checker := selfupdate.Checker{Releases: fakeReleases{err: errors.New(fakeErr)}}
	s2 := selfupdate.NewSupervisor(checker, selfupdate.Applier{}, statePath)

	srv2 := httptest.NewServer(s2.Handler("secret"))
	defer srv2.Close()

	req, _ := http.NewRequest(http.MethodPost, srv2.URL+"/check", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", resp.StatusCode)
	}
	var st selfupdate.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if st.Message != fakeErr {
		t.Errorf("expected message %q, got %q", fakeErr, st.Message)
	}
}

func TestHandler_ApplyAccepted(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{healthy: true})
	srv := httptest.NewServer(s.Handler("secret"))
	defer srv.Close()

	// 1. Check
	req1, _ := http.NewRequest(http.MethodPost, srv.URL+"/check", nil)
	req1.Header.Set("Authorization", "Bearer secret")
	resp1, _ := http.DefaultClient.Do(req1)
	resp1.Body.Close()

	// 2. Apply
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/apply", nil)
	req2.Header.Set("Authorization", "Bearer secret")
	resp2, _ := http.DefaultClient.Do(req2)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp2.StatusCode)
	}

	// 3. Poll
	done := false
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		req3, _ := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
		req3.Header.Set("Authorization", "Bearer secret")
		resp3, _ := http.DefaultClient.Do(req3)
		var st selfupdate.Status
		json.NewDecoder(resp3.Body).Decode(&st)
		resp3.Body.Close()

		if st.Phase == "idle" && st.Version == "v1.2.0" {
			done = true
			break
		}
	}
	if !done {
		t.Error("polling timed out before reaching idle with version v1.2.0")
	}
}

func TestHandler_ApplyNothing(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler("secret"))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/apply", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409, got %d", resp.StatusCode)
	}
}

func TestHandler_WrongMethod(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler("secret"))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/apply", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestHandler_UnknownPath(t *testing.T) {
	s, _ := defaultSupervisor(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler("secret"))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/nope", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}
