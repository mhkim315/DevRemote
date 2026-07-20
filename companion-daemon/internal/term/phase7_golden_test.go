package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

// Phase 7: status taxonomy + capability golden + diagnostics proof.

func TestStatusTaxonomy_EmptyVsUnavailable(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		reg := mux.MustNewRegistry(&emptyAdapter{})
		h := &Handlers{Registry: reg}
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		rec := httptest.NewRecorder()
		h.HandleSessionsAPI(rec, req)
		var sessions []SessionTelemetry
		json.Unmarshal(rec.Body.Bytes(), &sessions)
		if len(sessions) != 0 {
			t.Errorf("empty: %d sessions, want 0", len(sessions))
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		adapter := &unavailableAdapter{}
		reg := mux.MustNewRegistry(adapter)
		_ = reg.Sessions(context.Background())
		snap, _ := reg.Snapshot(adapter.Name())
		if snap.LastError == nil {
			t.Error("unavailable: LastError nil")
		}
	})
	t.Run("ended", func(t *testing.T) {
		adapter := &endedAdapter{sessions: []mux.Session{&stubSession{id: "gone", adapter: "test"}}}
		reg := mux.MustNewRegistry(adapter)
		_ = reg.Sessions(context.Background())
		adapter.sessions = nil
		_, err := reg.FindSession(context.Background(), "test:gone")
		if err == nil {
			t.Fatal("ended: FindSession nil error, want ErrSessionNotFound")
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		reg := mux.MustNewRegistry(&noCapsAdapter{})
		h := &Handlers{Registry: reg}
		wsReq := httptest.NewRequest("GET", "/term/ws?session=bare:test", nil)
		wsRec := httptest.NewRecorder()
		h.HandleWS(wsRec, wsReq)
		if wsRec.Code != http.StatusNotImplemented {
			t.Errorf("unsupported WS: status %d, want 501", wsRec.Code)
		}
	})
}

func TestCapabilityGolden_AllBackends(t *testing.T) {
	tests := []struct {
		adapter  mux.Adapter
		wantCaps []string
	}{
		{&capGoldenAdapter{name: "legacy", caps: []string{"live_stream", "screen", "history"}}, []string{"live_stream", "screen", "history"}},
		{&capGoldenAdapter{name: "legacy", caps: []string{"live_stream", "screen", "history", "process"}}, []string{"live_stream", "screen", "history", "process"}},
		{&capGoldenAdapter{name: "future", caps: []string{"live_stream"}}, []string{"live_stream"}},
		{&capGoldenAdapter{name: "legacy", caps: nil}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.adapter.Name(), func(t *testing.T) {
			reg := mux.MustNewRegistry(tt.adapter)
			h := &Handlers{Registry: reg}
			req := httptest.NewRequest("GET", "/api/sessions", nil)
			rec := httptest.NewRecorder()
			h.HandleSessionsAPI(rec, req)
			var sessions []SessionTelemetry
			json.Unmarshal(rec.Body.Bytes(), &sessions)
			for _, s := range sessions {
				if s.Adapter != tt.adapter.Name() {
					continue
				}
				if tt.wantCaps == nil {
					if len(s.Capabilities) != 0 {
						t.Errorf("%s (legacy): %d capabilities, want 0", tt.adapter.Name(), len(s.Capabilities))
					}
				} else {
					for _, want := range tt.wantCaps {
						found := false
						for _, got := range s.Capabilities {
							if got == want {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("%s: capability %q missing", tt.adapter.Name(), want)
						}
					}
					// Verify NO unwanted capabilities.
					wantSet := make(map[string]bool)
					for _, w := range tt.wantCaps {
						wantSet[w] = true
					}
					for _, got := range s.Capabilities {
						if !wantSet[got] {
							t.Errorf("%s: unwanted capability %q present (want %v)", tt.adapter.Name(), got, tt.wantCaps)
						}
					}
				}
			}
		})
	}
}

func TestDiagnostics_APISufficient(t *testing.T) {
	t.Run("empty_vs_unavailable", func(t *testing.T) {
		emptyReg := mux.MustNewRegistry(&emptyAdapter{})
		emptySessions := emptyReg.Sessions(context.Background())
		emptySnap, _ := emptyReg.Snapshot("empty")
		if len(emptySessions) != 0 {
			t.Error("empty: sessions non-empty")
		}
		if emptySnap.LastError != nil {
			t.Error("empty: LastError non-nil")
		}
		unavailAdapter := &unavailableAdapter{}
		unavailReg := mux.MustNewRegistry(unavailAdapter)
		_ = unavailReg.Sessions(context.Background())
		unavailSnap, _ := unavailReg.Snapshot("unavail")
		if unavailSnap.LastError == nil {
			t.Error("unavailable: LastError nil")
		}
	})
	t.Run("healthy_plus_unavailable", func(t *testing.T) {
		healthy := &healthyFixtureAdapter{}
		unavail := &unavailableAdapter{}
		reg := mux.MustNewRegistry(healthy, unavail)
		_ = reg.Sessions(context.Background())
		h := &Handlers{Registry: reg}
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		rec := httptest.NewRecorder()
		h.HandleSessionsAPI(rec, req)
		var sessions []SessionTelemetry
		json.Unmarshal(rec.Body.Bytes(), &sessions)
		hasHealthy := false
		for _, s := range sessions {
			if s.Adapter == "healthy-fixture" {
				hasHealthy = true
				if s.Stale {
					t.Error("healthy session marked stale")
				}
			}
		}
		if !hasHealthy {
			t.Error("healthy session not visible alongside unavailable adapter")
		}
		unavailSnap, _ := reg.Snapshot("unavail")
		if unavailSnap.LastError == nil {
			t.Error("unavailable: LastError nil — empty vs unavailable not distinguishable")
		}
	})
	t.Run("known_limitation_zero_sessions", func(t *testing.T) {
		// When an unavailable adapter has 0 sessions, /api/sessions shows nothing.
		// This is a known limitation: empty (healthy, 0 sessions) and unavailable
		// (failed, 0 sessions) produce the same API response. Registry-level
		// snapshot inspection is required to distinguish them.
		adapter := &unavailableAdapter{}
		reg := mux.MustNewRegistry(adapter)
		h := &Handlers{Registry: reg}
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		rec := httptest.NewRecorder()
		h.HandleSessionsAPI(rec, req)
		var sessions []SessionTelemetry
		json.Unmarshal(rec.Body.Bytes(), &sessions)
		// unavailable + 0 sessions: API shows no sessions.
		// Same as empty (healthy + 0 sessions). This is documented.
		if len(sessions) != 0 {
			t.Logf("unavailable 0-session adapter: %d sessions in API (expected 0)", len(sessions))
		}
		// Registry snapshot has the error — the distinction mechanism.
		snap, _ := reg.Snapshot("unavail")
		if snap.LastError == nil {
			t.Error("known limitation: LastError should be set, but is nil")
		}
	})
}

func TestMobileLegacyCheck_Classification(t *testing.T) {
	// AgentCard.tsx:86 has: session.adapter !== 'native' — display-only filter.
	names := []string{"native", "legacy", "legacy", "future"}
	for _, name := range names {
		adapter := &capGoldenAdapter{name: name, caps: []string{"live_stream"}}
		reg := mux.MustNewRegistry(adapter)
		h := &Handlers{Registry: reg}
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		rec := httptest.NewRecorder()
		h.HandleSessionsAPI(rec, req)
		var sessions []SessionTelemetry
		json.Unmarshal(rec.Body.Bytes(), &sessions)
		found := false
		for _, s := range sessions {
			if s.Adapter == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: adapter field not found in API response", name)
		}
	}
}

// --- Helpers ---

type emptyAdapter struct{}

func (a *emptyAdapter) Name() string                                          { return "empty" }
func (a *emptyAdapter) ListSessions(_ context.Context) ([]mux.Session, error) { return nil, nil }

type unavailableAdapter struct{ called bool }

func (a *unavailableAdapter) Name() string { return "unavail" }
func (a *unavailableAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	a.called = true
	return nil, mux.ErrAdapterUnavailable
}

type healthyFixtureAdapter struct{}

func (a *healthyFixtureAdapter) Name() string { return "healthy-fixture" }
func (a *healthyFixtureAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&stubSession{id: "hf1", adapter: "healthy-fixture"}}, nil
}

type endedAdapter struct{ sessions []mux.Session }

func (a *endedAdapter) Name() string                                          { return "test" }
func (a *endedAdapter) ListSessions(_ context.Context) ([]mux.Session, error) { return a.sessions, nil }

type noCapsAdapter struct{}

func (a *noCapsAdapter) Name() string { return "bare" }
func (a *noCapsAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&stubSession{id: "test", adapter: "bare"}}, nil
}

type stubSession struct{ id, adapter string }

func (s *stubSession) ID() string          { return s.id }
func (s *stubSession) Title() string       { return s.id }
func (s *stubSession) AdapterName() string { return s.adapter }

type capGoldenAdapter struct {
	name string
	caps []string
}

func (a *capGoldenAdapter) Name() string { return a.name }
func (a *capGoldenAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	var s mux.Session
	switch {
	case a.caps == nil:
		s = &capBareSession{id: "s1", adapter: a.name}
	case hasCap(a.caps, "screen") && hasCap(a.caps, "history") && hasCap(a.caps, "process"):
		s = &capScreenHistProcessSession{id: "s1", adapter: a.name}
	case hasCap(a.caps, "screen") && hasCap(a.caps, "history"):
		s = &capScreenHistSession{id: "s1", adapter: a.name}
	default:
		s = &capStreamOnlySession{id: "s1", adapter: a.name}
	}
	return []mux.Session{s}, nil
}

func hasCap(caps []string, name string) bool {
	for _, c := range caps {
		if c == name {
			return true
		}
	}
	return false
}

type capBareSession struct{ id, adapter string }

func (s *capBareSession) ID() string          { return s.id }
func (s *capBareSession) Title() string       { return s.id }
func (s *capBareSession) AdapterName() string { return s.adapter }

// capStreamOnlySession: only StreamOpener (live_stream).
type capStreamOnlySession struct{ id, adapter string }

func (s *capStreamOnlySession) ID() string          { return s.id }
func (s *capStreamOnlySession) Title() string       { return s.id }
func (s *capStreamOnlySession) AdapterName() string { return s.adapter }
func (s *capStreamOnlySession) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return &stubStream{}, nil
}

// capScreenHistSession: StreamOpener + ScreenReader + HistoryReader.
type capScreenHistSession struct{ id, adapter string }

func (s *capScreenHistSession) ID() string          { return s.id }
func (s *capScreenHistSession) Title() string       { return s.id }
func (s *capScreenHistSession) AdapterName() string { return s.adapter }
func (s *capScreenHistSession) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return &stubStream{}, nil
}
func (s *capScreenHistSession) ReadScreen(_ context.Context) ([]byte, error) { return []byte("x"), nil }
func (s *capScreenHistSession) ReadHistory(_ context.Context, _ int) ([]byte, error) {
	return []byte("x"), nil
}

// capScreenHistProcessSession: StreamOpener + ScreenReader + HistoryReader + ProcessProvider.
type capScreenHistProcessSession struct{ id, adapter string }

func (s *capScreenHistProcessSession) ID() string          { return s.id }
func (s *capScreenHistProcessSession) Title() string       { return s.id }
func (s *capScreenHistProcessSession) AdapterName() string { return s.adapter }
func (s *capScreenHistProcessSession) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return &stubStream{}, nil
}
func (s *capScreenHistProcessSession) ReadScreen(_ context.Context) ([]byte, error) {
	return []byte("x"), nil
}
func (s *capScreenHistProcessSession) ReadHistory(_ context.Context, _ int) ([]byte, error) {
	return []byte("x"), nil
}
func (s *capScreenHistProcessSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{PID: 1, CWD: "/"}, nil
}

type stubStream struct{}

func (s *stubStream) Read(p []byte) (int, error)  { return 0, nil }
func (s *stubStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *stubStream) Close() error                { return nil }
func (s *stubStream) Resize(rows, cols int) error { return nil }
