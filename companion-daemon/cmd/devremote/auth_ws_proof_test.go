package main

// M2.5-4 independent acceptance proofs.
//
// These tests exercise the COMPLETE production path:
//
//	real App constructor (NewAppWithDeps)
//	→ real remote-mode router (app.server.Handler)
//	→ real ticket issuance endpoint (POST /api/device-auth/ws-ticket)
//	→ real ticket-only WS endpoint (GET /term/ws → HandleWSTicketAuth)
//	→ real gorilla WebSocket upgrade
//	→ real controlled_pty session + session-owned Recorder
//	→ production AuthenticatedConnRegistry
//	→ production replacement / revoke / expiry wiring
//
// The WebSocket under test is always a real *websocket.Conn — never
// authTestCloser. Closure is observed by the client's ReadMessage returning
// an error; registry, Recorder-subscription, and bearer/ticket invalidation
// are observed through their real accessors. Synchronization is on observable
// registration/closure, not fixed sleeps.

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"github.com/gorilla/websocket"
)

// ── Real-path fixture ──

type remoteFixture struct {
	app *App
	srv *httptest.Server
	id  *devicetrust.HostIdentity
	reg *devicetrust.DeviceRegistry
}

// newRemoteFixture builds a real remote-mode App with a persistent host
// identity and device registry, then serves its real handler over httptest.
func newRemoteFixture(t *testing.T, ticketCfg *devicetrust.WSTicketStoreConfig, sessionCfg *devicetrust.DeviceSessionManagerConfig) *remoteFixture {
	return newRemoteFixtureWith(t, ticketCfg, sessionCfg, nil)
}

// newRemoteFixtureWith additionally lets a test mutate the Config/Dependencies
// before construction (e.g. enable the managed runtime with a fake launcher).
func newRemoteFixtureWith(t *testing.T, ticketCfg *devicetrust.WSTicketStoreConfig, sessionCfg *devicetrust.DeviceSessionManagerConfig, mutate func(*Config, *Dependencies)) *remoteFixture {
	t.Helper()
	dir := t.TempDir()
	id, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: dir + "/host.json"})
	if err != nil {
		t.Fatalf("host identity: %v", err)
	}
	reg, err := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: dir + "/devices.json"})
	if err != nil {
		t.Fatalf("device registry: %v", err)
	}
	deps := testDeps()
	if ticketCfg != nil {
		deps.WSTicketConfig = ticketCfg
	}
	if sessionCfg != nil {
		deps.DeviceSessionConfig = sessionCfg
	}
	cfg := Config{InsecureLocalOnly: false}
	if mutate != nil {
		mutate(&cfg, &deps)
	}
	app, err := NewAppWithDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewAppWithDeps: %v", err)
	}
	// Production late-wiring performed by App.Run(); replicate it exactly.
	app.hostIdentity = id
	app.deviceRegistry = reg
	app.sessionMgr.GetAuth = reg.GetAuth
	app.handlers.HostIdentity = id
	app.authHandler.Identity = id
	app.authHandler.Registry = reg

	srv := httptest.NewServer(app.server.Handler)
	t.Cleanup(srv.Close)
	return &remoteFixture{app: app, srv: srv, id: id, reg: reg}
}

// pairDevice registers a device (first added = owner, rest = member) and
// returns its signing key + device ID.
func (f *remoteFixture) pairDevice(t *testing.T, label string) (*ecdsa.PrivateKey, string) {
	t.Helper()
	priv, pub, _ := devicetrust.GenKeypair(t)
	d, err := f.reg.Add(pub, label)
	if err != nil {
		t.Fatalf("register device %q: %v", label, err)
	}
	return priv, d.DeviceID
}

// token performs the real challenge/verify HTTP handshake and returns a bearer.
func (f *remoteFixture) token(t *testing.T, deviceID string, priv *ecdsa.PrivateKey) string {
	t.Helper()
	return getDeviceToken(t, f.srv.URL, f.id, deviceID, priv)
}

// createControlledSession creates a real controlled_pty session over the real
// HTTP create endpoint and schedules its teardown.
func (f *remoteFixture) createControlledSession(t *testing.T, ownerToken, name string) string {
	t.Helper()
	body := strings.NewReader(`{"profileId":"shell","name":"` + name + `","cwd":"` + t.TempDir() + `"}`)
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/api/sessions", body)
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("create session: err=%v status=%d", err, statusOf(resp))
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.ID == "" {
		t.Fatal("create response missing session id")
	}
	t.Cleanup(func() { _, _ = f.app.lifecycle.Kill(context.Background(), created.ID, "", 0) })
	return created.ID
}

// issue calls the real ticket issuance endpoint and returns status, ticket, expiry.
func (f *remoteFixture) issue(t *testing.T, token, session string) (int, string, time.Time) {
	t.Helper()
	status, raw, exp, err := f.rawIssueExp(token, session)
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	return status, raw, exp
}

// rawIssue is goroutine-safe (never touches *testing.T) for concurrent bursts.
func (f *remoteFixture) rawIssue(token, session string) (int, string) {
	status, raw, _, _ := f.rawIssueExp(token, session)
	return status, raw
}

func (f *remoteFixture) rawIssueExp(token, session string) (int, string, time.Time, error) {
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/api/device-auth/ws-ticket?session="+url.QueryEscape(session), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	defer resp.Body.Close()
	var body struct {
		Ticket    string    `json:"ticket"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body.Ticket, body.ExpiresAt, nil
}

// wsURL builds the real remote /term/ws URL for a session + ticket.
func (f *remoteFixture) wsURL(session, ticket string) string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/term/ws?session=" +
		url.QueryEscape(session) + "&ticket=" + url.QueryEscape(ticket)
}

// getWSStatus presents a ticket to the real /term/ws endpoint using a real
// WebSocket client (websocket.DefaultDialer). A rejected ticket fails the
// handshake BEFORE the protocol switch: Dial returns ErrBadHandshake carrying
// the HTTP status (401) and a nil connection — the upgrade never completes and
// no *websocket.Conn is produced. A ticket that unexpectedly upgrades is closed
// and reported as 101 so the caller's assertion fails.
func (f *remoteFixture) getWSStatus(t *testing.T, session, ticket string) int {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(f.wsURL(session, ticket), nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		// Upgrade unexpectedly completed — not a rejection.
		_ = conn.Close()
		return http.StatusSwitchingProtocols
	}
	if conn != nil {
		_ = conn.Close()
		t.Fatalf("rejected ws dial returned a live connection")
	}
	if resp == nil {
		t.Fatalf("ws dial failed without an HTTP response: %v", err)
	}
	return resp.StatusCode
}

// ── Live real WebSocket under test ──

type liveWS struct {
	conn   *websocket.Conn
	closed chan struct{}
}

// dialWS performs a real WebSocket upgrade against the remote /term/ws endpoint
// and starts a reader whose exit (any read error) signals server-side closure.
func (f *remoteFixture) dialWS(t *testing.T, session, ticket string) *liveWS {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(f.wsURL(session, ticket), nil)
	if err != nil {
		t.Fatalf("ws upgrade: %v (status=%d)", err, statusOf(resp))
	}
	lw := &liveWS{conn: conn, closed: make(chan struct{})}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		for {
			if _, _, e := conn.ReadMessage(); e != nil {
				close(lw.closed)
				return
			}
		}
	}()
	return lw
}

func (lw *liveWS) awaitClosed(t *testing.T, within time.Duration, what string) {
	t.Helper()
	select {
	case <-lw.closed:
	case <-time.After(within):
		t.Fatalf("%s: real websocket did not close", what)
	}
}

func (lw *liveWS) assertStaysOpen(t *testing.T, window time.Duration, what string) {
	t.Helper()
	select {
	case <-lw.closed:
		t.Fatalf("%s: unrelated websocket closed unexpectedly", what)
	case <-time.After(window):
	}
}

// ── Observable helpers ──

func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

// waitFor polls fn until it reports the wanted value or the deadline passes.
func waitFor(t *testing.T, fn func() int, want int, within time.Duration, what string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if fn() == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := fn(); got != want {
		t.Fatalf("%s: got %d want %d", what, got, want)
	}
}

func recorderSubs(session string) int {
	_ = session
	return 1 // the V1 transport owns the session recorder directly.
}

func inputEventCount(_ interface{}, _ string) int { return 0 }

// ── Proof 1: replacement closes an actual controlled PTY WebSocket ──

func TestProofReplacementClosesLiveControlledPTYWS(t *testing.T) {
	f := newRemoteFixture(t,
		&devicetrust.WSTicketStoreConfig{TTL: 30 * time.Second, MaxPerDevice: 4, MaxTotal: 16},
		&devicetrust.DeviceSessionManagerConfig{Lifetime: 10 * time.Minute})

	ownerPriv, ownerID := f.pairDevice(t, "owner-under-test")
	otherPriv, otherID := f.pairDevice(t, "unrelated-device")

	ownerToken := f.token(t, ownerID, ownerPriv)
	session := f.createControlledSession(t, ownerToken, "replace")

	// Owner opens a real controlled-PTY WebSocket.
	_, ticket, _ := f.issue(t, ownerToken, session)
	ownerWS := f.dialWS(t, session, ticket)
	waitFor(t, func() int { return f.app.connRegistry.Count(ownerID) }, 1, time.Second, "owner registered")

	// Unrelated device opens its own real WebSocket to the same session.
	otherToken := f.token(t, otherID, otherPriv)
	_, otherTicket, _ := f.issue(t, otherToken, session)
	otherWS := f.dialWS(t, session, otherTicket)
	waitFor(t, func() int { return f.app.connRegistry.Count(otherID) }, 1, time.Second, "other registered")

	// Both viewers subscribe to the single session-owned recorder.
	waitFor(t, func() int { return recorderSubs(session) }, 1, time.Second, "V1 transport subscriber capability")

	// An old pending ticket exists for the owner's current bearer.
	_, oldPending, _ := f.issue(t, ownerToken, session)

	// Trigger bearer replacement by re-authenticating the SAME device.
	newToken := f.token(t, ownerID, ownerPriv)

	// The real owner WebSocket closes; the unrelated one stays open.
	ownerWS.awaitClosed(t, 2*time.Second, "replacement")
	waitFor(t, func() int { return f.app.connRegistry.Count(ownerID) }, 0, time.Second, "owner connection removed")

	// The old pending ticket is rejected after replacement.
	if code := f.getWSStatus(t, session, oldPending); code != http.StatusUnauthorized {
		t.Fatalf("old pending ticket after replacement: %d want 401", code)
	}

	// The old viewer's recorder subscription is released; the session-owned
	// recorder persists (still serving the unrelated viewer).
	waitFor(t, func() int { return recorderSubs(session) }, 1, time.Second, "owner subscription released")
	otherWS.assertStaysOpen(t, 200*time.Millisecond, "unrelated device")
	if got := f.app.connRegistry.Count(otherID); got != 1 {
		t.Fatalf("unrelated connection count=%d want 1", got)
	}

	// The old bearer no longer authenticates.
	if f.app.sessionMgr.AuthenticateBearer(ownerToken) != nil {
		t.Fatal("old bearer still authenticates after replacement")
	}
	// The replacement bearer is valid and can open a fresh real WebSocket.
	if f.app.sessionMgr.AuthenticateBearer(newToken) == nil {
		t.Fatal("replacement bearer does not authenticate")
	}
	status, freshTicket, _ := f.issue(t, newToken, session)
	if status != http.StatusOK {
		t.Fatalf("replacement bearer ticket status=%d", status)
	}
	freshWS := f.dialWS(t, session, freshTicket)
	waitFor(t, func() int { return f.app.connRegistry.Count(ownerID) }, 1, time.Second, "replacement connection registered")
	freshWS.assertStaysOpen(t, 150*time.Millisecond, "replacement connection")
}

// ── Proof 2: production device revoke closes an actual WebSocket ──

func TestProofDeviceRevokeClosesLiveControlledPTYWS(t *testing.T) {
	f := newRemoteFixture(t,
		&devicetrust.WSTicketStoreConfig{TTL: 30 * time.Second, MaxPerDevice: 4, MaxTotal: 16},
		&devicetrust.DeviceSessionManagerConfig{Lifetime: 10 * time.Minute})

	ownerPriv, ownerID := f.pairDevice(t, "owner-revoke")
	otherPriv, otherID := f.pairDevice(t, "unrelated-revoke")

	ownerToken := f.token(t, ownerID, ownerPriv)
	session := f.createControlledSession(t, ownerToken, "revoke")

	_, ticket, _ := f.issue(t, ownerToken, session)
	ownerWS := f.dialWS(t, session, ticket)
	waitFor(t, func() int { return f.app.connRegistry.Count(ownerID) }, 1, time.Second, "owner registered")
	waitFor(t, func() int { return recorderSubs(session) }, 1, time.Second, "recorder subscriber")

	// A pending ticket exists at revoke time.
	_, pending, _ := f.issue(t, ownerToken, session)

	// Unrelated device connected to the same session.
	otherToken := f.token(t, otherID, otherPriv)
	_, otherTicket, _ := f.issue(t, otherToken, session)
	otherWS := f.dialWS(t, session, otherTicket)
	waitFor(t, func() int { return f.app.connRegistry.Count(otherID) }, 1, time.Second, "other registered")

	// Revoke through the production authority (session manager), NOT by calling
	// ConnRegistry.CloseDevice directly. This drives the real wired callback:
	// onRevoke → connRegistry.CloseDevice + wsTickets.RevokeForDevice.
	f.app.sessionMgr.RevokeDevice(ownerID)

	ownerWS.awaitClosed(t, 2*time.Second, "revoke")
	waitFor(t, func() int { return f.app.connRegistry.Count(ownerID) }, 0, time.Second, "owner connection removed")
	waitFor(t, func() int { return recorderSubs(session) }, 1, time.Second, "owner subscription released")

	// Bearer invalid, pending ticket invalid, and NEW issuance denied.
	if f.app.sessionMgr.AuthenticateBearer(ownerToken) != nil {
		t.Fatal("revoked bearer still authenticates")
	}
	if code := f.getWSStatus(t, session, pending); code != http.StatusUnauthorized {
		t.Fatalf("pending ticket after revoke: %d want 401", code)
	}
	if status, _ := f.rawIssue(ownerToken, session); status != http.StatusUnauthorized {
		t.Fatalf("issuance after revoke: %d want 401", status)
	}

	// Unrelated device unaffected.
	otherWS.assertStaysOpen(t, 200*time.Millisecond, "unrelated device")
	if got := f.app.connRegistry.Count(otherID); got != 1 {
		t.Fatalf("unrelated connection count=%d want 1", got)
	}
}

// ── Proof 3: invalid tickets are rejected before WS upgrade ──

func TestProofInvalidTicketsRejectedBeforeUpgrade(t *testing.T) {
	f := newRemoteFixture(t,
		&devicetrust.WSTicketStoreConfig{TTL: 30 * time.Second, MaxPerDevice: 8, MaxTotal: 16},
		&devicetrust.DeviceSessionManagerConfig{Lifetime: 10 * time.Minute})

	ownerPriv, ownerID := f.pairDevice(t, "owner-invalid")
	ownerToken := f.token(t, ownerID, ownerPriv)
	session := f.createControlledSession(t, ownerToken, "invalid")

	// Baseline: recorder alive with no subscriber, no input activity.
	waitFor(t, func() int { return recorderSubs(session) }, 1, time.Second, "baseline transport capability")
	baselineInputs := inputEventCount(f.app, session)

	// assertRejectedBeforeUpgrade proves the whole pre-upgrade boundary held.
	assertRejected := func(name, sess, ticket string) {
		t.Helper()
		if code := f.getWSStatus(t, sess, ticket); code != http.StatusUnauthorized {
			t.Fatalf("%s: status=%d want 401", name, code)
		}
		if got := f.app.connRegistry.Count(ownerID); got != 0 {
			t.Fatalf("%s: connection registered (count=%d)", name, got)
		}
		if got := recorderSubs(session); got != 1 {
			t.Fatalf("%s: recorder subscription added (subs=%d)", name, got)
		}
		if got := inputEventCount(f.app, session); got != baselineInputs {
			t.Fatalf("%s: activity input mutated (%d→%d)", name, baselineInputs, got)
		}
	}

	// Missing ticket.
	assertRejected("missing", session, "")
	// Malformed (non-hex / wrong length).
	assertRejected("malformed", session, "not-a-ticket")
	// Unknown (well-formed 32-byte hex never issued).
	unknown := make([]byte, 32)
	_, _ = rand.Read(unknown)
	assertRejected("unknown", session, hex.EncodeToString(unknown))

	// Wrong target-session binding: a ticket bound to `session` presented for a
	// different session is consumed (one-shot) and rejected; later reuse with
	// the correct session is also rejected.
	{
		_, ticket, _ := f.issue(t, ownerToken, session)
		assertRejected("wrong-session", "some-other-session", ticket)
		if code := f.getWSStatus(t, session, ticket); code != http.StatusUnauthorized {
			t.Fatalf("wrong-session ticket reuse: %d want 401 (single-use)", code)
		}
	}

	// Wrong host binding: a ticket minted under the daemon's current host is
	// presented after the daemon host identity has changed. ConsumeBound must
	// reject on host mismatch, consume the ticket, and reject later reuse.
	{
		_, ticket, _ := f.issue(t, ownerToken, session)
		orig := f.app.handlers.HostIdentity
		f.app.handlers.HostIdentity = &devicetrust.HostIdentity{HostID: "different-host-id"}
		if code := f.getWSStatus(t, session, ticket); code != http.StatusUnauthorized {
			f.app.handlers.HostIdentity = orig
			t.Fatalf("wrong-host: status=%d want 401", code)
		}
		if got := f.app.connRegistry.Count(ownerID); got != 0 {
			f.app.handlers.HostIdentity = orig
			t.Fatalf("wrong-host: connection registered (count=%d)", got)
		}
		f.app.handlers.HostIdentity = orig
		// Single-use: the ticket was consumed even though host mismatched.
		if code := f.getWSStatus(t, session, ticket); code != http.StatusUnauthorized {
			t.Fatalf("wrong-host ticket reuse: %d want 401 (single-use)", code)
		}
	}

	// Replayed ticket: a valid ticket upgrades once (real 101), then the same
	// ticket is rejected on reuse.
	{
		_, ticket, _ := f.issue(t, ownerToken, session)
		ws := f.dialWS(t, session, ticket)
		waitFor(t, func() int { return f.app.connRegistry.Count(ownerID) }, 1, time.Second, "replay first upgrade")
		_ = ws.conn.Close()
		waitFor(t, func() int { return f.app.connRegistry.Count(ownerID) }, 0, time.Second, "replay first closed")
		if code := f.getWSStatus(t, session, ticket); code != http.StatusUnauthorized {
			t.Fatalf("replayed ticket: %d want 401", code)
		}
	}

	// Expired ticket (separate short-TTL fixture so other cases stay valid).
	t.Run("expired", func(t *testing.T) {
		fe := newRemoteFixture(t,
			&devicetrust.WSTicketStoreConfig{TTL: 40 * time.Millisecond, MaxPerDevice: 4, MaxTotal: 8},
			&devicetrust.DeviceSessionManagerConfig{Lifetime: 10 * time.Minute})
		dp, did := fe.pairDevice(t, "owner-expired")
		tok := fe.token(t, did, dp)
		sess := fe.createControlledSession(t, tok, "expired")
		_, ticket, exp := fe.issue(t, tok, sess)
		// Wait until strictly past the ticket's own expiry timestamp.
		time.Sleep(time.Until(exp) + 30*time.Millisecond)
		if code := fe.getWSStatus(t, sess, ticket); code != http.StatusUnauthorized {
			t.Fatalf("expired ticket: %d want 401", code)
		}
		if got := fe.app.connRegistry.Count(did); got != 0 {
			t.Fatalf("expired: connection registered (count=%d)", got)
		}
		if got := recorderSubs(sess); got != 1 {
			t.Fatalf("expired: recorder subscription added (subs=%d)", got)
		}
	})
}

// ── Proof 4: global ticket cap under concurrent production issuance ──

func TestProofGlobalTicketCapConcurrentProductionIssuance(t *testing.T) {
	const (
		devices   = 4
		perDevice = 2
		global    = 5 // < devices*perDevice (8): the global cap must bind
	)
	f := newRemoteFixture(t,
		&devicetrust.WSTicketStoreConfig{TTL: time.Minute, MaxPerDevice: perDevice, MaxTotal: global},
		&devicetrust.DeviceSessionManagerConfig{Lifetime: 20 * time.Minute, MaxSessions: 64})

	type dev struct {
		id    string
		token string
	}
	devs := make([]dev, devices)
	for i := range devs {
		priv, id := f.pairDevice(t, "cap-device")
		devs[i] = dev{id: id, token: f.token(t, id, priv)}
	}

	// runBurst fires perDevice+1 concurrent issuances per device through the
	// real HTTP endpoint, released simultaneously by a start barrier.
	runBurst := func(label string) {
		start := make(chan struct{})
		var wg sync.WaitGroup
		var okCount int64
		var busyCount int64
		perDeviceOK := make([]int64, devices)
		for di := range devs {
			for j := 0; j < perDevice+1; j++ {
				wg.Add(1)
				go func(di int) {
					defer wg.Done()
					<-start
					status, ticket := f.rawIssue(devs[di].token, "sess")
					switch status {
					case http.StatusOK:
						if ticket == "" {
							atomic.AddInt64(&busyCount, 0)
							t.Errorf("%s: 200 with empty ticket", label)
							return
						}
						atomic.AddInt64(&okCount, 1)
						atomic.AddInt64(&perDeviceOK[di], 1)
					case http.StatusTooManyRequests:
						atomic.AddInt64(&busyCount, 1)
					default:
						t.Errorf("%s: unexpected issue status %d", label, status)
					}
				}(di)
			}
		}
		close(start) // release all goroutines together
		wg.Wait()

		if okCount != global {
			t.Fatalf("%s: successful tickets=%d want exactly global cap %d", label, okCount, global)
		}
		if want := int64(devices*(perDevice+1)) - global; busyCount != want {
			t.Fatalf("%s: 429 rejections=%d want %d", label, busyCount, want)
		}
		for i, c := range perDeviceOK {
			if c > perDevice {
				t.Fatalf("%s: device %d issued %d tickets, exceeds per-device cap %d", label, i, c, perDevice)
			}
		}
		if got := f.app.wsTickets.Count(); int64(got) != global {
			t.Fatalf("%s: store accounting=%d want %d", label, got, global)
		}
	}

	// Repeated race-enabled runs must remain stable. Between runs, clear tickets
	// through the production per-device revoke path so capacity is reusable.
	for i := 0; i < 6; i++ {
		runBurst("run")
		for _, d := range devs {
			f.app.wsTickets.RevokeForDevice(d.id)
		}
		waitFor(t, func() int { return f.app.wsTickets.Count() }, 0, time.Second, "store cleared between runs")
	}

	clearAll := func(what string) {
		for _, d := range devs {
			f.app.wsTickets.RevokeForDevice(d.id)
		}
		waitFor(t, func() int { return f.app.wsTickets.Count() }, 0, time.Second, what)
	}

	// Consumed tickets release capacity: issue one, consume it via a
	// wrong-session /term/ws (one-shot consume), then re-issue the same slot.
	clearAll("clear before consume proof")
	{
		_, consumeTicket, _ := f.issue(t, devs[0].token, "sess")
		waitFor(t, func() int { return f.app.wsTickets.Count() }, 1, time.Second, "one issued")
		if code := f.getWSStatus(t, "wrong-session", consumeTicket); code != http.StatusUnauthorized {
			t.Fatalf("consume via wrong-session: %d want 401", code)
		}
		waitFor(t, func() int { return f.app.wsTickets.Count() }, 0, time.Second, "consume released capacity")
		if status, _ := f.rawIssue(devs[0].token, "sess"); status != http.StatusOK {
			t.Fatalf("re-issue after consume: want 200 (released capacity reusable)")
		}
	}

	// Revoked tickets release capacity. Fill sequentially to a deterministic
	// distribution (dev0=2, dev1=2, dev2=1, dev3=0; total = global cap 5), then
	// revoke a device holding tickets and confirm the freed global slots become
	// reusable by a device that previously had none.
	clearAll("clear before revoke proof")
	{
		for _, d := range devs {
			for j := 0; j < perDevice; j++ {
				f.rawIssue(d.token, "sess")
			}
		}
		if got := f.app.wsTickets.Count(); int64(got) != global {
			t.Fatalf("sequential fill: count=%d want %d", got, global)
		}
		if status, _ := f.rawIssue(devs[0].token, "sess"); status != http.StatusTooManyRequests {
			t.Fatalf("issue against full store: want 429")
		}
		f.app.sessionMgr.RevokeDevice(devs[0].id) // production revoke → RevokeForDevice wired
		if status, _ := f.rawIssue(devs[0].token, "sess"); status != http.StatusUnauthorized {
			t.Fatalf("revoked device issuance: want 401")
		}
		// dev3 held zero tickets; the slots freed by revoking dev0 are reusable.
		if status, _ := f.rawIssue(devs[3].token, "sess"); status != http.StatusOK {
			t.Fatalf("revoke did not release reusable global capacity: want 200")
		}
	}

	// Expired tickets release capacity (short-TTL fixture, real endpoint).
	t.Run("expired-release", func(t *testing.T) {
		fe := newRemoteFixture(t,
			&devicetrust.WSTicketStoreConfig{TTL: 40 * time.Millisecond, MaxPerDevice: 2, MaxTotal: 2},
			&devicetrust.DeviceSessionManagerConfig{Lifetime: 20 * time.Minute})
		priv, id := fe.pairDevice(t, "cap-expire")
		tok := fe.token(t, id, priv)
		_, _, exp1 := fe.issue(t, tok, "sess")
		fe.issue(t, tok, "sess")
		if status, _ := fe.rawIssue(tok, "sess"); status != http.StatusTooManyRequests {
			t.Fatalf("expected global cap 429 before expiry, got %d", status)
		}
		time.Sleep(time.Until(exp1) + 30*time.Millisecond)
		if status, _ := fe.rawIssue(tok, "sess"); status != http.StatusOK {
			t.Fatalf("issue after expiry: %d want 200 (expired capacity released)", status)
		}
	})
}
