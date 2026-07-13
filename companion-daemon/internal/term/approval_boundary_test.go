package term

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/devicetrust"
)

// A1-D — authenticated exact-action execution boundary tests. These exercise the
// hardened handler: strict bounded decode, runtime-identity revalidation before
// delivery, at-most-once concurrency, and the composed device-auth route.

func localH(store *AuthoritativeApprovalStore, cmds CommandBroker) *Handlers {
	return &Handlers{Approvals: store, Cmds: cmds, InsecureLocalOnly: true}
}

// ── strict bounded decode ──

func TestBoundary_RejectsUnknownFields(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := localH(store, NewCommandBroker())
	rr := doAction(h, "codex:s1", "a1", `{"action":"reject","evil":"x"}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("unknown field code=%d want 400", rr.Code)
	}
	if snap, _ := store.LookupRecord("codex:s1", "a1"); snap.State != ApprovalPending {
		t.Errorf("unknown-field request mutated state: %q", snap.State)
	}
}

func TestBoundary_RejectsTrailingData(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := localH(store, NewCommandBroker())
	if rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}{"action":"reject"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("trailing data code=%d want 400", rr.Code)
	}
}

func TestBoundary_RejectsOversizedInput(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	opts := []agent.InteractionOption{{ID: "send", Kind: "neutral", Input: &agent.InputSchema{Placement: "as_payload"}}}
	seedApproval(store, "codex:s1", "a1", opts)
	h := localH(store, NewCommandBroker())
	big := strings.Repeat("A", maxApprovalInputBytes+1)
	body := fmt.Sprintf(`{"action":"send","input":%q}`, big)
	if rr := doAction(h, "codex:s1", "a1", body); rr.Code != http.StatusBadRequest {
		t.Errorf("oversized input code=%d want 400", rr.Code)
	}
}

func TestBoundary_InvalidUTF8InputNormalizedSafely(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	cmds := NewCommandBroker()
	opts := []agent.InteractionOption{{ID: "send", Kind: "neutral", Input: &agent.InputSchema{Placement: "as_payload"}}}
	seedApproval(store, "codex:s1", "a1", opts)
	h := localH(store, cmds)
	// Raw invalid UTF-8 bytes inside the JSON string. Go's json decoder normalizes
	// them to U+FFFD, so the decoded input is always valid UTF-8 — the safety
	// property that matters is that NO invalid UTF-8 ever reaches the terminal.
	body := "{\"action\":\"send\",\"input\":\"\xff\xfe\"}"
	rr := doAction(h, "codex:s1", "a1", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d want 200 (%s)", rr.Code, rr.Body.String())
	}
	if got := cmds.Take("codex:s1"); got != nil && !utf8.Valid(got) {
		t.Errorf("invalid UTF-8 reached the terminal: %q", got)
	}
}

// ── runtime-identity revalidation ──

func TestBoundary_LaunchReplacedBeforeAction(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	cmds := NewCommandBroker()
	seedApproval(store, "codex:s1", "a1", codexOpts()) // seeded at LaunchGen 1
	h := localH(store, cmds)
	// Live launch generation moved to 2 → the request is stale authority.
	h.LaunchGenOf = func(string) (int64, bool) { return 2, true }

	rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`)
	if rr.Code != http.StatusConflict { // stale_generation → 409
		t.Fatalf("stale launch code=%d want 409 (%s)", rr.Code, rr.Body.String())
	}
	if cmd := cmds.Take("codex:s1"); cmd != nil {
		t.Errorf("stale action must emit no command, got %q", cmd)
	}
	// The stale record is invalidated so a retry also fails closed.
	if snap, _ := store.LookupRecord("codex:s1", "a1"); snap.State != ApprovalInvalidated {
		t.Errorf("stale record state=%q want invalidated", snap.State)
	}
}

func TestBoundary_LaunchDisappearedBeforeAction(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := localH(store, NewCommandBroker())
	h.LaunchGenOf = func(string) (int64, bool) { return 0, false } // no live launch
	if rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusConflict {
		t.Errorf("disappeared launch code=%d want 409", rr.Code)
	}
}

func TestBoundary_MatchingLaunchProceeds(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := localH(store, NewCommandBroker())
	h.LaunchGenOf = func(string) (int64, bool) { return 1, true } // matches seed
	if rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusOK {
		t.Errorf("matching launch code=%d want 200 (%s)", rr.Code, rr.Body.String())
	}
}

// ── expired between lookup and commit (store-level atomic re-check) ──

func TestBoundary_ExpiredBetweenLookupAndReserve(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	base := time.Unix(1000, 0)
	store.now = func() time.Time { return base }
	seedApproval(store, "codex:s1", "a1", codexOpts())
	// LookupRecord succeeds while pending; then time jumps past expiry before the
	// reservation. Reserve applies lazy expiry atomically → expired, no delivery.
	snap, ok := store.LookupRecord("codex:s1", "a1")
	if !ok || snap.State != ApprovalPending {
		t.Fatal("setup: expected pending")
	}
	store.now = func() time.Time { return base.Add(authApprovalExpiry + time.Second) }
	if _, oc := store.Reserve("codex:s1", "a1"); oc != OutcomeExpired {
		t.Errorf("reserve after expiry oc=%q want expired", oc)
	}
}

// ── concurrent double-submit executes at most once ──

func TestBoundary_ConcurrentDuplicateAtMostOnce(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	// A fire-and-forget option so a successful action actually emits a command; we
	// count how many commands the broker ever receives across concurrent submits.
	opts := []agent.InteractionOption{{ID: "send", Kind: "neutral", Input: &agent.InputSchema{Placement: "as_payload"}}}
	seedApproval(store, "codex:s1", "a1", opts)
	var puts int64
	cmds := &countingBroker{inner: NewCommandBroker(), puts: &puts}
	h := localH(store, cmds)

	const n = 24
	var ok, conflict int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr := doAction(h, "codex:s1", "a1", `{"action":"send","input":"x"}`)
			switch rr.Code {
			case http.StatusOK:
				atomic.AddInt64(&ok, 1)
			case http.StatusConflict:
				atomic.AddInt64(&conflict, 1)
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Errorf("exactly one submit must succeed, got %d", ok)
	}
	if conflict != n-1 {
		t.Errorf("the other %d submits must conflict, got %d", n-1, conflict)
	}
	if got := atomic.LoadInt64(&puts); got != 1 {
		t.Errorf("delivery must occur at most once, got %d Puts", got)
	}
}

type countingBroker struct {
	inner CommandBroker
	puts  *int64
}

func (c *countingBroker) Put(sessionID string, command []byte) {
	atomic.AddInt64(c.puts, 1)
	c.inner.Put(sessionID, command)
}
func (c *countingBroker) Take(sessionID string) []byte { return c.inner.Take(sessionID) }

// ── cross-session isolation at the boundary ──

func TestBoundary_CrossSessionActionNotFound(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := localH(store, NewCommandBroker())
	// Same approval ID but a DIFFERENT session path must not resolve s1's request.
	if rr := doAction(h, "codex:s2", "a1", `{"action":"reject"}`); rr.Code != http.StatusNotFound {
		t.Errorf("cross-session action code=%d want 404", rr.Code)
	}
	if snap, _ := store.LookupRecord("codex:s1", "a1"); snap.State != ApprovalPending {
		t.Errorf("cross-session action mutated s1: %q", snap.State)
	}
}

// ── composed device-auth route (production remote path) ──

func routedAction(m *devicetrust.DeviceSessionManager, h *Handlers, bearer, sessionID, approvalID, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/sessions/{id}/approvals/{approvalId}",
		devicetrust.RequirePrincipal(m, h.HandleApprovalAction, devicetrust.PermTerminalInput))
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/approvals/"+approvalID, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestBoundary_Route_OwnerBearerSucceeds(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	raw, _, _, err := m.CreateAfterVerifiedChallenge("dev-owner", "host", "boot", devicetrust.PermissionsForRole(devicetrust.RoleOwner))
	if err != nil {
		t.Fatal(err)
	}
	// Production remote handler: NOT insecure-local, so it binds the device principal.
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker()}
	if rr := routedAction(m, h, raw, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusOK {
		t.Errorf("owner bearer code=%d want 200 (%s)", rr.Code, rr.Body.String())
	}
}

func TestBoundary_Route_MissingBearerUnauthorized_NoMutation(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker()}
	if rr := routedAction(m, h, "", "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusUnauthorized {
		t.Errorf("missing bearer code=%d want 401", rr.Code)
	}
	// A rejected-at-auth request must never touch the store.
	if snap, _ := store.LookupRecord("codex:s1", "a1"); snap.State != ApprovalPending {
		t.Errorf("unauthorized request mutated store: %q", snap.State)
	}
}

func TestBoundary_Route_MemberForbidden(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	// A member device lacks PermTerminalInput.
	raw, _, _, _ := m.CreateAfterVerifiedChallenge("dev-member", "host", "boot", devicetrust.PermissionsForRole(devicetrust.RoleMember))
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker()}
	if rr := routedAction(m, h, raw, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusForbidden {
		t.Errorf("member bearer code=%d want 403", rr.Code)
	}
	if snap, _ := store.LookupRecord("codex:s1", "a1"); snap.State != ApprovalPending {
		t.Errorf("forbidden request mutated store: %q", snap.State)
	}
}

func TestBoundary_Route_RevokedBearerUnauthorized(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	raw, _, _, _ := m.CreateAfterVerifiedChallenge("dev-owner", "host", "boot", devicetrust.PermissionsForRole(devicetrust.RoleOwner))
	// A replacement session for the same device revokes the old bearer.
	_, _, _, _ = m.CreateAfterVerifiedChallenge("dev-owner", "host", "boot", devicetrust.PermissionsForRole(devicetrust.RoleOwner))
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker()}
	if rr := routedAction(m, h, raw, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusUnauthorized {
		t.Errorf("revoked bearer code=%d want 401", rr.Code)
	}
}

// A production (non-local) handler reached WITHOUT a device principal in context
// (e.g. misconfigured routing) must fail closed at the handler's auth-context bind.
func TestBoundary_NonLocalWithoutPrincipalForbidden(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker()} // InsecureLocalOnly=false, no RequirePrincipal wrapper
	if rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusForbidden {
		t.Errorf("non-local no-principal code=%d want 403", rr.Code)
	}
}
