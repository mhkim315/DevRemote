package term

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/transcript"
	"github.com/gorilla/websocket"
)

func TestInputA_EffectiveInputCapabilitiesAreTicketAuthorized(t *testing.T) {
	denied := &devicetrust.Principal{DeviceID: "viewer", Permissions: []string{devicetrust.PermSessionsRead}}
	if got := effectiveInputCapabilities(denied); len(got) != 0 {
		t.Fatalf("viewer capabilities = %v, want no input capability", got)
	}
	owner := &devicetrust.Principal{DeviceID: "owner", Permissions: []string{devicetrust.PermTerminalInput}}
	if got := effectiveInputCapabilities(owner); len(got) != 1 || got[0] != devicetrust.PermTerminalInput {
		t.Fatalf("owner capabilities = %v, want terminal:input", got)
	}
}

func TestInputA_PermissionAnnouncementCarriesCapabilities(t *testing.T) {
	p := &devicetrust.Principal{DeviceID: "owner", Permissions: []string{devicetrust.PermTerminalInput}}
	var hello struct {
		Type         string   `json:"type"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(permissionAnnouncement(p, "controlled_pty:test", 7, "connection-test"), &hello); err != nil {
		t.Fatal(err)
	}
	if hello.Type != "hello" || len(hello.Capabilities) != 1 || hello.Capabilities[0] != devicetrust.PermTerminalInput {
		t.Fatalf("hello = %+v, want server-authorized terminal:input capability", hello)
	}
}

func TestInputA_SessionCapabilitiesArePrincipalAuthorized(t *testing.T) {
	identity, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(t.TempDir(), "host.json")})
	if err != nil {
		t.Fatal(err)
	}
	sessions := devicetrust.NewPermissiveSessionManager("input-a-caps", time.Minute)
	ownerToken, _, _, err := sessions.CreateAfterVerifiedChallenge("owner", identity.HostID, sessions.BootID(), []string{devicetrust.PermSessionsRead, devicetrust.PermTerminalInput}, 0)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, _, _, err := sessions.CreateAfterVerifiedChallenge("viewer", identity.HostID, sessions.BootID(), []string{devicetrust.PermSessionsRead}, 0)
	if err != nil {
		t.Fatal(err)
	}
	owned := testOwnedPTYRuntime(nil, nil)
	owned.RegisterForTest("controlled_pty:capability-session", "", "test", nil)
	h := &Handlers{Lifecycle: testLifecycleService(owned, nil)}
	endpoint := devicetrust.RequirePrincipal(sessions, h.HandleSessionsV2, devicetrust.PermSessionsRead)
	for _, tc := range []struct {
		name, token string
		wantInput   bool
	}{
		{name: "viewer", token: viewerToken, wantInput: false},
		{name: "owner", token: ownerToken, wantInput: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rr := httptest.NewRecorder()
			endpoint(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			var rows []SessionTelemetry
			if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("rows=%d, want one", len(rows))
			}
			got := false
			for _, cap := range rows[0].Capabilities {
				if cap == devicetrust.PermTerminalInput {
					got = true
				}
			}
			if got != tc.wantInput {
				t.Fatalf("capabilities=%v, terminal input=%v want %v", rows[0].Capabilities, got, tc.wantInput)
			}
		})
	}
}

type inputAWriteCounter struct {
	mu    sync.Mutex
	calls int
}

func (w *inputAWriteCounter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	return len(p), nil
}

func (w *inputAWriteCounter) Calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

// TestInputA_DenialViaHandleWS exercises the actual ticket-consuming
// HandleWS path. It sends a binary websocket frame, reads the bounded denial,
// and proves the captured TerminalTransport did not receive WriteInput.
func TestInputA_DenialViaHandleWS(t *testing.T) {
	const session = "controlled_pty:input-a-deny"
	pr, pw := io.Pipe()
	stream := &mockStream{pr: pr, pw: pw}
	recorder, _ := StartRecorder(session, stream)
	defer recorder.Stop()
	defer pw.Close()

	writes := &inputAWriteCounter{}
	transport := newTerminalTransport(session, 1, writes, stream, recorder)
	owned := testOwnedPTYRuntime(nil, nil)
	owned.RegisterForTest(session, "", "test", recorder)
	owned.mu.Lock()
	owned.entries[session].transport = transport
	owned.mu.Unlock()

	identity, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(t.TempDir(), "host.json")})
	if err != nil {
		t.Fatal(err)
	}
	sessions := devicetrust.NewPermissiveSessionManager("input-a-boot", time.Minute)
	bearer, _, _, err := sessions.CreateAfterVerifiedChallenge("device-readonly", identity.HostID, sessions.BootID(), []string{devicetrust.PermSessionsRead}, 0)
	if err != nil {
		t.Fatal(err)
	}
	principal := sessions.AuthenticateBearer(bearer)
	if principal == nil {
		t.Fatal("device session did not authenticate")
	}
	tickets := devicetrust.NewWSTicketStore()
	ticket, _, err := tickets.Issue(principal, identity.HostID, session)
	if err != nil {
		t.Fatal(err)
	}
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	h := &Handlers{
		Lifecycle:    testLifecycleService(owned, nil),
		authorizer:   testMutationAuthorizer{},
		WSTickets:    tickets,
		SessionMgr:   sessions,
		HostIdentity: identity,
		Transcript:   transcriptSvc,
		// Input-A's bounded read_only contract is retained for the explicit
		// local-development binary path. Paired production rejects binary with
		// update_required and is covered by Input-B.
		InsecureLocalOnly: true,
	}

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	u.Path = "/term/ws"
	u.RawQuery = "session=" + url.QueryEscape(session) + "&ticket=" + url.QueryEscape(ticket)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var hello struct {
		Type         string   `json:"type"`
		Capabilities []string `json:"capabilities"`
	}
	if messageType != websocket.TextMessage || json.Unmarshal(payload, &hello) != nil || hello.Type != "hello" || len(hello.Capabilities) != 0 {
		t.Fatalf("viewer hello = type:%d payload:%s decoded:%+v", messageType, payload, hello)
	}

	// Two rapid frames exercise the same production reader loop: only the
	// first may produce the bounded denial; neither may write or mutate the
	// transcript before authorization is granted.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("blocked-before-first-send")); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("blocked-again")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	messageType, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var denial struct {
		Type string `json:"type"`
	}
	if messageType != websocket.TextMessage || json.Unmarshal(payload, &denial) != nil || denial.Type != "read_only" {
		t.Fatalf("denial = type:%d payload:%s decoded:%+v", messageType, payload, denial)
	}
	if got := writes.Calls(); got != 0 {
		t.Fatalf("unauthorized binary frame invoked TerminalTransport.WriteInput %d times, want 0", got)
	}
	if got := transcriptSvc.ListTranscript(session); len(got) != 0 {
		t.Fatalf("unauthorized binary frame mutated transcript: %+v", got)
	}

	// The per-connection limiter must coalesce the second denial. A read
	// deadline well inside the one-second window proves no second TextMessage
	// was emitted, i.e. at most one denial per second.
	conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if mt, extra, err := conn.ReadMessage(); err == nil {
		t.Fatalf("unexpected second denial within rate window: type=%d payload=%s", mt, extra)
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("second denial read error = %v, want deadline timeout", err)
	}
}
