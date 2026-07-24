package term

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
)

// ── SP1-P2A: delivery material + certified resolved-consumption boundary ──
//
// Boundary tests drive the PRODUCTION pump and the real Deliver sequence
// against a raw-line scripted provider. Actionable records with delivery
// material are ingested DIRECTLY (production ingestion stays non-actionable;
// the observation→actionable connection is P2B) and the pending provider
// identity is armed explicitly with the same preserved-token shape the P1
// observation produces.

// rawLines collects the exact provider-received response lines.
type rawLines struct {
	mu    sync.Mutex
	lines [][]byte
}

func (r *rawLines) add(b []byte) {
	r.mu.Lock()
	r.lines = append(r.lines, append([]byte(nil), b...))
	r.mu.Unlock()
}

func (r *rawLines) snapshot() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.lines))
	copy(out, r.lines)
	return out
}

func (r *rawLines) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lines)
}

// rawScriptLauncher hands the fake provider the EXACT raw line the daemon
// wrote (runAppServerScript would re-parse and lose bytes).
type rawScriptLauncher struct {
	mu      sync.Mutex
	procs   []*fakeManagedProc
	handler func(raw []byte, emit func(line string))
}

func (l *rawScriptLauncher) Launch(_ string, _ []string) (ManagedProcess, error) {
	p := newFakeManagedProc()
	l.mu.Lock()
	l.procs = append(l.procs, p)
	l.mu.Unlock()
	go func() {
		defer close(p.done)
		sc := bufio.NewScanner(p.stdinR)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		emit := func(line string) { p.stdoutW.Write([]byte(line + "\n")) }
		for sc.Scan() {
			raw := bytes.TrimSpace(sc.Bytes())
			if len(raw) == 0 {
				continue
			}
			l.handler(append([]byte(nil), raw...), emit)
		}
	}()
	return p, nil
}

func jsonLine(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

func resolvedLine(idToken string) string {
	return `{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"` + apprTestThread + `","requestId":` + idToken + `}}`
}

// deliverySession is one managed session with an active bound turn, a
// raw-line provider script that records every daemon response write, and an
// optional auto-resolve mode (the provider resolves any response id it
// receives — the CP0 accept ordering).
type deliverySession struct {
	managed     *ManagedCodexService
	store       *AuthoritativeApprovalStore
	launcher    *rawScriptLauncher
	rec         *pumpRecorder
	id          string
	rt          *codexManagedRuntime
	written     *rawLines
	autoResolve int32
	probes      int
}

// buildDeliverySession constructs the launcher/store/service/recorder WITHOUT
// wiring the store or launching a session, so callers choose the P1
// observation wiring (SetApprovalStore) or the P2B activation transition.
func buildDeliverySession(t *testing.T, autoResolve bool) *deliverySession {
	t.Helper()
	s := &deliverySession{written: &rawLines{}}
	if autoResolve {
		s.autoResolve = 1
	}
	s.launcher = &rawScriptLauncher{handler: func(raw []byte, emit func(string)) {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		method, _ := m["method"].(string)
		idf, hasID := m["id"].(float64)
		switch method {
		case "initialize":
			emit(jsonLine(map[string]any{"jsonrpc": "2.0", "id": idf, "result": map[string]any{}}))
		case "thread/start":
			emit(jsonLine(map[string]any{"jsonrpc": "2.0", "id": idf, "result": map[string]any{"threadId": apprTestThread}}))
		case "turn/start":
			emit(jsonLine(map[string]any{"jsonrpc": "2.0", "id": idf, "result": map[string]any{"turn": map[string]any{"id": apprTestTurn}}}))
			emit(jsonLine(map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{"threadId": apprTestThread, "turn": map[string]any{"id": apprTestTurn}}}))
		case "":
			if !hasID {
				return
			}
			// A daemon approval RESPONSE write: record the exact bytes.
			s.written.add(raw)
			if atomic.LoadInt32(&s.autoResolve) == 1 {
				emit(resolvedLine(strconv.FormatInt(int64(idf), 10)))
			}
		}
	}}
	s.store = testApprovalStore()
	s.managed = newTestManagedService(s.launcher)
	s.rec = newPumpRecorder()
	s.managed.pumpObserver = s.rec.observe
	return s
}

// launchTurn creates the managed session and starts one bound turn.
func (s *deliverySession) launchTurn(t *testing.T) {
	t.Helper()
	id, err := s.managed.CreateAttached("", "test-device", 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	s.id = id
	if err := s.managed.SubmitPrompt(id, 1, "run the build", "test-device", 0); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitForStatus(t, s.managed.Registry(), id, ManagedStatusWorking)
	s.managed.mu.Lock()
	s.rt = s.managed.runtimes[id]
	s.managed.mu.Unlock()
	if s.rt == nil {
		t.Fatalf("runtime not published")
	}
	t.Cleanup(func() { _ = s.managed.Kill(id, 1, "", 0) })
}

func newDeliverySession(t *testing.T, autoResolve bool) *deliverySession {
	t.Helper()
	s := buildDeliverySession(t, autoResolve)
	if err := s.managed.SetApprovalStore(s.store); err != nil {
		t.Fatalf("set approval store: %v", err)
	}
	s.launchTurn(t)
	return s
}

// newActivatedDeliverySession performs the SP1-P2B production activation
// transition BEFORE the first runtime and returns the installed endpoints.
func newActivatedDeliverySession(t *testing.T, autoResolve bool) (*deliverySession, *CodexManagedApprovalDelivery, func(string) (RuntimeRef, bool)) {
	t.Helper()
	s := buildDeliverySession(t, autoResolve)
	delivery, runtimeOf, err := s.managed.InstallApprovalExecution(s.store)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	s.launchTurn(t)
	return s, delivery.(*CodexManagedApprovalDelivery), runtimeOf
}

// inject writes one raw provider line into the runtime's stdout.
func (s *deliverySession) inject(t *testing.T, raw string) {
	t.Helper()
	s.launcher.mu.Lock()
	p := s.launcher.procs[len(s.launcher.procs)-1]
	s.launcher.mu.Unlock()
	if _, err := p.stdoutW.Write([]byte(raw + "\n")); err != nil {
		t.Fatalf("inject: %v", err)
	}
}

// sync waits until the pump fully processed every previously injected line.
func (s *deliverySession) sync(t *testing.T) {
	t.Helper()
	s.probes++
	s.inject(t, `{"jsonrpc":"2.0","method":"probe/sync","params":{"threadId":"`+apprTestThread+`"}}`)
	s.rec.waitForCount(t, "probe/sync", s.probes)
}

// armPending installs the preserved native identity exactly as the P1
// observation would (test-arranged: the production observation→actionable
// connection is P2B scope).
func (s *deliverySession) armPending(t *testing.T, idToken, approvalID string) {
	t.Helper()
	idInt, err := strconv.ParseInt(idToken, 10, 64)
	if err != nil {
		t.Fatalf("bad token %q", idToken)
	}
	s.rt.turnMu.Lock()
	s.rt.pendingApprovals[idInt] = pendingProviderRequest{
		idToken: idToken, idInt: idInt, threadID: apprTestThread, turnID: apprTestTurn,
		itemID: "item-x", approvalID: approvalID, observedAt: time.Now(),
	}
	s.rt.turnMu.Unlock()
}

// waitWritten waits (bounded) until the provider recorded exactly n response
// writes — recording happens on the script goroutine after the daemon's write
// returns, so an assertion immediately after the write races the recorder.
func (s *deliverySession) waitWritten(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.written.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("provider never recorded %d writes (have %d)", n, s.written.count())
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := s.written.count(); got != n {
		t.Fatalf("want exactly %d writes, got %d", n, got)
	}
}

// acceptBytes/declineBytes are the production response material (the
// CP0/SP1-P3-live proven consumed shape, no jsonrpc member).
func acceptBytes(idToken string) []byte {
	return codexDecisionResponse(idToken, "accept")
}

func declineBytes(idToken string) []byte {
	return codexDecisionResponse(idToken, "decline")
}

func certifiedOptions() []agent.InteractionOption {
	return []agent.InteractionOption{
		{ID: "allow_once", Label: "Approve", Kind: "approve"},
		{ID: "deny", Label: "Reject", Kind: "reject"},
	}
}

// ingestActionable ingests ONE actionable record with per-option delivery
// material directly (test-arranged; never the production path).
func ingestActionable(t *testing.T, store *AuthoritativeApprovalStore, sessionID, approvalID, idToken string) {
	t.Helper()
	ok := store.IngestObserved(ApprovalIngest{
		SessionID: sessionID, LaunchGen: 1, StreamGen: 0,
		Provider: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion,
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: approvalID, SessionID: sessionID, AgentKind: codexAppServerAdapter,
				Kind: "approval", Options: certifiedOptions(), Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance: contract.ProvenanceProviderProtocol, Actionable: true,
			RequiredPerm: devicetrust.PermTerminalInput,
			DeliveryMaterial: []ApprovalDeliveryMaterial{
				{OptionID: "allow_once", SchemaVersion: "codex.appserver.decision.v1", ResponseBytes: acceptBytes(idToken)},
				{OptionID: "deny", SchemaVersion: "codex.appserver.decision.v1", ResponseBytes: declineBytes(idToken)},
			},
		}},
	})
	if !ok {
		t.Fatalf("actionable ingest with material must be admitted")
	}
}

func boundManagedRT() RuntimeRef {
	return RuntimeRef{Adapter: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion, LaunchGen: 1, StreamGen: 0}
}

// resolveAtWrittenMark wires the boundary's barrier so the provider's
// resolved is injected and pump-routed deterministically AFTER the written
// mark — the CP0 accept ordering. (An inline auto-resolve races the written
// mark on the in-process pipe rendezvous: the fake provider can emit resolved
// the instant the write syscall returns, which the production semantics
// correctly classify as the ambiguous during-write outcome. Real pipes
// decouple write-return from provider processing; the barrier restores that
// ordering deterministically.)
func (s *deliverySession) resolveAtWrittenMark(t *testing.T, d *CodexManagedApprovalDelivery, idToken string) {
	t.Helper()
	d.barrier = func(stage string) {
		if stage != "post-written-mark" {
			return
		}
		s.inject(t, resolvedLine(idToken))
		s.sync(t)
	}
}

func mustClaim(t *testing.T, store *AuthoritativeApprovalStore, sessionID, approvalID, optionID, key string) ClaimResult {
	t.Helper()
	c := store.ClaimForExecution(ClaimRequest{
		SessionID: sessionID, ApprovalID: approvalID, OptionID: optionID,
		Runtime: boundManagedRT(), Requester: completeRequester(), IdempotencyKey: key,
	})
	if c.Outcome != ClaimGranted {
		t.Fatalf("claim: %s", c.Outcome)
	}
	return c
}

// ── §4 store material extension ──

// TestDeliveryMaterial_ClaimReturnsExactImmutableBytes: the claim payload is
// the EXACT ingested bytes; mutating the caller's slice after ingestion does
// not change them; the binding digest is of those exact bytes.
func TestDeliveryMaterial_ClaimReturnsExactImmutableBytes(t *testing.T) {
	store := testApprovalStore()
	sid := codexAppServerAdapter + ":codex-app-m"
	accept := acceptBytes("7")
	orig := append([]byte(nil), accept...)
	ok := store.IngestObserved(ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion,
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "codexas-1-7", SessionID: sid, Kind: "approval", Options: certifiedOptions()},
			Provenance: contract.ProvenanceProviderProtocol, Actionable: true,
			RequiredPerm:     devicetrust.PermTerminalInput,
			DeliveryMaterial: []ApprovalDeliveryMaterial{{OptionID: "allow_once", SchemaVersion: "v1", ResponseBytes: accept}, {OptionID: "deny", SchemaVersion: "v1", ResponseBytes: declineBytes("7")}},
		}},
	})
	if !ok {
		t.Fatalf("ingest refused")
	}
	for i := range accept {
		accept[i] = 'X' // caller-side mutation after ingestion
	}
	c := mustClaim(t, store, sid, "codexas-1-7", "allow_once", "k1")
	if !bytes.Equal(c.Payload, orig) {
		t.Fatalf("claim payload must be the exact original bytes: %q", c.Payload)
	}
	if c.Binding.PayloadDigest != payloadDigest(orig) {
		t.Fatalf("binding digest must cover the exact bytes")
	}
}

// TestDeliveryMaterial_DigestCoversMaterial: records differing only in
// material yield different action digests, and both differ from the
// no-material digest of the same option.
func TestDeliveryMaterial_DigestCoversMaterial(t *testing.T) {
	digestFor := func(sid string, material []ApprovalDeliveryMaterial, actionable bool) string {
		store := testApprovalStore()
		if !store.IngestObserved(ApprovalIngest{
			SessionID: sid, LaunchGen: 1, StreamGen: 0,
			Provider: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion,
			Items: []ApprovalIngestItem{{
				Approval:   agent.AgentApproval{ID: "codexas-1-7", SessionID: sid, Kind: "approval", Options: certifiedOptions()},
				Provenance: contract.ProvenanceProviderProtocol, Actionable: actionable,
				RequiredPerm: devicetrust.PermTerminalInput, DeliveryMaterial: material,
			}},
		}) {
			t.Fatalf("ingest refused for %s", sid)
		}
		c := mustClaim(t, store, sid, "codexas-1-7", "allow_once", "k1")
		return c.Binding.ActionDigest
	}
	sid := codexAppServerAdapter + ":codex-app-d"
	matA := []ApprovalDeliveryMaterial{{OptionID: "allow_once", SchemaVersion: "v1", ResponseBytes: acceptBytes("7")}, {OptionID: "deny", SchemaVersion: "v1", ResponseBytes: declineBytes("7")}}
	matB := []ApprovalDeliveryMaterial{{OptionID: "allow_once", SchemaVersion: "v1", ResponseBytes: acceptBytes("8")}, {OptionID: "deny", SchemaVersion: "v1", ResponseBytes: declineBytes("8")}}
	dA := digestFor(sid, matA, true)
	dB := digestFor(sid, matB, true)
	dNone := digestFor(sid, nil, true)
	if dA == dB || dA == dNone || dB == dNone {
		t.Fatalf("material must be part of the action digest: %s %s %s", dA, dB, dNone)
	}
}

// TestDeliveryMaterial_InvalidRejectsItem: every invalid material shape
// rejects the WHOLE item.
func TestDeliveryMaterial_InvalidRejectsItem(t *testing.T) {
	sid := codexAppServerAdapter + ":codex-app-i"
	base := func(actionable bool, mats []ApprovalDeliveryMaterial) bool {
		store := testApprovalStore()
		return store.IngestObserved(ApprovalIngest{
			SessionID: sid, LaunchGen: 1, StreamGen: 0,
			Provider: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion,
			Items: []ApprovalIngestItem{{
				Approval:   agent.AgentApproval{ID: "codexas-1-7", SessionID: sid, Kind: "approval", Options: certifiedOptions()},
				Provenance: contract.ProvenanceProviderProtocol, Actionable: actionable,
				RequiredPerm: devicetrust.PermTerminalInput, DeliveryMaterial: mats,
			}},
		})
	}
	good := ApprovalDeliveryMaterial{OptionID: "allow_once", SchemaVersion: "v1", ResponseBytes: acceptBytes("7")}
	cases := map[string]struct {
		actionable bool
		mats       []ApprovalDeliveryMaterial
	}{
		"material on non-actionable": {false, []ApprovalDeliveryMaterial{good}},
		"unknown option":             {true, []ApprovalDeliveryMaterial{{OptionID: "nope", SchemaVersion: "v1", ResponseBytes: acceptBytes("7")}}},
		"duplicate option":           {true, []ApprovalDeliveryMaterial{good, good}},
		"empty bytes":                {true, []ApprovalDeliveryMaterial{{OptionID: "allow_once", SchemaVersion: "v1"}}},
		"oversized bytes":            {true, []ApprovalDeliveryMaterial{{OptionID: "allow_once", SchemaVersion: "v1", ResponseBytes: bytes.Repeat([]byte("x"), maxDeliveryMaterialBytes+1)}}},
		"empty schema":               {true, []ApprovalDeliveryMaterial{{OptionID: "allow_once", ResponseBytes: acceptBytes("7")}}},
		"oversized schema":           {true, []ApprovalDeliveryMaterial{{OptionID: "allow_once", SchemaVersion: string(bytes.Repeat([]byte("v"), maxDeliverySchemaVerBytes+1)), ResponseBytes: acceptBytes("7")}}},
	}
	for name, tc := range cases {
		if base(tc.actionable, tc.mats) {
			t.Fatalf("%s must reject the item", name)
		}
	}
	if !base(true, []ApprovalDeliveryMaterial{good}) {
		t.Fatalf("valid material must be admitted (control)")
	}
}

// TestDeliveryMaterial_SubstitutedDigestNeverCommits: a receipt whose
// delivered digest is of substituted bytes never commits.
func TestDeliveryMaterial_SubstitutedDigestNeverCommits(t *testing.T) {
	store := testApprovalStore()
	sid := codexAppServerAdapter + ":codex-app-s"
	ingestActionable(t, store, sid, "codexas-1-7", "7")
	c := mustClaim(t, store, sid, "codexas-1-7", "allow_once", "k1")
	commit := store.RecordDelivery(DeliveryReceipt{
		Outcome: DeliveryAccepted, ClaimToken: c.Token, Binding: c.Binding,
		ReceiptID: "r1", DeliveredPayloadDigest: payloadDigest([]byte(`{"jsonrpc":"2.0","id":7,"result":{"decision":"decline"}}`)),
	})
	if commit.Committed || commit.State != ApprovalDeliveryFailed {
		t.Fatalf("substituted digest must not commit: %+v", commit)
	}
}

// TestDeliveryMaterial_NeverInPublicDTOs: response bytes and schema versions
// never reach ListSafe/List/managed rows.
func TestDeliveryMaterial_NeverInPublicDTOs(t *testing.T) {
	store := testApprovalStore()
	sid := codexAppServerAdapter + ":codex-app-p"
	ingestActionable(t, store, sid, "codexas-1-7", "7")
	var surfaces []byte
	for _, v := range []any{store.ListSafe(sid), store.List(sid)} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		surfaces = append(surfaces, b...)
	}
	for _, secret := range []string{`"decision"`, "accept\"", "decline", "codex.appserver.decision.v1", "jsonrpc"} {
		if bytes.Contains(surfaces, []byte(secret)) {
			t.Fatalf("delivery material %q leaked into a public DTO", secret)
		}
	}
}

// ── §5 certified boundary ──

// TestCodexDelivery_WriteThenExactResolvedCommits: the happy path — the
// boundary writes EXACTLY the claim bytes once, the provider resolves, the
// receipt is accepted and the store commits approved.
func TestCodexDelivery_WriteThenExactResolvedCommits(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 5 * time.Second
	s.resolveAtWrittenMark(t, d, "7")
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	if receipt.Outcome != DeliveryAccepted || receipt.ReceiptID == "" {
		t.Fatalf("delivery must be accepted: %+v", receipt)
	}
	if receipt.DeliveredPayloadDigest != c.Binding.PayloadDigest {
		t.Fatalf("delivered digest must equal claim digest")
	}
	lines := s.written.snapshot()
	if len(lines) != 1 || !bytes.Equal(lines[0], c.Payload) {
		t.Fatalf("exactly one exact-byte write expected: %q", lines)
	}
	commit := s.store.RecordDelivery(receipt)
	if !commit.Committed || commit.State != ApprovalApproved {
		t.Fatalf("commit must be approved: %+v", commit)
	}
	// The consumed provider request is gone from the pending observations.
	if pending, _ := s.rt.approvalObservationSnapshot(); len(pending) != 0 {
		t.Fatalf("consumed request must leave pending: %+v", pending)
	}
}

// TestCodexDelivery_DenyPathCommitsRejected: the deny option carries the
// decline bytes and commits rejected.
func TestCodexDelivery_DenyPathCommitsRejected(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "deny", "k1")
	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 5 * time.Second
	s.resolveAtWrittenMark(t, d, "7")
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("deny delivery must be accepted: %+v", receipt)
	}
	if lines := s.written.snapshot(); len(lines) != 1 || !bytes.Equal(lines[0], declineBytes("7")) {
		t.Fatalf("exact decline bytes expected: %q", lines)
	}
	commit := s.store.RecordDelivery(receipt)
	if !commit.Committed || commit.State != ApprovalRejected {
		t.Fatalf("commit must be rejected-kind: %+v", commit)
	}
}

// TestCodexDelivery_SubstitutedPayloadZeroWrites: tampered bytes with the
// real claim token are rejected BEFORE any provider write.
func TestCodexDelivery_SubstitutedPayloadZeroWrites(t *testing.T) {
	s := newDeliverySession(t, true)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")
	d := NewCodexManagedApprovalDelivery(s.managed)
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: declineBytes("7")})
	if receipt.Outcome != DeliveryRejected {
		t.Fatalf("substitution must reject: %+v", receipt)
	}
	if s.written.count() != 0 {
		t.Fatalf("substitution must never reach the wire")
	}
	if commit := s.store.RecordDelivery(receipt); commit.Committed {
		t.Fatalf("substitution must not commit")
	}
}

// TestCodexDelivery_IDEchoMismatchZeroWrites: material whose top-level id
// token differs from the preserved pending token never reaches the wire.
func TestCodexDelivery_IDEchoMismatchZeroWrites(t *testing.T) {
	s := newDeliverySession(t, true)
	ingestActionable(t, s.store, s.id, "codexas-1-9", "9") // material echoes id 9
	s.armPending(t, "7", "codexas-1-9")                    // provider request id is 7
	c := mustClaim(t, s.store, s.id, "codexas-1-9", "allow_once", "k1")
	d := NewCodexManagedApprovalDelivery(s.managed)
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	if receipt.Outcome != DeliveryRejected || s.written.count() != 0 {
		t.Fatalf("id echo mismatch must reject with zero writes: %+v", receipt)
	}
}

// TestCodexDelivery_ResolvedBeforeDeliverZeroWrites: a provider resolution
// consumed before Deliver leaves no pending identity — the delivery fails
// with zero writes and the store never records success.
func TestCodexDelivery_ResolvedBeforeDeliverZeroWrites(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	s.inject(t, resolvedLine("7")) // provider resolves first (cancel)
	s.sync(t)

	d := NewCodexManagedApprovalDelivery(s.managed)
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	if receipt.Outcome != DeliveryUnavailable || s.written.count() != 0 {
		t.Fatalf("pre-consumed resolution must fail with zero writes: %+v", receipt)
	}
	commit := s.store.RecordDelivery(receipt)
	if commit.Committed || commit.State != ApprovalDeliveryFailed {
		t.Fatalf("must end delivery_failed: %+v", commit)
	}
}

// TestCodexDelivery_ResolvedInArmWindowSkipsWrite: a provider resolution
// routed between arm and write (deterministic barrier) fails the delivery
// and the write is SKIPPED entirely.
func TestCodexDelivery_ResolvedInArmWindowSkipsWrite(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 5 * time.Second
	d.barrier = func(stage string) {
		if stage != "post-arm" {
			return
		}
		// The provider resolves while the waiter is armed but unwritten.
		s.inject(t, resolvedLine("7"))
		s.sync(t)
	}
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	if receipt.Outcome != DeliveryRejected {
		t.Fatalf("pre-write resolution must fail: %+v", receipt)
	}
	if s.written.count() != 0 {
		t.Fatalf("the write must be skipped after a pre-write resolution")
	}
	if commit := s.store.RecordDelivery(receipt); commit.Committed {
		t.Fatalf("pre-write resolution must not commit")
	}
}

// TestCodexDelivery_DuplicateArmConflicts: a second Deliver for the same
// native id while the first waits fails with zero additional writes; the
// first still succeeds on the exact resolved.
func TestCodexDelivery_DuplicateArmConflicts(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")
	req := ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload}

	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 5 * time.Second
	markReached := make(chan struct{})
	release := make(chan struct{})
	d.barrier = func(stage string) {
		if stage != "post-written-mark" {
			return
		}
		close(markReached)
		<-release
	}
	first := make(chan DeliveryReceipt, 1)
	go func() { first <- d.Deliver(req) }()

	// Deterministic: the first delivery holds at the written mark.
	<-markReached
	second := d.Deliver(req)
	if second.Outcome != DeliveryConflict {
		t.Fatalf("duplicate arm must conflict: %+v", second)
	}
	s.waitWritten(t, 1) // duplicate added zero writes
	close(release)
	s.inject(t, resolvedLine("7"))
	receipt := <-first
	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("first delivery must still succeed: %+v", receipt)
	}
}

// TestCodexDelivery_TimeoutThenLateResolvedNeverCommits: no resolved within
// the bound → ambiguous non-retryable failure; a LATE resolved finds no
// waiter and cannot commit or resurrect the record.
func TestCodexDelivery_TimeoutThenLateResolvedNeverCommits(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 100 * time.Millisecond
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	if receipt.Outcome != DeliveryConflict {
		t.Fatalf("timeout must be the ambiguous conflict outcome: %+v", receipt)
	}
	if s.written.count() != 1 {
		t.Fatalf("timeout happens after the single write")
	}
	commit := s.store.RecordDelivery(receipt)
	if commit.Committed || commit.State != ApprovalDeliveryFailed {
		t.Fatalf("timeout must end delivery_failed: %+v", commit)
	}
	// Ambiguous outcomes are non-retryable.
	retry := s.store.ClaimForExecution(ClaimRequest{
		SessionID: s.id, ApprovalID: "codexas-1-7", OptionID: "allow_once",
		Runtime: boundManagedRT(), Requester: completeRequester(), IdempotencyKey: "k1",
	})
	if retry.Outcome != ClaimRetryExhausted {
		t.Fatalf("ambiguous timeout must not be retryable: %s", retry.Outcome)
	}
	// The LATE resolved is inert: no waiter, record not pending.
	s.inject(t, resolvedLine("7"))
	s.sync(t)
	snap, ok := s.store.LookupRecord(s.id, "codexas-1-7")
	if !ok || snap.State != ApprovalDeliveryFailed {
		t.Fatalf("late resolved must not change the record: %+v", snap)
	}
}

// TestCodexDelivery_ExitFailsWaiterDeterministically: the child exiting
// while the waiter is armed fails the delivery with stale_runtime.
func TestCodexDelivery_ExitFailsWaiterDeterministically(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 5 * time.Second
	done := make(chan DeliveryReceipt, 1)
	go func() {
		done <- d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for s.written.count() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("delivery never wrote")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := s.managed.Kill(s.id, 1, "", 0); err != nil {
		t.Fatalf("kill: %v", err)
	}
	receipt := <-done
	if receipt.Outcome != DeliveryStaleRuntime {
		t.Fatalf("exit must fail the waiter with stale_runtime: %+v", receipt)
	}
	commit := s.store.RecordDelivery(receipt)
	if commit.Committed {
		t.Fatalf("exit must not commit")
	}
}

// TestCodexDelivery_StaleBindingsZeroWrites: stale epoch, wrong adapter,
// wrong version, and a deleted session never reach the wire.
func TestCodexDelivery_StaleBindingsZeroWrites(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")
	d := NewCodexManagedApprovalDelivery(s.managed)

	staleEpoch := c.Binding
	staleEpoch.Runtime.LaunchGen = 2
	if r := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: staleEpoch, Payload: c.Payload}); r.Outcome != DeliveryStaleRuntime {
		t.Fatalf("stale epoch: %+v", r)
	}
	wrongAdapter := c.Binding
	wrongAdapter.Runtime.Adapter = "legacy"
	if r := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: wrongAdapter, Payload: c.Payload}); r.Outcome != DeliveryRuntimeMismatch {
		t.Fatalf("wrong adapter: %+v", r)
	}
	wrongVersion := c.Binding
	wrongVersion.Runtime.Version = "0.144.4"
	if r := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: wrongVersion, Payload: c.Payload}); r.Outcome != DeliveryStaleRuntime {
		t.Fatalf("wrong version: %+v", r)
	}
	if s.written.count() != 0 {
		t.Fatalf("no stale binding may reach the wire")
	}

	if err := s.managed.Kill(s.id, 1, "", 0); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if err := s.managed.Delete(s.id, 1, "", 0); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if r := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload}); r.Outcome != DeliveryUnavailable {
		t.Fatalf("deleted session: %+v", r)
	}
	if s.written.count() != 0 {
		t.Fatalf("deleted session may not be written to")
	}
}

// TestCodexDelivery_MalformedResolvedInertNonVacuous: every malformed
// resolved variant leaves the armed waiter untouched — proven non-vacuously
// because the EXACT resolved afterwards still completes the delivery.
func TestCodexDelivery_MalformedResolvedInertNonVacuous(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 10 * time.Second
	markReached := make(chan struct{})
	d.barrier = func(stage string) {
		if stage == "post-written-mark" {
			close(markReached)
		}
	}
	done := make(chan DeliveryReceipt, 1)
	go func() {
		done <- d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	}()
	// Deterministic: the waiter is armed AND marked written before any
	// malformed variant is offered.
	<-markReached

	oversizedParams := `{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"` + apprTestThread + `","requestId":7,"` +
		"p" + `":"` + string(bytes.Repeat([]byte("x"), maxResolvedParamsBytes)) + `"}}`
	variants := []string{
		`{"jsonrpc":"1.0","method":"serverRequest/resolved","params":{"threadId":"` + apprTestThread + `","requestId":7}}`,                                     // wrong jsonrpc
		`{"jsonrpc":"2.0","method":"serverRequest/resolved","extra":1,"params":{"threadId":"` + apprTestThread + `","requestId":7}}`,                           // unknown top-level field
		`{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"` + apprTestThread + `","requestId":7,"why":"x"}}`,                           // unknown params field
		`{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"` + apprTestThread + `","threadId":"` + apprTestThread + `","requestId":7}}`, // duplicate key
		`{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"thread-OTHER","requestId":7}}`,                                               // wrong thread
		`{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"` + apprTestThread + `","requestId":"7"}}`,                                   // string id
		oversizedParams, // params over bound
	}
	for _, v := range variants {
		s.inject(t, v)
	}
	// Oversized whole message (> maxResolvedMessageBytes).
	s.inject(t, `{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"`+apprTestThread+`","requestId":7},"`+
		"z"+`":"`+string(bytes.Repeat([]byte("x"), maxResolvedMessageBytes))+`"}`)
	s.sync(t)

	select {
	case r := <-done:
		t.Fatalf("malformed resolved must not complete the delivery: %+v", r)
	default:
	}
	s.inject(t, resolvedLine("7"))
	receipt := <-done
	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("exact resolved must still succeed: %+v", receipt)
	}
	if commit := s.store.RecordDelivery(receipt); !commit.Committed || commit.State != ApprovalApproved {
		t.Fatalf("commit must be approved: %+v", commit)
	}
}

// TestCodexDelivery_ProductionActionabilityStillOff: the P1 production
// observation path is untouched by P2A — a wire-certified request still
// creates a NON-actionable record that cannot be claimed, and the P2A
// boundary is unreachable for it (no options, no material, no claim).
func TestCodexDelivery_ProductionActionabilityStillOff(t *testing.T) {
	s := newApprovalSession(t)
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	got := waitForSafeApprovals(t, s.store, s.id, 1)
	if got[0].Actionable || len(got[0].Options) != 0 {
		t.Fatalf("production record must stay non-actionable: %+v", got)
	}
	claim := s.store.ClaimForExecution(ClaimRequest{
		SessionID: s.id, ApprovalID: got[0].ID, OptionID: "allow_once",
		Runtime: boundManagedRT(), Requester: completeRequester(), IdempotencyKey: "k1",
	})
	if claim.Outcome != ClaimNotActionable {
		t.Fatalf("production record must not be claimable: %s", claim.Outcome)
	}
}

// ── P2A-R1 blocker 1: write-claim linearization ──

// TestCodexDelivery_KnownBadCheckThenWriteControl reproduces the
// PRE-REMEDIATION race as a known-bad control: the old sequence checked the
// waiter under respMu, released the lock, and then wrote. A resolved routed
// in that window left the old code writing provider bytes for an
// already-resolved request. This control proves the interleaving is real and
// that the test harness can catch it; the production path under the same
// interleaving performs ZERO writes (TestCodexDelivery_ResolvedInArmWindowSkipsWrite)
// or fails ambiguously without retry (TestCodexDelivery_ResolvedDuringWriteClaim).
func TestCodexDelivery_KnownBadCheckThenWriteControl(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	// Arm a waiter exactly like Deliver does.
	w := &approvalResponseWaiter{idToken: "7", state: waiterStateArmed, ch: make(chan approvalWaiterOutcome, 1)}
	s.rt.respMu.Lock()
	s.rt.respWaiters[7] = w
	s.rt.respMu.Unlock()

	// KNOWN-BAD: check-then-write without claiming the write.
	s.rt.respMu.Lock()
	stillArmed := s.rt.respWaiters[7] == w
	s.rt.respMu.Unlock()
	if !stillArmed {
		t.Fatalf("control setup broken")
	}
	// The contested window: the provider resolves NOW and the pump fully
	// processes it (waiter consumed as pre-write).
	s.inject(t, resolvedLine("7"))
	s.sync(t)
	select {
	case out := <-w.ch:
		if out != waiterResolvedPreWrite {
			t.Fatalf("control: want pre-write outcome, got %v", out)
		}
	default:
		t.Fatalf("control: resolved was not routed to the waiter")
	}
	// The old algorithm now writes because stillArmed was observed true.
	if err := s.rt.writeRawResponse(c.Payload); err != nil {
		t.Fatalf("control write: %v", err)
	}
	s.waitWritten(t, 1) // ⇒ provider bytes written for an already-resolved request
}

// TestCodexDelivery_ResolvedDuringWriteClaim: a resolved routed AFTER the
// write claim but BEFORE the written mark (deterministic barrier) yields the
// ambiguous non-retryable conflict — the single write happened, it is never
// reported as success, and a manual retry (which could duplicate the write)
// is impossible.
func TestCodexDelivery_ResolvedDuringWriteClaim(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 5 * time.Second
	d.barrier = func(stage string) {
		if stage != "post-write-claim" {
			return
		}
		s.inject(t, resolvedLine("7"))
		s.sync(t)
	}
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	if receipt.Outcome != DeliveryConflict {
		t.Fatalf("during-write resolution must be the ambiguous conflict: %+v", receipt)
	}
	s.waitWritten(t, 1) // exactly the one claimed write
	commit := s.store.RecordDelivery(receipt)
	if commit.Committed || commit.State != ApprovalDeliveryFailed {
		t.Fatalf("during-write resolution must not commit: %+v", commit)
	}
	// Non-retryable: a retry could otherwise write a second time.
	retry := s.store.ClaimForExecution(ClaimRequest{
		SessionID: s.id, ApprovalID: "codexas-1-7", OptionID: "allow_once",
		Runtime: boundManagedRT(), Requester: completeRequester(), IdempotencyKey: "k1",
	})
	if retry.Outcome != ClaimRetryExhausted {
		t.Fatalf("during-write ambiguity must not be retryable: %s", retry.Outcome)
	}
	if s.written.count() != 1 {
		t.Fatalf("no path may produce a duplicate write")
	}
}

// TestCodexDelivery_ExitInWriteClaimWindow: stop/kill races the SAME state
// transitions — an exit during the claimed write fails the delivery with
// stale_runtime and never commits.
func TestCodexDelivery_ExitInWriteClaimWindow(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 5 * time.Second
	d.barrier = func(stage string) {
		if stage != "post-write-claim" {
			return
		}
		if err := s.managed.Kill(s.id, 1, "", 0); err != nil {
			t.Errorf("kill: %v", err)
		}
	}
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	if receipt.Outcome != DeliveryStaleRuntime {
		t.Fatalf("exit during claimed write must be stale_runtime: %+v", receipt)
	}
	if commit := s.store.RecordDelivery(receipt); commit.Committed {
		t.Fatalf("exit during claimed write must not commit: %+v", commit)
	}
}

// ── P2A-R1 blocker 2: exact action↔response semantic coupling ──

// ingestActionableCustom ingests one actionable record with caller-provided
// delivery material (test-arranged adversarial ingest shapes).
func ingestActionableCustom(t *testing.T, store *AuthoritativeApprovalStore, sessionID, approvalID string, mats []ApprovalDeliveryMaterial) {
	t.Helper()
	ok := store.IngestObserved(ApprovalIngest{
		SessionID: sessionID, LaunchGen: 1, StreamGen: 0,
		Provider: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion,
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: approvalID, SessionID: sessionID, AgentKind: codexAppServerAdapter,
				Kind: "approval", Options: certifiedOptions(), Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance: contract.ProvenanceProviderProtocol, Actionable: true,
			RequiredPerm:     devicetrust.PermTerminalInput,
			DeliveryMaterial: mats,
		}},
	})
	if !ok {
		t.Fatalf("custom actionable ingest must be admitted (bounds-valid material)")
	}
}

// TestCodexDelivery_ResponseSemanticsRejectedPreWrite: a swapped
// option↔response ingest, a non-certified schema identity, an extra result
// field, an unknown/amendment/cancel decision, a non-object result, and a
// missing decision are all rejected BEFORE the wire with zero writes.
func TestCodexDelivery_ResponseSemanticsRejectedPreWrite(t *testing.T) {
	s := newDeliverySession(t, true) // auto-resolve would fire IF a write leaked
	d := NewCodexManagedApprovalDelivery(s.managed)
	d.timeout = 2 * time.Second

	cases := []struct {
		name     string
		option   string
		material []ApprovalDeliveryMaterial
	}{
		{"swapped option/response", "allow_once", []ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: declineBytes("%TOK%")},
			{OptionID: "deny", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: acceptBytes("%TOK%")},
		}},
		{"non-certified schema", "allow_once", []ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: "codex.appserver.decision.v0", ResponseBytes: acceptBytes("%TOK%")},
			{OptionID: "deny", SchemaVersion: "codex.appserver.decision.v0", ResponseBytes: declineBytes("%TOK%")},
		}},
		{"extra result field", "allow_once", []ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: []byte(`{"jsonrpc":"2.0","id":%TOK%,"result":{"decision":"accept","extra":1}}`)},
			{OptionID: "deny", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: declineBytes("%TOK%")},
		}},
		{"amendment decision", "allow_once", []ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: []byte(`{"jsonrpc":"2.0","id":%TOK%,"result":{"decision":"acceptWithExecpolicyAmendment"}}`)},
			{OptionID: "deny", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: declineBytes("%TOK%")},
		}},
		{"cancel decision", "deny", []ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: acceptBytes("%TOK%")},
			{OptionID: "deny", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: []byte(`{"jsonrpc":"2.0","id":%TOK%,"result":{"decision":"cancel"}}`)},
		}},
		{"non-object result", "allow_once", []ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: []byte(`{"jsonrpc":"2.0","id":%TOK%,"result":"accept"}`)},
			{OptionID: "deny", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: declineBytes("%TOK%")},
		}},
		{"missing decision", "allow_once", []ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: []byte(`{"jsonrpc":"2.0","id":%TOK%,"result":{}}`)},
			{OptionID: "deny", SchemaVersion: codexDecisionSchemaV1, ResponseBytes: declineBytes("%TOK%")},
		}},
	}
	for i, tc := range cases {
		tok := strconv.Itoa(100 + i)
		approvalID := "codexas-1-" + tok
		mats := make([]ApprovalDeliveryMaterial, len(tc.material))
		for j, m := range tc.material {
			mats[j] = ApprovalDeliveryMaterial{OptionID: m.OptionID, SchemaVersion: m.SchemaVersion,
				ResponseBytes: bytes.ReplaceAll(m.ResponseBytes, []byte("%TOK%"), []byte(tok))}
		}
		ingestActionableCustom(t, s.store, s.id, approvalID, mats)
		s.armPending(t, tok, approvalID)
		c := mustClaim(t, s.store, s.id, approvalID, tc.option, "k-"+tok)
		receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
		if receipt.Outcome != DeliveryRejected {
			t.Fatalf("%s: must reject pre-write, got %+v", tc.name, receipt)
		}
		if commit := s.store.RecordDelivery(receipt); commit.Committed {
			t.Fatalf("%s: must not commit", tc.name)
		}
	}
	if s.written.count() != 0 {
		t.Fatalf("no semantic-mismatch case may reach the wire: %d writes", s.written.count())
	}
}

// TestCodexDelivery_TamperedBindingOptionRejected: mutating the binding's
// internal OptionID after claim fails BOTH the boundary mapping check (before
// the wire) and the store commit comparison.
func TestCodexDelivery_TamperedBindingOptionRejected(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")

	tampered := c.Binding
	tampered.OptionID = "deny" // claim selected allow_once
	d := NewCodexManagedApprovalDelivery(s.managed)
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: tampered, Payload: c.Payload})
	if receipt.Outcome != DeliveryRejected || s.written.count() != 0 {
		t.Fatalf("tampered option must reject pre-write: %+v", receipt)
	}
	// A forged ACCEPTED receipt carrying the tampered binding cannot commit.
	forged := DeliveryReceipt{
		Outcome: DeliveryAccepted, ClaimToken: c.Token, Binding: tampered,
		ReceiptID: "r1", DeliveredPayloadDigest: c.Binding.PayloadDigest,
	}
	if commit := s.store.RecordDelivery(forged); commit.Committed {
		t.Fatalf("tampered binding must not commit")
	}
	// Control: the untampered binding still succeeds end-to-end.
	d.timeout = 5 * time.Second
	s.resolveAtWrittenMark(t, d, "7")
	receipt = d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: c.Binding, Payload: c.Payload})
	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("control delivery must succeed: %+v", receipt)
	}
	if commit := s.store.RecordDelivery(receipt); !commit.Committed || commit.State != ApprovalApproved {
		t.Fatalf("control commit must be approved: %+v", commit)
	}
}

// TestCodexDelivery_BindingSchemaTamperRejected: mutating the binding's
// internal DeliverySchema after claim fails the boundary check and the store
// commit comparison.
func TestCodexDelivery_BindingSchemaTamperRejected(t *testing.T) {
	s := newDeliverySession(t, false)
	ingestActionable(t, s.store, s.id, "codexas-1-7", "7")
	s.armPending(t, "7", "codexas-1-7")
	c := mustClaim(t, s.store, s.id, "codexas-1-7", "allow_once", "k1")
	if c.Binding.DeliverySchema != codexDecisionSchemaV1 || c.Binding.OptionID != "allow_once" {
		t.Fatalf("claim must bind the selected option and certified schema: %+v", c.Binding)
	}
	tampered := c.Binding
	tampered.DeliverySchema = "codex.appserver.decision.v0"
	d := NewCodexManagedApprovalDelivery(s.managed)
	receipt := d.Deliver(ApprovalDeliveryRequest{ClaimToken: c.Token, Binding: tampered, Payload: c.Payload})
	if receipt.Outcome != DeliveryRejected || s.written.count() != 0 {
		t.Fatalf("tampered schema must reject pre-write: %+v", receipt)
	}
	forged := DeliveryReceipt{
		Outcome: DeliveryAccepted, ClaimToken: c.Token, Binding: tampered,
		ReceiptID: "r1", DeliveredPayloadDigest: c.Binding.PayloadDigest,
	}
	if commit := s.store.RecordDelivery(forged); commit.Committed {
		t.Fatalf("tampered schema binding must not commit")
	}
}
