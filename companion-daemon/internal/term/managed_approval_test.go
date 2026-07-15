package term

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
)

// ── SP1-P1: structured NON-ACTIONABLE approval observation ──
//
// Every test drives the PRODUCTION pump through the deterministic fake
// launcher. Crafted provider messages are written as raw JSONL lines so the
// exact top-level JSON-RPC id token (including forms float64 marshaling
// cannot express) and the exact byte lengths reach the projection unchanged.

const (
	apprTestThread = "thread-APR"
	apprTestTurn   = "turn-1"
	// Distinctive provider-payload markers that must never leak into any
	// DTO, public projection, or log line.
	apprSecretCmd       = "SECRET-CMD-BYTES-7f3a"
	apprSecretCWD       = "/Users/secret-home/project"
	apprSecretAmendment = "SECRET-AMENDMENT-BYTES-9c1d"
)

// certifiedApprovalRaw builds the evidence-exact certified request shape
// (CP0 wire_accept_pinned.jsonl seq 24) with the given raw id token.
func certifiedApprovalRaw(idToken, turnID, itemID string) string {
	return `{"jsonrpc":"2.0","id":` + idToken + `,"method":"item/commandExecution/requestApproval",` +
		`"params":{"threadId":"` + apprTestThread + `","turnId":"` + turnID + `","itemId":"` + itemID + `",` +
		`"command":["` + apprSecretCmd + `","rm","-rf"],"cwd":"` + apprSecretCWD + `","environmentId":"local",` +
		`"proposedExecpolicyAmendment":{"amendment":"` + apprSecretAmendment + `"},` +
		`"availableDecisions":["accept",{"acceptWithExecpolicyAmendment":{"amendment":"` + apprSecretAmendment + `"}},"cancel"]}}`
}

// approvalSession is one launched managed session driven by a scripted
// provider with an active bound turn (turn-1) and a raw injection channel.
type approvalSession struct {
	managed *ManagedCodexService
	store   *AuthoritativeApprovalStore
	fl      *fakeLauncher
	rec     *pumpRecorder
	id      string
	rt      *codexManagedRuntime
	// providerWrites counts every line the runtime wrote to the provider.
	providerWrites *int64
	// probes issued so far (for deterministic consumption barriers).
	probes int
}

// newApprovalSessionWith launches a managed session with the given sink
// (nil ⇒ SetApprovalStore is never called), starts one prompt turn, and waits
// until the exact turn binding is live (status working).
func newApprovalSessionWith(t *testing.T, store *AuthoritativeApprovalStore) *approvalSession {
	t.Helper()
	var writes int64
	fl := &fakeLauncher{handler: func(method string, id float64, params map[string]any, out func(map[string]any)) {
		atomic.AddInt64(&writes, 1)
		switch method {
		case "initialize":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		case "thread/start":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"threadId": apprTestThread}})
		case "turn/start":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"turn": map[string]any{"id": apprTestTurn}}})
			out(map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{"threadId": apprTestThread, "turn": map[string]any{"id": apprTestTurn}}})
		}
	}}
	managed := newTestManagedService(fl)
	if store != nil {
		if err := managed.SetApprovalStore(store); err != nil {
			t.Fatalf("set approval store: %v", err)
		}
	}
	rec := newPumpRecorder()
	managed.pumpObserver = rec.observe

	id, err := managed.CreateAttached("")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := managed.SubmitPrompt(id, 1, "run the build"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitForStatus(t, managed.Registry(), id, ManagedStatusWorking)

	managed.mu.Lock()
	rt := managed.runtimes[id]
	managed.mu.Unlock()
	if rt == nil {
		t.Fatalf("runtime not published")
	}
	t.Cleanup(func() { _ = managed.Kill(id, 1) })
	return &approvalSession{managed: managed, store: store, fl: fl, rec: rec, id: id, rt: rt, providerWrites: &writes}
}

func newApprovalSession(t *testing.T) *approvalSession {
	t.Helper()
	return newApprovalSessionWith(t, NewAuthoritativeApprovalStore())
}

// injectRaw writes one raw provider JSONL line into the runtime's stdout.
func (s *approvalSession) injectRaw(t *testing.T, raw string) {
	t.Helper()
	s.fl.mu.Lock()
	p := s.fl.procs[len(s.fl.procs)-1]
	s.fl.mu.Unlock()
	if _, err := p.stdoutW.Write([]byte(raw + "\n")); err != nil {
		t.Fatalf("inject raw: %v", err)
	}
}

// sync injects a probe notification and waits until the pump consumed it —
// because the pump is a single goroutine, every previously injected message
// has then been FULLY processed (the observer fires before processing, so a
// later message's observation proves the earlier message completed).
func (s *approvalSession) sync(t *testing.T) {
	t.Helper()
	s.probes++
	s.injectRaw(t, `{"jsonrpc":"2.0","method":"probe/sync","params":{"threadId":"`+apprTestThread+`"}}`)
	s.rec.waitForCount(t, "probe/sync", s.probes)
}

func (s *approvalSession) snapshot() (pending []pendingProviderRequest, rejects int) {
	return s.rt.approvalObservationSnapshot()
}

func waitForSafeApprovals(t *testing.T, store *AuthoritativeApprovalStore, sid string, n int) []SafeApprovalDTO {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := store.ListSafe(sid)
		if len(got) == n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("safe approvals never reached %d (now %d)", n, len(got))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func safeApprovalByID(t *testing.T, store *AuthoritativeApprovalStore, sid, approvalID string) SafeApprovalDTO {
	t.Helper()
	for _, dto := range store.ListSafe(sid) {
		if dto.ID == approvalID {
			return dto
		}
	}
	t.Fatalf("approval %s not found", approvalID)
	return SafeApprovalDTO{}
}

// completeRequester is a fully server-derived requester context with the
// stored permission — the strongest possible caller, so a not_actionable
// refusal below is non-vacuous.
func completeRequester() RequesterContext {
	return RequesterContext{DeviceID: "dev-1", HostID: "host-1", BearerSessionID: "sess-1",
		BootID: "boot-1", Permissions: []string{devicetrust.PermTerminalInput}}
}

// TestManagedApproval_CertifiedRequestNonActionable is the positive control:
// the exactly-certified request creates ONE non-actionable record with zero
// options, one bounded private pending entry preserving the exact id token,
// no claim path, no provider write, and the managed telemetry row carries the
// same bounded safe DTO.
func TestManagedApproval_CertifiedRequestNonActionable(t *testing.T) {
	s := newApprovalSession(t)
	before := atomic.LoadInt64(s.providerWrites)

	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)

	got := waitForSafeApprovals(t, s.store, s.id, 1)
	dto := got[0]
	if dto.Actionable || len(dto.Options) != 0 {
		t.Fatalf("record must be non-actionable with zero options: %+v", dto)
	}
	if dto.ID != "codexas-1-7" || dto.SessionID != s.id || dto.State != "pending" {
		t.Fatalf("unexpected DTO identity: %+v", dto)
	}

	pending, rejects := s.snapshot()
	if len(pending) != 1 || rejects != 0 {
		t.Fatalf("want 1 pending / 0 rejects, got %d/%d", len(pending), rejects)
	}
	p := pending[0]
	if p.idToken != "7" || p.idInt != 7 || p.threadID != apprTestThread ||
		p.turnID != apprTestTurn || p.itemID != "item-4" || !p.hasAmendmentPayload ||
		p.approvalID != "codexas-1-7" {
		t.Fatalf("pending record mismatch: %+v", p)
	}

	// Zero mobile CTA / zero claim path: even a complete requester with the
	// stored permission and the exact bound runtime cannot claim.
	claim := s.store.ClaimForExecution(ClaimRequest{
		SessionID: s.id, ApprovalID: dto.ID, OptionID: "allow_once",
		Runtime:   RuntimeRef{Adapter: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion, LaunchGen: 1, StreamGen: 0},
		Requester: completeRequester(), IdempotencyKey: "k1",
	})
	if claim.Outcome != ClaimNotActionable {
		t.Fatalf("claim must be not_actionable, got %s", claim.Outcome)
	}

	// Zero provider response writes: the daemon wrote nothing new.
	if after := atomic.LoadInt64(s.providerWrites); after != before {
		t.Fatalf("provider writes changed %d -> %d", before, after)
	}

	// The managed telemetry row exposes the SAME bounded safe DTO (no second
	// approval surface).
	rows := appendManagedRows(nil, s.managed, s.store)
	if len(rows) != 1 || len(rows[0].Approvals) != 1 || rows[0].Approvals[0].Actionable {
		t.Fatalf("managed row approvals mismatch: %+v", rows)
	}
}

// TestManagedApproval_ParamsIDCannotSubstitute: a request whose only id is
// params.id creates ZERO state — the top-level JSON-RPC id is the only
// provider request identity (params.id is not even an allowlisted field).
func TestManagedApproval_ParamsIDCannotSubstitute(t *testing.T) {
	s := newApprovalSession(t)
	raw := `{"jsonrpc":"2.0","method":"item/commandExecution/requestApproval",` +
		`"params":{"id":7,"threadId":"` + apprTestThread + `","turnId":"` + apprTestTurn + `","itemId":"item-4",` +
		`"command":["x"],"cwd":"/w","environmentId":"local","availableDecisions":["accept",{"acceptWithExecpolicyAmendment":{}},"cancel"]}}`
	s.injectRaw(t, raw)
	s.sync(t)

	pending, rejects := s.snapshot()
	if len(pending) != 0 || rejects != 1 {
		t.Fatalf("want 0 pending / 1 reject, got %d/%d", len(pending), rejects)
	}
	if got := s.store.ListSafe(s.id); len(got) != 0 {
		t.Fatalf("no record may exist, got %+v", got)
	}
}

// TestManagedApproval_StrictDecoder unit-proves the strict object decoder:
// unknown fields, duplicate keys, non-objects, and trailing content reject.
func TestManagedApproval_StrictDecoder(t *testing.T) {
	allowed := map[string]bool{"a": true, "b": true}
	for name, raw := range map[string]string{
		"unknown field":       `{"a":1,"z":2}`,
		"duplicate key":       `{"a":1,"a":2}`,
		"duplicate allowed":   `{"a":1,"b":2,"b":3}`,
		"non-object":          `[1,2]`,
		"trailing content":    `{"a":1}{}`,
		"trailing garbage":    `{"a":1}x`,
		"non-string key form": `{1:2}`,
	} {
		if _, ok := decodeStrictObject([]byte(raw), allowed); ok {
			t.Fatalf("%s must reject: %s", name, raw)
		}
	}
	out, ok := decodeStrictObject([]byte(`{"a":{"x":1},"b":"v"}`), allowed)
	if !ok || string(out["a"]) != `{"x":1}` || string(out["b"]) != `"v"` {
		t.Fatalf("valid object must decode with exact raw values: %v %v", out, ok)
	}
	// singleObjectKey: duplicate/second key and non-objects reject.
	for name, raw := range map[string]string{
		"two keys":      `{"a":1,"b":2}`,
		"duplicate key": `{"a":1,"a":2}`,
		"empty object":  `{}`,
		"string":        `"a"`,
	} {
		if _, ok := singleObjectKey([]byte(raw)); ok {
			t.Fatalf("singleObjectKey %s must reject: %s", name, raw)
		}
	}
	if key, ok := singleObjectKey([]byte(`{"tag":{"deep":1}}`)); !ok || key != "tag" {
		t.Fatalf("singleObjectKey valid case failed: %q %v", key, ok)
	}
}

// TestManagedApproval_MalformedFieldsRejected: unknown/missing/oversized/
// wrong-type/duplicate fields, a missing or wrong jsonrpc envelope, wrong
// environment, and every uncertified decision fingerprint are rejected with
// zero state.
func TestManagedApproval_MalformedFieldsRejected(t *testing.T) {
	s := newApprovalSession(t)
	base := func(id, params string) string {
		return `{"jsonrpc":"2.0","id":` + id + `,"method":"item/commandExecution/requestApproval","params":` + params + `}`
	}
	goodDecisions := `["accept",{"acceptWithExecpolicyAmendment":{}},"cancel"]`
	goodParams := func(env, decisions string) string {
		return `{"threadId":"` + apprTestThread + `","turnId":"` + apprTestTurn + `","itemId":"item-4",` +
			`"command":["x"],"cwd":"/w","environmentId":"` + env + `","availableDecisions":` + decisions + `}`
	}
	cases := []struct {
		name string
		raw  string
	}{
		{"string id", base(`"7"`, goodParams("local", goodDecisions))},
		{"fractional id", base(`7.5`, goodParams("local", goodDecisions))},
		{"exponent id", base(`1e3`, goodParams("local", goodDecisions))},
		{"oversized id token", base(`123456789012345678901`, goodParams("local", goodDecisions))},
		{"int64 overflow id", base(`9999999999999999999`, goodParams("local", goodDecisions))},
		{"unknown top-level field", `{"jsonrpc":"2.0","id":10,"method":"item/commandExecution/requestApproval","extra":1,"params":` + goodParams("local", goodDecisions) + `}`},
		{"duplicate top-level id", `{"jsonrpc":"2.0","id":10,"id":11,"method":"item/commandExecution/requestApproval","params":` + goodParams("local", goodDecisions) + `}`},
		{"wrong jsonrpc", `{"jsonrpc":"1.0","id":10,"method":"item/commandExecution/requestApproval","params":` + goodParams("local", goodDecisions) + `}`},
		{"unknown params field", base(`11`, `{"threadId":"`+apprTestThread+`","turnId":"`+apprTestTurn+`","itemId":"i","command":["x"],"cwd":"/w","environmentId":"local","reason":"why","availableDecisions":`+goodDecisions+`}`)},
		{"duplicate params key", base(`11`, `{"threadId":"`+apprTestThread+`","turnId":"`+apprTestTurn+`","turnId":"`+apprTestTurn+`","itemId":"i","command":["x"],"cwd":"/w","environmentId":"local","availableDecisions":`+goodDecisions+`}`)},
		{"missing threadId", base(`11`, `{"turnId":"`+apprTestTurn+`","itemId":"i","command":["x"],"cwd":"/w","environmentId":"local","availableDecisions":`+goodDecisions+`}`)},
		{"wrong-type turnId", base(`12`, `{"threadId":"`+apprTestThread+`","turnId":42,"itemId":"i","command":["x"],"cwd":"/w","environmentId":"local","availableDecisions":`+goodDecisions+`}`)},
		{"oversized itemId", base(`13`, `{"threadId":"`+apprTestThread+`","turnId":"`+apprTestTurn+`","itemId":"`+strings.Repeat("x", 200)+`","command":["x"],"cwd":"/w","environmentId":"local","availableDecisions":`+goodDecisions+`}`)},
		{"missing itemId", base(`14`, `{"threadId":"`+apprTestThread+`","turnId":"`+apprTestTurn+`","command":["x"],"cwd":"/w","environmentId":"local","availableDecisions":`+goodDecisions+`}`)},
		{"missing command", base(`15`, `{"threadId":"`+apprTestThread+`","turnId":"`+apprTestTurn+`","itemId":"i","cwd":"/w","environmentId":"local","availableDecisions":`+goodDecisions+`}`)},
		{"missing cwd", base(`16`, `{"threadId":"`+apprTestThread+`","turnId":"`+apprTestTurn+`","itemId":"i","command":["x"],"environmentId":"local","availableDecisions":`+goodDecisions+`}`)},
		{"wrong environment", base(`17`, goodParams("remote", goodDecisions))},
		{"missing environment", base(`18`, `{"threadId":"`+apprTestThread+`","turnId":"`+apprTestTurn+`","itemId":"i","command":["x"],"cwd":"/w","availableDecisions":`+goodDecisions+`}`)},
		{"missing decisions", base(`19`, `{"threadId":"`+apprTestThread+`","turnId":"`+apprTestTurn+`","itemId":"i","command":["x"],"cwd":"/w","environmentId":"local"}`)},
		{"decisions not a list", base(`20`, goodParams("local", `"accept"`))},
		{"uncertified tag set", base(`21`, goodParams("local", `["accept","decline"]`))},
		{"extra tag", base(`22`, goodParams("local", `["accept",{"acceptWithExecpolicyAmendment":{}},"cancel","extra"]`))},
		{"wrong order", base(`23`, goodParams("local", `["cancel","accept",{"acceptWithExecpolicyAmendment":{}}]`))},
		{"multi-key object element", base(`24`, goodParams("local", `["accept",{"acceptWithExecpolicyAmendment":{},"other":{}},"cancel"]`))},
		{"duplicate-key object element", base(`25`, goodParams("local", `["accept",{"acceptWithExecpolicyAmendment":{},"acceptWithExecpolicyAmendment":{}},"cancel"]`))},
		{"empty decisions", base(`26`, goodParams("local", `[]`))},
		{"foreign thread", base(`27`, `{"threadId":"thread-OTHER","turnId":"`+apprTestTurn+`","itemId":"i","command":["x"],"cwd":"/w","environmentId":"local","availableDecisions":`+goodDecisions+`}`)},
		{"missing params", `{"jsonrpc":"2.0","id":28,"method":"item/commandExecution/requestApproval"}`},
	}
	for _, tc := range cases {
		s.injectRaw(t, tc.raw)
	}
	s.sync(t)

	pending, _ := s.snapshot()
	if len(pending) != 0 {
		t.Fatalf("no variant may arm pending state: %+v", pending)
	}
	if got := s.store.ListSafe(s.id); len(got) != 0 {
		t.Fatalf("no variant may create a record: %+v", got)
	}
}

// ── B2: individual byte bounds, one-under and one-over ──

// certifiedRawSized builds a certified request whose params raw object is
// EXACTLY paramsLen bytes (padded inside the opaque command field), with an
// optional exact-size amendment marker and decision payload filler.
func certifiedRawSized(t *testing.T, idToken string, paramsLen int) string {
	t.Helper()
	build := func(pad int) string {
		return `{"threadId":"` + apprTestThread + `","turnId":"` + apprTestTurn + `","itemId":"item-B",` +
			`"command":["x","` + strings.Repeat("p", pad) + `"],"cwd":"/w","environmentId":"local",` +
			`"availableDecisions":["accept",{"acceptWithExecpolicyAmendment":{}},"cancel"]}`
	}
	basePad := len(build(0))
	if paramsLen < basePad {
		t.Fatalf("paramsLen %d below base %d", paramsLen, basePad)
	}
	params := build(paramsLen - basePad)
	if len(params) != paramsLen {
		t.Fatalf("params sizing bug: want %d got %d", paramsLen, len(params))
	}
	return `{"jsonrpc":"2.0","id":` + idToken + `,"method":"item/commandExecution/requestApproval","params":` + params + `}`
}

// certifiedRawWithAmendmentMarker builds a certified request whose raw
// proposedExecpolicyAmendment field is EXACTLY markerLen bytes.
func certifiedRawWithAmendmentMarker(t *testing.T, idToken string, markerLen int) string {
	t.Helper()
	shell := `{"pad":""}`
	if markerLen < len(shell) {
		t.Fatalf("markerLen %d below shell %d", markerLen, len(shell))
	}
	marker := `{"pad":"` + strings.Repeat("a", markerLen-len(shell)) + `"}`
	if len(marker) != markerLen {
		t.Fatalf("marker sizing bug: want %d got %d", markerLen, len(marker))
	}
	return `{"jsonrpc":"2.0","id":` + idToken + `,"method":"item/commandExecution/requestApproval",` +
		`"params":{"threadId":"` + apprTestThread + `","turnId":"` + apprTestTurn + `","itemId":"item-B",` +
		`"command":["x"],"cwd":"/w","environmentId":"local","proposedExecpolicyAmendment":` + marker + `,` +
		`"availableDecisions":["accept",{"acceptWithExecpolicyAmendment":{}},"cancel"]}}`
}

// certifiedRawWithDecisionElement builds a certified request whose amendment
// DECISION element is EXACTLY elementLen raw bytes.
func certifiedRawWithDecisionElement(t *testing.T, idToken string, elementLen int) string {
	t.Helper()
	shell := `{"acceptWithExecpolicyAmendment":{"pad":""}}`
	if elementLen < len(shell) {
		t.Fatalf("elementLen %d below shell %d", elementLen, len(shell))
	}
	element := `{"acceptWithExecpolicyAmendment":{"pad":"` + strings.Repeat("a", elementLen-len(shell)) + `"}}`
	if len(element) != elementLen {
		t.Fatalf("element sizing bug: want %d got %d", elementLen, len(element))
	}
	return `{"jsonrpc":"2.0","id":` + idToken + `,"method":"item/commandExecution/requestApproval",` +
		`"params":{"threadId":"` + apprTestThread + `","turnId":"` + apprTestTurn + `","itemId":"item-B",` +
		`"command":["x"],"cwd":"/w","environmentId":"local",` +
		`"availableDecisions":["accept",` + element + `,"cancel"]}}`
}

// TestManagedApproval_ByteBounds: every individual bound admits at exactly
// the bound and rejects with ZERO new state one byte over. The whole-message
// bound is the outer rampart: with the strict top-level allowlist, the
// largest reachable certified message is envelope + params bound (proven
// under it here), so its one-over case necessarily also exceeds the params
// bound — the assertion is total rejection.
func TestManagedApproval_ByteBounds(t *testing.T) {
	s := newApprovalSession(t)
	records := 0
	expectAdmitted := func(name, raw string) {
		t.Helper()
		s.injectRaw(t, raw)
		s.sync(t)
		records++
		if got := s.store.ListSafe(s.id); len(got) != records {
			t.Fatalf("%s: want %d records, got %d", name, records, len(got))
		}
	}
	expectRejected := func(name, raw string) {
		t.Helper()
		s.injectRaw(t, raw)
		s.sync(t)
		if got := s.store.ListSafe(s.id); len(got) != records {
			t.Fatalf("%s: record count changed to %d (want %d)", name, len(got), records)
		}
	}

	// params: exactly at bound admits; one over rejects. The at-bound message
	// is also the largest REACHABLE certified message and is under the
	// whole-message bound (asserted explicitly).
	under := certifiedRawSized(t, "41", maxApprovalParamsBytes)
	if len(under) >= maxApprovalMessageBytes {
		t.Fatalf("params-at-bound message %d must be under the message bound %d", len(under), maxApprovalMessageBytes)
	}
	expectAdmitted("params one-under", under)
	expectRejected("params one-over", certifiedRawSized(t, "42", maxApprovalParamsBytes+1))

	// decision element: exactly at bound admits; one over rejects.
	expectAdmitted("decision element one-under", certifiedRawWithDecisionElement(t, "43", maxDecisionElementBytes))
	expectRejected("decision element one-over", certifiedRawWithDecisionElement(t, "44", maxDecisionElementBytes+1))

	// amendment marker: exactly at bound admits; one over rejects.
	expectAdmitted("amendment marker one-under", certifiedRawWithAmendmentMarker(t, "45", maxAmendmentMarkerBytes))
	expectRejected("amendment marker one-over", certifiedRawWithAmendmentMarker(t, "46", maxAmendmentMarkerBytes+1))

	// whole message one-over: total exactly bound+1 → rejected outright.
	overBase := certifiedRawSized(t, "47", maxApprovalParamsBytes)
	pad := maxApprovalMessageBytes + 1 - len(overBase)
	over := certifiedRawSized(t, "47", maxApprovalParamsBytes+pad)
	if len(over) != maxApprovalMessageBytes+1 {
		t.Fatalf("message sizing bug: want %d got %d", maxApprovalMessageBytes+1, len(over))
	}
	expectRejected("message one-over", over)

	pending, _ := s.snapshot()
	if len(pending) != records {
		t.Fatalf("pending (%d) diverged from records (%d)", len(pending), records)
	}
}

// TestManagedApproval_WrongAuthorityVersionRejected: a runtime whose pinned
// config carries a non-certified (or display-shaped) authority version
// creates zero state even for an otherwise perfect request.
func TestManagedApproval_WrongAuthorityVersionRejected(t *testing.T) {
	for _, version := range []string{"0.144.4", "codex-cli 0.144.1", ""} {
		fl := &fakeLauncher{handler: func(method string, id float64, params map[string]any, out func(map[string]any)) {
			switch method {
			case "initialize":
				out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
			case "thread/start":
				out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"threadId": apprTestThread}})
			case "turn/start":
				out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"turn": map[string]any{"id": apprTestTurn}}})
				out(map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{"threadId": apprTestThread, "turn": map[string]any{"id": apprTestTurn}}})
			}
		}}
		store := NewAuthoritativeApprovalStore()
		managed := NewManagedCodexService(CodexAppServerEntryConfig{
			Bin: "/pinned/toolchain/node_modules/.bin/codex", Version: "codex-cli 0.144.1",
			AuthorityVersion: version,
		}, fl)
		managed.verify = func() error { return nil }
		if err := managed.SetApprovalStore(store); err != nil {
			t.Fatalf("set approval store: %v", err)
		}
		rec := newPumpRecorder()
		managed.pumpObserver = rec.observe

		id, err := managed.CreateAttached("")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := managed.SubmitPrompt(id, 1, "run"); err != nil {
			t.Fatalf("prompt: %v", err)
		}
		waitForStatus(t, managed.Registry(), id, ManagedStatusWorking)
		sess := &approvalSession{managed: managed, store: store, fl: fl, rec: rec, id: id}
		sess.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
		sess.sync(t)
		if got := store.ListSafe(id); len(got) != 0 {
			t.Fatalf("version %q must reject: %+v", version, got)
		}
		_ = managed.Kill(id, 1)
	}
}

// TestManagedApproval_DuplicateRequestID: the same native id observed twice
// never creates a second authority record or a second pending entry.
func TestManagedApproval_DuplicateRequestID(t *testing.T) {
	s := newApprovalSession(t)
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)

	pending, rejects := s.snapshot()
	if len(pending) != 1 || rejects != 1 {
		t.Fatalf("want 1 pending / 1 reject, got %d/%d", len(pending), rejects)
	}
	if got := waitForSafeApprovals(t, s.store, s.id, 1); got[0].ID != "codexas-1-7" {
		t.Fatalf("exactly one record expected: %+v", got)
	}
}

// TestManagedApproval_NilSinkArmsNothing (B3): without a wired approval store
// no admitted record is possible, so a certified request arms NOTHING —
// private pending state can never exist without its store record.
func TestManagedApproval_NilSinkArmsNothing(t *testing.T) {
	s := newApprovalSessionWith(t, nil)
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	pending, rejects := s.snapshot()
	if len(pending) != 0 || rejects != 1 {
		t.Fatalf("nil sink must arm nothing: pending=%d rejects=%d", len(pending), rejects)
	}
}

// TestManagedApproval_StoreRefusalArmsNothing (B3): when the store refuses
// admission (a newer generation was already observed for the session), the
// certified request creates NO pending entry either — pending and record are
// one outcome.
func TestManagedApproval_StoreRefusalArmsNothing(t *testing.T) {
	s := newApprovalSession(t)
	// The store has already seen a NEWER generation for this session id.
	s.store.Ingest(ApprovalIngest{
		SessionID: s.id, LaunchGen: 99, StreamGen: 0,
		Provider: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion,
		Items: []ApprovalIngestItem{{
			Approval:     agent.AgentApproval{ID: "codexas-99-1", SessionID: s.id, AgentKind: codexAppServerAdapter, Kind: "approval", Source: agent.SourceJSONL, Confidence: 1},
			Provenance:   contract.ProvenanceProviderProtocol,
			Actionable:   false,
			RequiredPerm: devicetrust.PermTerminalInput,
		}},
	})
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	pending, rejects := s.snapshot()
	if len(pending) != 0 || rejects != 1 {
		t.Fatalf("store refusal must arm nothing: pending=%d rejects=%d", len(pending), rejects)
	}
	got := s.store.ListSafe(s.id)
	if len(got) != 1 || got[0].ID != "codexas-99-1" {
		t.Fatalf("only the pre-existing newer-generation record may exist: %+v", got)
	}
}

// TestManagedApproval_TurnBindingFailClosed: a request bound to a completed
// turn, to a never-bound turn, or arriving after input close creates zero
// state.
func TestManagedApproval_TurnBindingFailClosed(t *testing.T) {
	s := newApprovalSession(t)

	// Never-bound turn during the active turn.
	s.injectRaw(t, certifiedApprovalRaw("31", "turn-999", "item-1"))
	s.sync(t)
	if pending, _ := s.snapshot(); len(pending) != 0 {
		t.Fatalf("foreign turn must not arm state: %+v", pending)
	}

	// Complete turn-1, then a request still naming turn-1.
	s.injectRaw(t, `{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"`+apprTestThread+`","turn":{"id":"`+apprTestTurn+`"}}}`)
	s.injectRaw(t, certifiedApprovalRaw("32", apprTestTurn, "item-2"))
	s.sync(t)
	if pending, _ := s.snapshot(); len(pending) != 0 {
		t.Fatalf("completed turn must not arm state: %+v", pending)
	}

	// Closed input (stop/kill first phase) while the pump is still alive.
	if err := s.managed.SubmitPrompt(s.id, 1, "again"); err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	waitForStatus(t, s.managed.Registry(), s.id, ManagedStatusWorking)
	s.rt.closeInput()
	s.injectRaw(t, certifiedApprovalRaw("33", apprTestTurn, "item-3"))
	s.sync(t)
	pending, _ := s.snapshot()
	if len(pending) != 0 {
		t.Fatalf("closed runtime must not arm state: %+v", pending)
	}
	if got := s.store.ListSafe(s.id); len(got) != 0 {
		t.Fatalf("no record may exist: %+v", got)
	}
}

// TestManagedApproval_BoundedPendingExhaustion: the 5th concurrent pending
// request fails closed with no record.
func TestManagedApproval_BoundedPendingExhaustion(t *testing.T) {
	s := newApprovalSession(t)
	for i := 1; i <= maxPendingApprovals+1; i++ {
		s.injectRaw(t, certifiedApprovalRaw(fmt.Sprintf("%d", i), apprTestTurn, fmt.Sprintf("item-%d", i)))
	}
	s.sync(t)
	pending, rejects := s.snapshot()
	if len(pending) != maxPendingApprovals || rejects != 1 {
		t.Fatalf("want %d pending / 1 reject, got %d/%d", maxPendingApprovals, len(pending), rejects)
	}
	if got := s.store.ListSafe(s.id); len(got) != maxPendingApprovals {
		t.Fatalf("want %d records, got %d", maxPendingApprovals, len(got))
	}
}

// TestManagedApproval_ResolvedInvalidatesExactRecord (B4): an exact
// provider-side resolution drops the pending observation AND invalidates
// ONLY that record — never a success, never another record. Foreign
// requestIds/threads and resolutions before any request are inert, and a
// re-sent request for a resolved id creates no new state.
func TestManagedApproval_ResolvedInvalidatesExactRecord(t *testing.T) {
	s := newApprovalSession(t)
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.injectRaw(t, certifiedApprovalRaw("8", apprTestTurn, "item-5"))
	s.sync(t)
	if pending, _ := s.snapshot(); len(pending) != 2 {
		t.Fatalf("arm failed: %+v", pending)
	}

	// Foreign requestId and foreign thread: inert.
	s.injectRaw(t, `{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"`+apprTestThread+`","requestId":99}}`)
	s.injectRaw(t, `{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"thread-OTHER","requestId":7}}`)
	// Resolved BEFORE any request for that id: nothing created.
	s.injectRaw(t, `{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"`+apprTestThread+`","requestId":55}}`)
	s.sync(t)
	if pending, _ := s.snapshot(); len(pending) != 2 {
		t.Fatalf("foreign resolved must be inert: %+v", pending)
	}
	if dto := safeApprovalByID(t, s.store, s.id, "codexas-1-7"); dto.State != "pending" {
		t.Fatalf("foreign resolved must not change the record: %+v", dto)
	}

	// Exact resolution: pending entry dropped, THAT record invalidated (a
	// no-longer-open intervention is never displayed as current), the OTHER
	// record untouched, and nothing recorded as a success.
	s.injectRaw(t, `{"jsonrpc":"2.0","method":"serverRequest/resolved","params":{"threadId":"`+apprTestThread+`","requestId":7}}`)
	s.sync(t)
	if pending, _ := s.snapshot(); len(pending) != 1 || pending[0].idToken != "8" {
		t.Fatalf("exact resolved must drop only pending 7: %+v", pending)
	}
	if dto := safeApprovalByID(t, s.store, s.id, "codexas-1-7"); dto.State != string(ApprovalInvalidated) {
		t.Fatalf("resolved record must be invalidated, not pending/success: %+v", dto)
	}
	if dto := safeApprovalByID(t, s.store, s.id, "codexas-1-8"); dto.State != "pending" {
		t.Fatalf("other record must be untouched: %+v", dto)
	}

	// A duplicate of the RESOLVED id after resolution: the store record is
	// terminal, so nothing is admitted and nothing is armed.
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	if pending, _ := s.snapshot(); len(pending) != 1 {
		t.Fatalf("re-sent resolved id must not re-arm: %+v", pending)
	}
	if dto := safeApprovalByID(t, s.store, s.id, "codexas-1-7"); dto.State != string(ApprovalInvalidated) {
		t.Fatalf("resolved record must stay invalidated: %+v", dto)
	}
}

// TestManagedApproval_TurnCompletionInvalidatesRecords (B4): completing the
// exact turn drops its pending observations and invalidates ONLY their
// records.
func TestManagedApproval_TurnCompletionInvalidatesRecords(t *testing.T) {
	s := newApprovalSession(t)
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	waitForSafeApprovals(t, s.store, s.id, 1)

	s.injectRaw(t, `{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"`+apprTestThread+`","turn":{"id":"`+apprTestTurn+`"}}}`)
	s.sync(t)
	if pending, _ := s.snapshot(); len(pending) != 0 {
		t.Fatalf("turn completion must drop pending: %+v", pending)
	}
	if dto := safeApprovalByID(t, s.store, s.id, "codexas-1-7"); dto.State != string(ApprovalInvalidated) {
		t.Fatalf("turn completion must invalidate the record: %+v", dto)
	}
}

// TestManagedApproval_ExitInvalidatesAndDeleteClears: child exit invalidates
// the session's records; Delete drops them entirely.
func TestManagedApproval_ExitInvalidatesAndDeleteClears(t *testing.T) {
	s := newApprovalSession(t)
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	waitForSafeApprovals(t, s.store, s.id, 1)

	if err := s.managed.Kill(s.id, 1); err != nil {
		t.Fatalf("kill: %v", err)
	}
	got := waitForSafeApprovals(t, s.store, s.id, 1)
	if got[0].State != string(ApprovalInvalidated) {
		t.Fatalf("exit must invalidate the record: %+v", got)
	}
	if pending, _ := s.snapshot(); len(pending) != 0 {
		t.Fatalf("exit must drop pending state: %+v", pending)
	}

	if err := s.managed.Delete(s.id, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := s.store.ListSafe(s.id); len(got) != 0 {
		t.Fatalf("delete must clear records: %+v", got)
	}
}

// TestManagedApproval_StaleGenerationIngestDropped: the frozen store's
// generation high-water drops a managed-shaped ingest from an older epoch
// once a newer epoch has been observed for the session, and IngestObserved
// reports the refusal.
func TestManagedApproval_StaleGenerationIngestDropped(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	sid := codexAppServerAdapter + ":codex-app-x"
	ingest := func(launchGen int64, approvalID string) bool {
		return store.IngestObserved(ApprovalIngest{
			SessionID: sid, LaunchGen: launchGen, StreamGen: 0,
			Provider: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion,
			Items: []ApprovalIngestItem{{
				Approval:     agent.AgentApproval{ID: approvalID, SessionID: sid, AgentKind: codexAppServerAdapter, Kind: "approval", Source: agent.SourceJSONL, Confidence: 1},
				Provenance:   contract.ProvenanceProviderProtocol,
				Actionable:   false,
				RequiredPerm: devicetrust.PermTerminalInput,
			}},
		})
	}
	if !ingest(2, "codexas-2-9") { // establishes the generation high-water
		t.Fatalf("current-generation ingest must be admitted")
	}
	if ingest(1, "codexas-1-7") { // stale epoch: must be dropped entirely
		t.Fatalf("stale-generation ingest must be refused")
	}
	got := store.ListSafe(sid)
	if len(got) != 1 || got[0].ID != "codexas-2-9" {
		t.Fatalf("stale-generation ingest must be dropped: %+v", got)
	}
}

// TestManagedApproval_NoProviderBytesInDTOsOrLogs: the distinctive command,
// CWD, amendment payload, and raw JSON-RPC markers never appear in the safe
// DTO, the public list, the managed telemetry rows, or the process log.
func TestManagedApproval_NoProviderBytesInDTOsOrLogs(t *testing.T) {
	var logBuf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(prev)

	s := newApprovalSession(t)
	s.injectRaw(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	waitForSafeApprovals(t, s.store, s.id, 1)

	var surfaces []byte
	for _, v := range []any{
		s.store.ListSafe(s.id),
		s.store.List(s.id),
		appendManagedRows(nil, s.managed, s.store),
	} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		surfaces = append(surfaces, b...)
	}
	surfaces = append(surfaces, logBuf.Bytes()...)
	for _, secret := range []string{apprSecretCmd, apprSecretCWD, apprSecretAmendment, "jsonrpc", "requestApproval", "availableDecisions", "proposedExecpolicyAmendment"} {
		if bytes.Contains(surfaces, []byte(secret)) {
			t.Fatalf("provider material %q leaked into a public surface or log", secret)
		}
	}
}

// TestManagedApproval_LiveWireShapeAdmitted pins the EXACT lines captured
// from the live pinned-0.144.1 app-server during SP1-P3 (paths/ids
// substituted): the real server request and resolved notification carry NO
// jsonrpc member, the request additionally carries startedAtMs and
// commandActions, its native id starts at 0, and the amendment payload is an
// array. The certified projection must admit both — and their content fields
// must still never leak.
func TestManagedApproval_LiveWireShapeAdmitted(t *testing.T) {
	s := newApprovalSession(t)
	raw := `{"method":"item/commandExecution/requestApproval","id":0,"params":{` +
		`"threadId":"` + apprTestThread + `","turnId":"` + apprTestTurn + `",` +
		`"itemId":"exec-b02b3c0d-3adf-4ba4-8e00-0dbf97403627","startedAtMs":1784115559140,` +
		`"environmentId":"local","command":"/bin/zsh -lc '/bin/sh -c \"` + apprSecretCmd + `\"'",` +
		`"cwd":"` + apprSecretCWD + `",` +
		`"commandActions":[{"type":"unknown","command":"/bin/sh -c \"` + apprSecretCmd + `\""}],` +
		`"proposedExecpolicyAmendment":["/bin/sh","-c","` + apprSecretAmendment + `"],` +
		`"availableDecisions":["accept",{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["/bin/sh","-c","` + apprSecretAmendment + `"]}},"cancel"]}}`
	s.injectRaw(t, raw)
	s.sync(t)
	got := waitForSafeApprovals(t, s.store, s.id, 1)
	if got[0].ID != "codexas-1-0" || got[0].Actionable {
		t.Fatalf("live shape must be admitted as the non-actionable observation: %+v", got)
	}
	pending, rejects := s.snapshot()
	if len(pending) != 1 || rejects != 0 || pending[0].idToken != "0" || !pending[0].hasAmendmentPayload {
		t.Fatalf("live shape must arm exactly one pending record: %+v rejects=%d", pending, rejects)
	}
	b, _ := json.Marshal(s.store.ListSafe(s.id))
	for _, secret := range []string{apprSecretCmd, apprSecretCWD, apprSecretAmendment, "commandActions", "startedAtMs"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("live-shape field %q leaked into the safe DTO", secret)
		}
	}
	// The live resolved shape (no jsonrpc) must route: it consumes the
	// pending observation and invalidates the record.
	s.injectRaw(t, `{"method":"serverRequest/resolved","params":{"threadId":"`+apprTestThread+`","requestId":0}}`)
	s.sync(t)
	if pending, _ := s.snapshot(); len(pending) != 0 {
		t.Fatalf("live resolved shape must consume the pending record: %+v", pending)
	}
	if dto := safeApprovalByID(t, s.store, s.id, "codexas-1-0"); dto.State != string(ApprovalInvalidated) {
		t.Fatalf("live resolved shape must invalidate the record: %+v", dto)
	}
}
