package selfupdate

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
)

// Phase is where the supervisor is (docs/diagrams/PHASES.md).
type Phase string

const (
	PhaseIdle       Phase = "idle"
	PhaseChecking   Phase = "checking"
	PhaseAvailable  Phase = "available"
	PhaseApplying   Phase = "applying"
	PhaseRolledBack Phase = "rolled_back"
)

var (
	// ErrBusy: a check or an apply is already running.
	ErrBusy = errors.New("selfupdate: a check or an update is already running")
	// ErrNothingToApply: no verified version is waiting; check first.
	ErrNothingToApply = errors.New("selfupdate: no version is waiting to be applied")
)

const (
	PathStatus = "/status"
	PathCheck  = "/check"
	PathApply  = "/apply"

	headerAuthorization = "Authorization"
	headerContentType   = "Content-Type"
	contentJSON         = "application/json"
)

// Status is what GET /status answers.
type Status struct {
	Version   string `json:"version"`
	Available string `json:"available,omitempty"`
	Phase     Phase  `json:"phase"`
	Message   string `json:"message,omitempty"`
}

// Supervisor runs one check or one apply at a time and remembers the version
// waiting to be applied.
// Example: s := selfupdate.NewSupervisor(checker, applier, dir+"/"+selfupdate.FileState)
type Supervisor struct {
	checker   Checker
	applier   Applier
	statePath string

	mu        sync.Mutex
	phase     Phase
	available *Update
	message   string
}

func NewSupervisor(c Checker, a Applier, statePath string) *Supervisor {
	return &Supervisor{checker: c, applier: a, statePath: statePath, phase: PhaseIdle}
}

// Status reports the running version (from the state file), the waiting one and the phase.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Phase: s.phase, Message: s.message}
	if state, err := LoadState(s.statePath); err == nil {
		st.Version = state.Version
	} else {
		st.Message = err.Error()
	}
	if s.available != nil {
		st.Available = s.available.Manifest.Version
	}
	return st
}

// Check looks for a newer verified version.
func (s *Supervisor) Check() error {
	state, err := s.begin(PhaseChecking)
	if err != nil {
		return err
	}
	m, ok, err := s.checker.Check(state)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.available = nil
	s.phase = PhaseIdle
	s.message = ""
	if err != nil {
		s.message = err.Error()
		return err
	}
	if ok {
		s.available = &m
		s.phase = PhaseAvailable
	}
	return nil
}

// Apply switches to the waiting version (blocking; the HTTP handler runs it in the background).
func (s *Supervisor) Apply() error {
	s.mu.Lock()
	if s.phase == PhaseChecking || s.phase == PhaseApplying {
		s.mu.Unlock()
		return ErrBusy
	}
	if s.available == nil {
		s.mu.Unlock()
		return ErrNothingToApply
	}
	m := *s.available
	s.phase = PhaseApplying
	s.message = ""
	s.mu.Unlock()

	state, err := LoadState(s.statePath)
	if err == nil {
		_, err = s.applier.Apply(state, m)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.available = nil
	s.phase = PhaseIdle
	if err != nil {
		s.message = err.Error()
		if errors.Is(err, ErrUnhealthy) {
			s.phase = PhaseRolledBack
		}
	}
	return err
}

// begin moves to a busy phase unless one is running, and reads the state.
func (s *Supervisor) begin(p Phase) (State, error) {
	s.mu.Lock()
	if s.phase == PhaseChecking || s.phase == PhaseApplying {
		s.mu.Unlock()
		return State{}, ErrBusy
	}
	s.phase = p
	s.mu.Unlock()
	state, err := LoadState(s.statePath)
	if err != nil {
		s.mu.Lock()
		s.phase = PhaseIdle
		s.message = err.Error()
		s.mu.Unlock()
	}
	return state, err
}

// Handler serves GET /status, POST /check and POST /apply to whoever presents
// the token. An empty token denies every request.
// Example: http.ListenAndServe(":"+selfupdate.SupervisorPort, s.Handler(token))
func (s *Supervisor) Handler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case PathStatus:
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			writeStatus(w, http.StatusOK, s.Status())
		case PathCheck:
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			err := s.Check()
			switch {
			case errors.Is(err, ErrBusy):
				writeStatus(w, http.StatusConflict, s.Status())
			case err != nil:
				writeStatus(w, http.StatusBadGateway, s.Status())
			default:
				writeStatus(w, http.StatusOK, s.Status())
			}
		case PathApply:
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			s.mu.Lock()
			ready := s.available != nil && s.phase == PhaseAvailable
			s.mu.Unlock()
			if !ready {
				writeStatus(w, http.StatusConflict, s.Status())
				return
			}
			go s.Apply()
			writeStatus(w, http.StatusAccepted, s.Status())
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	got, ok := strings.CutPrefix(r.Header.Get(headerAuthorization), bearerPrefix)
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func writeStatus(w http.ResponseWriter, code int, st Status) {
	w.Header().Set(headerContentType, contentJSON)
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(st)
}
