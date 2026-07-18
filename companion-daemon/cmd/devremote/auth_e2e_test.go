package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"github.com/gorilla/websocket"
)

func TestAuthProductionPath_UnavailableBeforePairing(t *testing.T) {
	dir := t.TempDir()
	// Host identity + empty device registry.
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(dir, "host.json")})
	reg, _ := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: filepath.Join(dir, "devices.json")})
	// Init device trust so the auth handler is wired.
	initAuth := func() { /* set via SetPairingContext-like path */ }
	_ = id
	_ = reg
	_ = initAuth

	// Build the App through the real constructor but with no paired device.
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	// Before any device is paired, the auth endpoints return 503.
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"version": 1, "hostId": "x", "deviceId": "y",
		"clientNonce": hex.EncodeToString(make([]byte, 32)),
	})
	resp, _ := http.Post(srv.URL+"/api/device-auth/challenge", "application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unpaired: challenge status=%d want 503, wiring broken?", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAuthProductionPath_ChallengeAndVerify(t *testing.T) {
	dir := t.TempDir()
	// Pre-create a host identity and a paired device.
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(dir, "host.json")})
	reg, _ := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: filepath.Join(dir, "devices.json")})
	devPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&devPriv.PublicKey)
	d, _ := reg.Add(pubDER, "e2e-device")

	// Build App with the device trust installed.
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.hostIdentity = id
	app.deviceRegistry = reg
	if app.authHandler != nil {
		app.authHandler.Identity = id
		app.authHandler.Registry = reg
	}

	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()

	// 1) Challenge.
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalBody, _ := json.Marshal(map[string]interface{}{
		"version": 1, "hostId": id.HostID, "deviceId": d.DeviceID,
		"clientNonce": hex.EncodeToString(clientNonce),
	})
	resp, err := http.Post(srv.URL+"/api/device-auth/challenge", "application/json", bytes.NewReader(chalBody))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("challenge: err=%v status=%d", err, resp.StatusCode)
	}
	var chalResp devicetrust.AuthChallengeResponse
	json.NewDecoder(resp.Body).Decode(&chalResp)
	resp.Body.Close()

	// 2) Verify.
	challengeID, _ := hex.DecodeString(chalResp.ChallengeID)
	serverNonce, _ := hex.DecodeString(chalResp.ServerNonce)
	createdAtMS := chalResp.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	expiresAtMS := chalResp.ExpiresAt.UnixMilli()
	transcript := devicetrust.AuthTranscript{Role: "device", HostID: chalResp.HostID,
		DeviceID: d.DeviceID, DaemonBootID: chalResp.DaemonBootID,
		ChallengeID: challengeID, ClientNonce: clientNonce, ServerNonce: serverNonce,
		CreatedAtMS: createdAtMS, ExpiresAtMS: expiresAtMS}
	digest := sha256.Sum256(transcript.Build())
	devSig, _ := ecdsa.SignASN1(rand.Reader, devPriv, digest[:])
	verifyBody, _ := json.Marshal(map[string]interface{}{
		"version": 1, "challengeId": chalResp.ChallengeID,
		"deviceId": d.DeviceID, "signature": hex.EncodeToString(devSig),
	})
	resp2, err := http.Post(srv.URL+"/api/device-auth/verify", "application/json", bytes.NewReader(verifyBody))
	if err != nil || resp2.StatusCode != 200 {
		t.Fatalf("verify: err=%v status=%d", err, resp2.StatusCode)
	}
	var tokResp devicetrust.VerifyResponse
	json.NewDecoder(resp2.Body).Decode(&tokResp)
	resp2.Body.Close()
	if tokResp.Token == "" || tokResp.TokenType != "Bearer" {
		t.Fatalf("token: %+v", tokResp)
	}
}

func TestRemoteMode_MemberCannotCreate(t *testing.T) {
	dir := t.TempDir()
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: dir + "/host.json"})
	reg, _ := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: dir + "/devices.json"})
	_, pubDER1, _ := devicetrust.GenKeypair(t)
	reg.Add(pubDER1, "owner") // first = owner
	devPriv, pubDER2, _ := devicetrust.GenKeypair(t)
	md, _ := reg.Add(pubDER2, "member")
	if md.Role != devicetrust.RoleMember {
		t.Fatal("expected member")
	}

	cfg := Config{InsecureLocalOnly: false}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.hostIdentity = id
	app.deviceRegistry = reg
	if app.authHandler != nil {
		app.authHandler.Identity = id
		app.authHandler.Registry = reg
	}
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()

	// Get a member bearer token.
	tok := getDeviceToken(t, srv.URL, id, md.DeviceID, devPriv)
	// POST /api/sessions (create) must fail for member.
	body := `{"profileId":"shell","name":"x","cwd":"/tmp"}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/sessions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member POST /api/sessions: %d want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func getDeviceToken(t *testing.T, baseURL string, id *devicetrust.HostIdentity, deviceID string, devPriv *ecdsa.PrivateKey) string {
	t.Helper()
	cn := make([]byte, 32)
	rand.Read(cn)
	chalReq, _ := json.Marshal(map[string]interface{}{
		"version": 1, "hostId": id.HostID, "deviceId": deviceID, "clientNonce": hex.EncodeToString(cn),
	})
	resp, _ := http.Post(baseURL+"/api/device-auth/challenge", "application/json", bytes.NewReader(chalReq))
	var cr devicetrust.AuthChallengeResponse
	json.NewDecoder(resp.Body).Decode(&cr)
	resp.Body.Close()
	cid, _ := hex.DecodeString(cr.ChallengeID)
	sn, _ := hex.DecodeString(cr.ServerNonce)
	cms := cr.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	ems := cr.ExpiresAt.UnixMilli()
	tr := devicetrust.AuthTranscript{Role: "device", HostID: cr.HostID, DeviceID: deviceID, DaemonBootID: cr.DaemonBootID, ChallengeID: cid, ClientNonce: cn, ServerNonce: sn, CreatedAtMS: cms, ExpiresAtMS: ems}
	dig := sha256.Sum256(tr.Build())
	dsig, _ := ecdsa.SignASN1(rand.Reader, devPriv, dig[:])
	vr, _ := json.Marshal(map[string]interface{}{"version": 1, "challengeId": cr.ChallengeID, "deviceId": deviceID, "signature": hex.EncodeToString(dsig)})
	resp2, _ := http.Post(baseURL+"/api/device-auth/verify", "application/json", bytes.NewReader(vr))
	var tok devicetrust.VerifyResponse
	json.NewDecoder(resp2.Body).Decode(&tok)
	resp2.Body.Close()
	return tok.Token
}

// TestPA2a_LinkRoutesRemoved_DeviceAuth verifies /api/v2/links returns 404
// (unregistered, not auth-failing) in device-auth remote mode.
func TestPA2a_LinkRoutesRemoved_DeviceAuth(t *testing.T) {
	dir := t.TempDir()
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: dir + "/host.json"})
	reg, _ := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: dir + "/devices.json"})
	_, pubDER, _ := devicetrust.GenKeypair(t)
	reg.Add(pubDER, "owner")

	app, err := NewAppWithDeps(Config{InsecureLocalOnly: false}, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps: %v", err)
	}
	app.hostIdentity = id
	app.deviceRegistry = reg
	if app.authHandler != nil {
		app.authHandler.Identity = id
		app.authHandler.Registry = reg
	}
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()

	// Without auth token: route must be 404 (unregistered), not 401.
	for _, method := range []string{"GET", "POST", "DELETE"} {
		req, _ := http.NewRequest(method, srv.URL+"/api/v2/links", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s /api/v2/links: %v", method, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s /api/v2/links (no auth): %d want 404 (route must be unregistered, not auth-gated)", method, resp.StatusCode)
		}
	}
}

func TestRemoteRouteMatrix(t *testing.T) {
	dir := t.TempDir()
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: dir + "/host.json"})
	reg, _ := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: dir + "/devices.json"})
	ownerPriv, pubDER1, _ := devicetrust.GenKeypair(t)
	od, _ := reg.Add(pubDER1, "owner")
	memberPriv, pubDER2, _ := devicetrust.GenKeypair(t)
	md, _ := reg.Add(pubDER2, "member")

	app, err := NewAppWithDeps(Config{InsecureLocalOnly: false}, testDeps())
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.hostIdentity = id
	app.deviceRegistry = reg
	app.handlers.HostIdentity = id
	if app.authHandler != nil {
		app.authHandler.Identity = id
		app.authHandler.Registry = reg
	}
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()
	ownerTok := getDeviceToken(t, srv.URL, id, od.DeviceID, ownerPriv)
	memberTok := getDeviceToken(t, srv.URL, id, md.DeviceID, memberPriv)

	// Static development credentials never authorize the remote route group.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/sessions"},
		{http.MethodPost, "/api/sessions"},
		{http.MethodGet, "/api/session-profiles"},
		{http.MethodGet, "/term/size?session=missing"},
		{http.MethodGet, "/push/register?token=x"},
		{http.MethodPost, "/debug/cmd?session=missing"},
	} {
		if code := authRequestStatus(t, srv.URL, tc.method, tc.path, "dev-token", nil); code != http.StatusUnauthorized {
			t.Errorf("legacy credential %s %s: %d want 401", tc.method, tc.path, code)
		}
	}

	// Read-only member can observe product state.
	for _, path := range []string{"/api/sessions", "/api/session-profiles"} {
		if code := authRequestStatus(t, srv.URL, http.MethodGet, path, memberTok, nil); code != http.StatusOK {
			t.Errorf("member GET %s: %d want 200", path, code)
		}
	}

	// Read-only member cannot reach any mutating handler.
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/sessions", `{}`},
		{http.MethodDelete, "/api/sessions?id=controlled_pty:nope", ""},
		{http.MethodPost, "/api/sessions/controlled_pty:nope/stop", ""},
		{http.MethodPost, "/api/sessions/controlled_pty:nope/kill", ""},
		{http.MethodDelete, "/api/sessions/controlled_pty:nope", ""},
		{http.MethodPost, "/api/sessions/s/approvals/a", `{}`},
		{http.MethodPost, "/debug/cmd?session=missing", "x"},
	} {
		if code := authRequestStatus(t, srv.URL, tc.method, tc.path, memberTok, strings.NewReader(tc.body)); code != http.StatusForbidden {
			t.Errorf("member %s %s: %d want 403", tc.method, tc.path, code)
		}
	}

	// Owner credentials reach the real handlers (their domain validation may
	// return 4xx, but authentication/authorization must not).
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/sessions", ""},
		{http.MethodPost, "/api/sessions", `{}`},
		{http.MethodPost, "/api/sessions/controlled_pty:nope/stop", ""},
		{http.MethodPost, "/api/sessions/controlled_pty:nope/kill", ""},
		{http.MethodDelete, "/api/sessions/controlled_pty:nope", ""},
		{http.MethodPost, "/api/sessions/s/approvals/a", `{}`},
		{http.MethodGet, "/term/size?session=missing", ""},
		{http.MethodGet, "/term/?session=missing", ""},
		{http.MethodGet, "/push/register?token=x", ""},
		{http.MethodGet, "/debug/diag", ""},
	} {
		code := authRequestStatus(t, srv.URL, tc.method, tc.path, ownerTok, strings.NewReader(tc.body))
		if code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusNotFound && tc.path == "/term/?session=missing" {
			t.Errorf("owner %s %s did not reach handler: %d", tc.method, tc.path, code)
		}
	}

	// Raw diagnostics are not registered on the remote listener.
	for _, path := range []string{"/debug/dump", "/debug/e8diag"} {
		if code := authRequestStatus(t, srv.URL, http.MethodGet, path, ownerTok, nil); code != http.StatusNotFound {
			t.Errorf("remote-only disabled route %s: %d want 404", path, code)
		}
	}
}

func TestLocalOnlyRouteMatrixPreserved(t *testing.T) {
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, testDeps())
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/session-profiles", http.StatusOK},
		{http.MethodGet, "/term/size?session=missing", http.StatusNotFound},
		{http.MethodGet, "/term/?session=missing", http.StatusOK},
		{http.MethodGet, "/push/register?token=x", http.StatusOK},
		{http.MethodPost, "/debug/dump", http.StatusOK},
		{http.MethodGet, "/debug/diag", http.StatusOK},
	} {
		code := authRequestStatus(t, srv.URL, tc.method, tc.path, "dev-token", nil)
		if code != tc.want {
			t.Errorf("local route %s %s: %d want %d", tc.method, tc.path, code, tc.want)
		}
	}
}

func authRequestStatus(t *testing.T, baseURL, method, path, token string, body *strings.Reader) int {
	t.Helper()
	var requestBody *strings.Reader
	if body == nil {
		requestBody = strings.NewReader("")
	} else {
		requestBody = body
	}
	req, err := http.NewRequest(method, baseURL+path, requestBody)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestRemoteWSTicketUpgradeAndBearerExpiry(t *testing.T) {
	dir := t.TempDir()
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: dir + "/host.json"})
	reg, _ := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: dir + "/devices.json"})
	ownerPriv, ownerPub, _ := devicetrust.GenKeypair(t)
	owner, _ := reg.Add(ownerPub, "owner")

	deps := testDeps()
	deps.DeviceSessionConfig = &devicetrust.DeviceSessionManagerConfig{
		Lifetime: 1200 * time.Millisecond, MaxSessions: 4, PurgeInterval: 50 * time.Millisecond,
	}
	deps.WSTicketConfig = &devicetrust.WSTicketStoreConfig{
		TTL: 5 * time.Second, MaxPerDevice: 2, MaxTotal: 4,
	}
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: false}, deps)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.hostIdentity = id
	app.deviceRegistry = reg
	app.handlers.HostIdentity = id
	app.authHandler.Identity = id
	app.authHandler.Registry = reg
	app.sessionMgr.StartPurgeLoop()
	defer app.sessionMgr.StopPurgeLoop()

	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()
	token := getDeviceToken(t, srv.URL, id, owner.DeviceID, ownerPriv)

	createBody := strings.NewReader(`{"profileId":"shell","name":"ws-expiry","cwd":"` + t.TempDir() + `"}`)
	createReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/sessions", createBody)
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil || createResp.StatusCode != http.StatusOK {
		t.Fatalf("create session: err=%v status=%d", err, createResp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()
	if created.ID == "" {
		t.Fatal("create response missing session id")
	}
	defer func() { _, _ = app.lifecycle.Kill(t.Context(), created.ID) }()

	ticketReq, _ := http.NewRequest(http.MethodPost,
		srv.URL+"/api/device-auth/ws-ticket?session="+url.QueryEscape(created.ID), nil)
	ticketReq.Header.Set("Authorization", "Bearer "+token)
	ticketResp, err := http.DefaultClient.Do(ticketReq)
	if err != nil || ticketResp.StatusCode != http.StatusOK {
		t.Fatalf("ticket: err=%v status=%d", err, ticketResp.StatusCode)
	}
	var ticket struct {
		Ticket    string    `json:"ticket"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	_ = json.NewDecoder(ticketResp.Body).Decode(&ticket)
	ticketResp.Body.Close()
	if ticket.Ticket == "" || ticket.ExpiresAt.IsZero() {
		t.Fatalf("invalid ticket response: %+v", ticket)
	}
	principal := app.sessionMgr.AuthenticateBearer(token)
	if principal == nil || !ticket.ExpiresAt.Equal(principal.BearerExpires) {
		t.Fatalf("ticket expiry=%s want bearer expiry=%v", ticket.ExpiresAt, principal)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/term/ws?session=" +
		url.QueryEscape(created.ID) + "&ticket=" + url.QueryEscape(ticket.Ticket)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ticket websocket upgrade: %v", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	for {
		_, _, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("websocket remained active beyond bearer expiry")
		}
	}
	unregisterDeadline := time.Now().Add(time.Second)
	for app.connRegistry.Count(owner.DeviceID) != 0 && time.Now().Before(unregisterDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := app.connRegistry.Count(owner.DeviceID); got != 0 {
		t.Fatalf("expired websocket still registered: %d", got)
	}
}

func TestRemoteWSTicketCapacityThroughProductionHandler(t *testing.T) {
	dir := t.TempDir()
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: dir + "/host.json"})
	reg, _ := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: dir + "/devices.json"})
	ownerPriv, ownerPub, _ := devicetrust.GenKeypair(t)
	owner, _ := reg.Add(ownerPub, "owner")
	deps := testDeps()
	deps.WSTicketConfig = &devicetrust.WSTicketStoreConfig{TTL: time.Minute, MaxPerDevice: 1, MaxTotal: 1}
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: false}, deps)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.hostIdentity = id
	app.deviceRegistry = reg
	app.handlers.HostIdentity = id
	app.authHandler.Identity = id
	app.authHandler.Registry = reg
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()
	token := getDeviceToken(t, srv.URL, id, owner.DeviceID, ownerPriv)
	issue := func(session string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/device-auth/ws-ticket?session="+url.QueryEscape(session), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("issue ticket: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			Ticket string `json:"ticket"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body.Ticket
	}
	status, raw := issue("one")
	if status != http.StatusOK || raw == "" {
		t.Fatalf("first ticket status=%d ticket=%q", status, raw)
	}
	if status, _ := issue("two"); status != http.StatusTooManyRequests {
		t.Fatalf("capacity status=%d want 429", status)
	}
	// A wrong-session use consumes the one-shot ticket and immediately releases
	// capacity; no successful WebSocket upgrade is required for cleanup.
	badURL := srv.URL + "/term/ws?session=wrong&ticket=" + url.QueryEscape(raw)
	if resp, err := http.Get(badURL); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong-session consume: err=%v status=%d", err, resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if status, _ := issue("two"); status != http.StatusOK {
		t.Fatalf("capacity not released after consume: %d", status)
	}
}

func TestRemoteWSTicketFailsClosedWithoutHostIdentity(t *testing.T) {
	deps := testDeps()
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: false}, deps)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	p, _ := func() (*devicetrust.Principal, string) {
		raw, _, _, createErr := app.sessionMgr.CreateAfterVerifiedChallenge(
			"device", "host", app.sessionMgr.BootID(), []string{devicetrust.PermSessionsRead},
		)
		if createErr != nil {
			t.Fatalf("create bearer: %v", createErr)
		}
		return app.sessionMgr.AuthenticateBearer(raw), raw
	}()
	rawTicket, _, err := app.wsTickets.Issue(p, "host", "session")
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/term/ws?session=session&ticket=" + url.QueryEscape(rawTicket))
	if err != nil {
		t.Fatalf("ticket request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("missing host identity status=%d want 503", resp.StatusCode)
	}
	if app.wsTickets.Count() != 1 {
		t.Fatalf("fail-closed host check consumed ticket: count=%d", app.wsTickets.Count())
	}
}

type authTestCloser struct {
	once sync.Once
	done chan struct{}
}

func (c *authTestCloser) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func TestRemoteReplacementAndRevokeInvalidateTicketsAndConnections(t *testing.T) {
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: false}, testDeps())
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	issueGrant := func() (*devicetrust.Principal, string) {
		raw, _, _, createErr := app.sessionMgr.CreateAfterVerifiedChallenge(
			"device", "host", app.sessionMgr.BootID(), []string{devicetrust.PermSessionsRead},
		)
		if createErr != nil {
			t.Fatalf("create bearer: %v", createErr)
		}
		p := app.sessionMgr.AuthenticateBearer(raw)
		ticket, _, issueErr := app.wsTickets.Issue(p, "host", "session")
		if issueErr != nil {
			t.Fatalf("issue ticket: %v", issueErr)
		}
		return p, ticket
	}

	p1, ticket1 := issueGrant()
	closer1 := &authTestCloser{done: make(chan struct{})}
	app.connRegistry.Register(p1.DeviceID, closer1)
	_, _, _, err = app.sessionMgr.CreateAfterVerifiedChallenge(
		p1.DeviceID, p1.HostID, app.sessionMgr.BootID(), []string{devicetrust.PermSessionsRead},
	)
	if err != nil {
		t.Fatalf("replace bearer: %v", err)
	}
	select {
	case <-closer1.done:
	case <-time.After(time.Second):
		t.Fatal("replacement did not close active connection")
	}
	if app.wsTickets.Count() != 0 || app.wsTickets.ConsumeBound(ticket1, "host", "session", app.sessionMgr) != nil {
		t.Fatal("replacement did not revoke pending ticket")
	}

	p2 := app.sessionMgr.AuthenticateBearer(func() string {
		raw, _, _, createErr := app.sessionMgr.CreateAfterVerifiedChallenge(
			"other", "host", app.sessionMgr.BootID(), []string{devicetrust.PermSessionsRead},
		)
		if createErr != nil {
			t.Fatalf("create second bearer: %v", createErr)
		}
		return raw
	}())
	ticket2, _, _ := app.wsTickets.Issue(p2, "host", "session")
	closer2 := &authTestCloser{done: make(chan struct{})}
	app.connRegistry.Register(p2.DeviceID, closer2)
	app.sessionMgr.RevokeDevice(p2.DeviceID)
	select {
	case <-closer2.done:
	case <-time.After(time.Second):
		t.Fatal("revoke did not close active connection")
	}
	if app.wsTickets.Count() != 0 || app.wsTickets.ConsumeBound(ticket2, "host", "session", app.sessionMgr) != nil {
		t.Fatal("revoke did not invalidate pending ticket")
	}
}
