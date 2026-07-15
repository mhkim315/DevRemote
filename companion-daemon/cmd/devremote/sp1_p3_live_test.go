package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// ── SP1-P3: LIVE production proof (pinned codex 0.144.1, real turns) ──
//
// This is the reviewer-mandated closure of SP1: on the REAL production
// composition (pinned identity + fail-closed Verify + real launcher + atomic
// P2B activation + real IPC `pokit run codex` create + authenticated REST),
// an authenticated allow_once and deny each succeed ONLY via the
// resolved-after-written witness (HTTP 200/accepted has no other path), and
// provider consumption is corroborated by command execution/non-execution.
//
// It spends REAL pinned-provider turns, so it is guarded:
//
//	POKIT_SP1_LIVE=1 go test ./cmd/devremote -run TestSP1P3_Live -v -timeout 20m
//
// If the provider consistently resolves in the during-write window (the safe
// ambiguous classification), this test FAILS — per the contract, SP1 must
// then be reported BLOCKED, not accepted.

const sp1LiveProbeFile = "pokit-sp1-live-probe.txt"

type sp1LiveApproval struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Actionable bool   `json:"actionable"`
	Options    []struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	} `json:"options"`
}

type sp1LiveSessionRow struct {
	ID        string            `json:"id"`
	Approvals []sp1LiveApproval `json:"approvals"`
}

// sp1LiveFixture drives the real remote-mode app + a real IPC socket.
type sp1LiveFixture struct {
	f     *remoteFixture
	token string
	ipc   string
}

func newSP1LiveFixture(t *testing.T) *sp1LiveFixture {
	t.Helper()
	// PRODUCTION managed service: deps.Managed stays nil, so NewAppWithDeps
	// builds the pinned-identity service and performs the atomic activation.
	f := newRemoteFixtureWith(t, nil, nil, func(cfg *Config, deps *Dependencies) {
		cfg.EnableManagedCodex = true
	})
	ownerPriv, ownerID := f.pairDevice(t, "sp1-live-owner")
	token := f.token(t, ownerID, ownerPriv)

	dir, err := os.MkdirTemp("/tmp", "sp1live-ipc")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ipc, err := term.StartIPCServer(sock, f.app.registry, f.app.events, f.app.links, nil, f.app.activity, f.app.lifecycle, f.app.managed)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	t.Cleanup(func() { ipc.Close() })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = f.app.managed.Shutdown(ctx)
	})
	return &sp1LiveFixture{f: f, token: token, ipc: sock}
}

// createCodexSession issues the EXACT `pokit run codex` structured create
// over the real 0600 IPC socket (non-detached: no automatic turn).
func (l *sp1LiveFixture) createCodexSession(t *testing.T, cwd string) string {
	t.Helper()
	body := buildRunCreateRequest([]string{"codex"}, cwd, false)
	payload, _ := json.Marshal(body)
	conn, err := net.Dial("unix", l.ipc)
	if err != nil {
		t.Fatalf("dial ipc: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatalf("write create: %v", err)
	}
	line, _ := bufio.NewReader(conn).ReadString('\n')
	var created map[string]string
	if err := json.Unmarshal([]byte(line), &created); err != nil || created["error"] != "" || created["id"] == "" {
		t.Fatalf("create response %q err=%v", line, err)
	}
	t.Logf("SP1-LIVE created managed session %s (cwd %s)", created["id"], cwd)
	return created["id"]
}

// approvalFor polls the authenticated /api/sessions until the session shows
// a PENDING actionable approval, and returns it plus the raw body.
func (l *sp1LiveFixture) approvalFor(t *testing.T, sessionID string, within time.Duration) (sp1LiveApproval, []byte) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		code, raw := l.f.getAll(t, "/api/sessions", l.token)
		if code != http.StatusOK {
			t.Fatalf("GET /api/sessions code=%d", code)
		}
		var rows []sp1LiveSessionRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatalf("decode sessions: %v", err)
		}
		for _, row := range rows {
			if row.ID != sessionID {
				continue
			}
			for _, a := range row.Approvals {
				if a.Actionable && a.State == "pending" {
					return a, raw
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no pending actionable approval appeared for %s within %s", sessionID, within)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// act POSTs one authenticated approval action and returns (code, outcome).
func (l *sp1LiveFixture) act(t *testing.T, sessionID, approvalID, action, key string) (int, string) {
	t.Helper()
	code, body := l.f.doJSON(t, "POST", "/api/sessions/"+sessionID+"/approvals/"+approvalID, l.token,
		`{"action":"`+action+`","idempotencyKey":"`+key+`"}`)
	var res map[string]string
	_ = json.Unmarshal([]byte(body), &res)
	if res["outcome"] == "" {
		return code, strings.TrimSpace(body)
	}
	return code, res["outcome"]
}

// waitStatus polls the authenticated native-status route.
func (l *sp1LiveFixture) waitStatus(t *testing.T, sessionID, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		code, raw := l.f.getAll(t, "/api/sessions/"+sessionID+"/native-status", l.token)
		if code == http.StatusOK {
			var dto struct {
				NativeStatus string `json:"nativeStatus"`
			}
			if json.Unmarshal(raw, &dto) == nil && dto.NativeStatus == want {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %s never reached %s within %s", sessionID, want, within)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// resolvePendingApprovals answers every FURTHER pending approval of the turn
// with the given action until the turn completes (a declined model may retry;
// the loop is bounded).
func (l *sp1LiveFixture) drainTurn(t *testing.T, sessionID, action string, keyPrefix string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Minute)
	extra := 0
	for {
		code, raw := l.f.getAll(t, "/api/sessions/"+sessionID+"/native-status", l.token)
		if code == http.StatusOK {
			var dto struct {
				NativeStatus string `json:"nativeStatus"`
			}
			if json.Unmarshal(raw, &dto) == nil && (dto.NativeStatus == "completed" || dto.NativeStatus == "exited") {
				return
			}
		}
		// Answer any further pending approval with the same decision.
		code, raw = l.f.getAll(t, "/api/sessions", l.token)
		if code == http.StatusOK {
			var rows []sp1LiveSessionRow
			if json.Unmarshal(raw, &rows) == nil {
				for _, row := range rows {
					if row.ID != sessionID {
						continue
					}
					for _, a := range row.Approvals {
						if a.Actionable && a.State == "pending" && extra < 4 {
							extra++
							c, oc := l.act(t, sessionID, a.ID, action, keyPrefix+"-extra-"+a.ID)
							t.Logf("SP1-LIVE extra pending approval %s -> %s (%d %s)", a.ID, action, c, oc)
						}
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("turn on %s never completed", sessionID)
		}
		time.Sleep(1 * time.Second)
	}
}

func sp1LivePrompt() string {
	return "Run exactly this one shell command and nothing else: echo ok > " + sp1LiveProbeFile + " . After the command finishes (or is denied), reply DONE and stop. Do not try any other command."
}

// TestSP1P3_LiveAcceptAndDeny is the mandatory P3 closure evidence.
func TestSP1P3_LiveAcceptAndDeny(t *testing.T) {
	if os.Getenv("POKIT_SP1_LIVE") != "1" {
		t.Skip("live pinned-provider proof; set POKIT_SP1_LIVE=1 to run")
	}
	l := newSP1LiveFixture(t)

	// ── allow_once: write -> resolved -> approved commit -> command EXECUTED ──
	acceptCWD, err := os.MkdirTemp("/tmp", "sp1live-accept")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(acceptCWD)
	acceptID := l.createCodexSession(t, acceptCWD)
	code, body := l.f.doJSON(t, "POST", "/api/managed-sessions/"+acceptID+"/prompt", l.token,
		`{"epoch":1,"text":"`+sp1LivePrompt()+`"}`)
	if code != http.StatusOK {
		t.Fatalf("accept prompt: code=%d body=%s", code, body)
	}
	appr, raw := l.approvalFor(t, acceptID, 3*time.Minute)
	if len(appr.Options) != 2 || appr.Options[0].ID != "allow_once" || appr.Options[1].ID != "deny" {
		t.Fatalf("live DTO must expose exactly the certified options: %+v", appr)
	}
	// DTO privacy on the LIVE authenticated read.
	for _, secret := range []string{"echo ok", sp1LiveProbeFile, "availableDecisions", "requestApproval", "jsonrpc", "/tmp/sp1live", "decision"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("provider material %q leaked into the live /api/sessions body", secret)
		}
	}
	code, outcome := l.act(t, acceptID, appr.ID, "allow_once", "sp1-live-accept")
	if code != http.StatusOK || outcome != "accepted" {
		t.Fatalf("LIVE allow_once did not reach resolved-after-written success: code=%d outcome=%s (during-write-only behavior => SP1 BLOCKED)", code, outcome)
	}
	t.Logf("SP1-LIVE accept: resolved-after-written commit outcome=%s", outcome)
	// Duplicate tap: SAME key replays already_accepted; a NEW key is refused.
	if code, oc := l.act(t, acceptID, appr.ID, "allow_once", "sp1-live-accept"); code != http.StatusOK || oc != "already_accepted" {
		t.Fatalf("duplicate tap (same key) must replay already_accepted: %d %s", code, oc)
	}
	if code, oc := l.act(t, acceptID, appr.ID, "allow_once", "sp1-live-accept-2"); code == http.StatusOK {
		t.Fatalf("a new key on the resolved record must be refused, got 200 %s", oc)
	}
	l.drainTurn(t, acceptID, "allow_once", "sp1-live-accept")
	if _, err := os.Stat(filepath.Join(acceptCWD, sp1LiveProbeFile)); err != nil {
		t.Fatalf("approved command must have EXECUTED (probe file missing): %v", err)
	}
	t.Logf("SP1-LIVE accept: probe file exists (command executed)")

	// ── deny: write -> resolved -> rejected commit -> command NOT executed ──
	denyCWD, err := os.MkdirTemp("/tmp", "sp1live-deny")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(denyCWD)
	denyID := l.createCodexSession(t, denyCWD)
	code, body = l.f.doJSON(t, "POST", "/api/managed-sessions/"+denyID+"/prompt", l.token,
		`{"epoch":2,"text":"`+sp1LivePrompt()+`"}`)
	if code != http.StatusOK {
		t.Fatalf("deny prompt: code=%d body=%s", code, body)
	}
	dAppr, _ := l.approvalFor(t, denyID, 3*time.Minute)
	code, outcome = l.act(t, denyID, dAppr.ID, "deny", "sp1-live-deny")
	if code != http.StatusOK || outcome != "accepted" {
		t.Fatalf("LIVE deny did not reach resolved-after-written success: code=%d outcome=%s (during-write-only behavior => SP1 BLOCKED)", code, outcome)
	}
	if snapState := recordState(t, l, denyID, dAppr.ID); snapState != "rejected" {
		t.Fatalf("deny must commit rejected, got %s", snapState)
	}
	t.Logf("SP1-LIVE deny: resolved-after-written commit (rejected)")
	l.drainTurn(t, denyID, "deny", "sp1-live-deny")
	if _, err := os.Stat(filepath.Join(denyCWD, sp1LiveProbeFile)); !os.IsNotExist(err) {
		t.Fatalf("denied command must NOT have executed (probe file present, err=%v)", err)
	}
	t.Logf("SP1-LIVE deny: probe file absent (command not executed)")

	// ── stale epoch / deletion on the deny session ──
	if code, body := l.f.doJSON(t, "POST", "/api/managed-sessions/"+denyID+"/stop", l.token, `{"epoch":2}`); code != http.StatusOK {
		t.Fatalf("stop: code=%d body=%s", code, body)
	}
	if code, oc := l.act(t, denyID, dAppr.ID, "deny", "sp1-live-stale"); code == http.StatusOK {
		t.Fatalf("action after stop must fail closed, got 200 %s", oc)
	}
	if code, body := l.f.doJSON(t, "DELETE", "/api/managed-sessions/"+denyID, l.token, `{"epoch":2}`); code != http.StatusOK {
		t.Fatalf("delete: code=%d body=%s", code, body)
	}
	if code, _ := l.act(t, denyID, dAppr.ID, "deny", "sp1-live-deleted"); code != http.StatusNotFound {
		t.Fatalf("action after delete must be not_found, got %d", code)
	}
	t.Logf("SP1-LIVE stale/deletion fail closed")

	// Cleanup the accept session too.
	if code, _ := l.f.doJSON(t, "POST", "/api/managed-sessions/"+acceptID+"/stop", l.token, `{"epoch":1}`); code != http.StatusOK {
		t.Logf("accept session stop code=%d (continuing)", code)
	}
	if code, _ := l.f.doJSON(t, "DELETE", "/api/managed-sessions/"+acceptID, l.token, `{"epoch":1}`); code != http.StatusOK {
		t.Logf("accept session delete code=%d (continuing)", code)
	}
	t.Logf("SP1-LIVE shutdown clean")
}

// recordState reads the record's public state via the authenticated list.
func recordState(t *testing.T, l *sp1LiveFixture, sessionID, approvalID string) string {
	t.Helper()
	_, raw := l.f.getAll(t, "/api/sessions", l.token)
	var rows []sp1LiveSessionRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, row := range rows {
		if row.ID != sessionID {
			continue
		}
		for _, a := range row.Approvals {
			if a.ID == approvalID {
				return a.State
			}
		}
	}
	return ""
}
