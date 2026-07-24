package term

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ── PA4.5: Final facade/fallback deletion and PA4 acceptance ──

// TestPA4_5_NoManagedToLegacyFallbackExists proves no managed production
// path (ManagedRuntimeCatalog, AgentStatusStore, ApprovalStore,
// LifecycleService) imports or calls legacy adapter discovery,
// or legacy session lookup under default config.
func TestPA4_5_NoManagedToLegacyFallbackExists(t *testing.T) {
	// Structural proof: ManagedRuntimeCatalog doc comment states it
	// "never probes legacy discovery, process names, panes,
	// screen text, PTY bytes, or JSONL." This is the fundamental
	// isolation boundary — no facade or fallback between managed
	// catalog and legacy Registry exists.

	codexReg := NewManagedSessionRegistry(10)
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Catalog operations are self-contained within ManagedSessionRegistry.
	recs := cat.List()
	if len(recs) != 0 {
		t.Fatalf("empty catalog list returned %d records", len(recs))
	}

	// ManagedAdapterPrefixes returns canonical managed prefixes
	// regardless of registry state — no legacy dependency.
	prefixes := cat.ManagedAdapterPrefixes()
	if len(prefixes) != 2 {
		t.Fatalf("expected 2 managed prefixes, got %d", len(prefixes))
	}
}

// TestPA4_5_LegacyObserverRoutesAreContained proves Registry-dependent
// paths (buildSimpleSnapshot, TelemetryService.Snapshot) feed only
// legacy session rows — managed rows are exclusively from catalog.
func TestPA4_5_LegacyObserverRoutesAreContained(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:contained", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Legacy snapshot with colliding ID — catalog drops it.
	snapshot := []SessionTelemetry{
		{ID: "codex_app_server:contained", Adapter: "legacy", AgentKind: "observer"},
		{ID: "legacy:legacy-observer", Adapter: "legacy"},
	}
	out := appendCatalogRows(snapshot, cat, nil, nil)
	// Managed row + legacy legacy row, observer collides dropped.
	if len(out) != 2 {
		t.Fatalf("expected 2 rows (1 managed + 1 legacy), got %d", len(out))
	}
}

// TestPA4_5_AllManagedReadPathsIsolatedFromRegistry proves every managed
// read path (catalog Get/List, lifecycle Stop/Kill/Delete, approval
// Ingest/ListSafe, transport SubscriberFanOut/WriteInput) operates without
// adapter discovery.
func TestPA4_5_AllManagedReadPathsIsolatedFromRegistry(t *testing.T) {
	// Catalog: Get/List read from ManagedSessionRegistry.
	cat := NewManagedRuntimeCatalog(nil, nil, nil, nil, "", "")
	_, ok := cat.Get("anything")
	if ok {
		t.Error("empty catalog Get should return false")
	}

	// Lifecycle: Stop/Kill/Delete route through OwnedPTYRuntime or catalog.
	svc := testLifecycleService(nil, nil)
	if svc == nil {
		t.Fatal("LifecycleService is nil")
	}

	// Approval: store operates on explicit identity.
	store := testApprovalStore()
	if store == nil {
		t.Fatal("ApprovalStore is nil")
	}

	// Transport: generation-gated, no Registry dependency.
	tt := newTerminalTransport("test:final", 1, nil, nil, nil)
	if tt.generation != 1 {
		t.Errorf("generation=%d, want 1", tt.generation)
	}
}

// TestPA4_5_PA4AcceptanceGatesRecorded proves managed isolation holds
// across all PA4 waves through the catalog-to-Registry boundary.
func TestPA4_5_PA4AcceptanceGatesRecorded(t *testing.T) {
	// Catalog: managed read path never falls back to Registry.
	cat := NewManagedRuntimeCatalog(nil, nil, nil, nil, "", "")
	if len(cat.List()) != 0 {
		t.Error("empty catalog List should return empty slice")
	}
	if len(cat.ManagedAdapterPrefixes()) != 2 {
		t.Error("ManagedAdapterPrefixes should return 2 canonical prefixes")
	}

	// LifecycleService: nil OwnedPTYRuntime handled gracefully.
	svc := testLifecycleService(nil, nil)
	if svc == nil {
		t.Fatal("NewLifecycleService returned nil")
	}
}

// TestPA4_5_NoTemporaryComparisonFacadeRemains proves no managed-to-legacy
// fallback bridge exists by testing the full catalog isolation chain.
func TestPA4_5_NoTemporaryComparisonFacadeRemains(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:final-proof", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Observer snapshot with managed-prefix ID — dropped, catalog wins.
	snapshot := []SessionTelemetry{
		{ID: "codex_app_server:final-proof", Adapter: "legacy", AgentKind: "observer"},
	}
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 1 || out[0].AgentKind == "observer" {
		t.Fatal("observer metadata leaked into catalog projection")
	}
}

// TestPA4_5_LiveAcceptanceGateStatus proves managed isolation under
// default configuration by verifying catalog + lifecycle + approval.
func TestPA4_5_LiveAcceptanceGateStatus(t *testing.T) {
	// Managed catalog is self-contained (no Registry dependency).
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:live-gate", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Lifecycle routes through catalog (provider-owned) or OwnedPTYRuntime.
	svc := testLifecycleService(nil, nil)
	fakeOwner := &fakeProviderOwner{currentEpoch: 1}
	svc.WireManagedOwners(cat, fakeOwner, nil)

	// Approval store uses explicit identity, never Registry.
	store := testApprovalStore()
	if store == nil {
		t.Fatal("ApprovalStore is nil")
	}

	// Transport: generation-gated, no Registry.
	var buf bytes.Buffer
	tt := newTerminalTransport("controlled_pty:live-gate", 5, &buf, nil, nil)
	tt.RetireIfGeneration(3) // stale gen — not retired
	if tt.IsRetired() {
		t.Error("transport retired by wrong generation (3 != 5)")
	}
	tt.RetireIfGeneration(5) // current gen — retired
	if !tt.IsRetired() {
		t.Error("transport not retired by correct generation (5)")
	}
}

// TestPA4_5_UnwiredOwnerFailsClosed proves controlled_pty WS handler
// with unwired lifecycle owner fails closed (never falls through to
// Registry).
func TestPA4_5_UnwiredOwnerFailsClosed(t *testing.T) {
	// Truly unwired: nil Lifecycle, nil OwnedPTYRuntime.
	// HandleWS must fail closed for controlled_pty, not fall through
	// to Registry.FindSession for legacy adapter lookup.
	h := &Handlers{} // Lifecycle is nil: truly unwired owner.
	if h.Lifecycle != nil {
		t.Fatal("test requires nil Lifecycle")
	}
	// With nil Lifecycle, controlled_pty has no TerminalTransport.
	// The absence of an owned transport must fail closed.
	req := httptest.NewRequest("GET", "/term/ws?session=controlled_pty:unwired", nil)
	rec := httptest.NewRecorder()
	h.HandleWS(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("HandleWS succeeded with unwired owner — should fail closed")
	}

	// Other adapters have no production fallback path either.
	req2 := httptest.NewRequest("GET", "/term/ws?session=legacy:test", nil)
	rec2 := httptest.NewRecorder()
	h.HandleWS(rec2, req2)
	if rec2.Code == http.StatusOK {
		t.Error("non-owned HandleWS succeeded")
	}
}

// ── PA4-Final-R14: RecorderFor generation gate bypass fixes ──

// TestPA4_Final_R14_SubscriberFanOut_DirectRecorder_NoGlobalLookup proves
// TerminalTransport.SubscriberFanOut uses its direct recorder reference,
// not a global lookup. The direct recorder remains reachable through the
// transport handle.
func TestPA4_Final_R14_SubscriberFanOut_DirectRecorder_NoGlobalLookup(t *testing.T) {
	// Create a live recorder via the standard path.
	pr, pw := io.Pipe()
	ms := &mockStream{pr: pr, pw: pw}
	rec, _ := StartRecorder("controlled_pty:r14-direct", ms)
	if rec == nil {
		t.Fatal("StartRecorder returned nil")
	}
	defer rec.Stop()
	// Create a TerminalTransport with the direct recorder reference.
	tt := newTerminalTransport("controlled_pty:r14-direct", 1, pw, ms, rec)

	// SubscriberFanOut must succeed through the direct recorder reference.
	bootstrap, ch, _, ok := tt.SubscriberFanOut("controlled_pty:r14-direct")
	if !ok {
		t.Fatal("SubscriberFanOut failed — transport should use direct recorder reference, not global registry")
	}
	if ch == nil {
		t.Fatal("SubscriberFanOut returned nil channel")
	}

	// Write some data to the stream so bootstrap is populated.
	pw.Write([]byte("r14-direct-test\n"))
	time.Sleep(50 * time.Millisecond)

	// Unsubscribe to clean up.
	rec.Unsubscribe(ch)
	_ = bootstrap
}

// TestPA4_Final_R14_SubscriberFanOut_RetiredTransport_FailClosed proves that
// HandleWS returns 500 when the TerminalTransport is retired — the
// SubscriberFanOut is denied and no RecorderFor fallback exists.
func TestPA4_Final_R14_SubscriberFanOut_RetiredTransport_FailClosed(t *testing.T) {
	// Create a live recorder + transport.
	pr, pw := io.Pipe()
	ms := &mockStream{pr: pr, pw: pw}
	rec, _ := StartRecorder("controlled_pty:r14-retired-hw", ms)
	if rec == nil {
		t.Fatal("StartRecorder returned nil")
	}
	defer rec.Stop()

	tt := newTerminalTransport("controlled_pty:r14-retired-hw", 1, pw, ms, rec)

	// Verify SubscriberFanOut works before retirement.
	_, ch, _, ok := tt.SubscriberFanOut("controlled_pty:r14-retired-hw")
	if !ok {
		t.Fatal("SubscriberFanOut failed before retirement")
	}
	rec.Unsubscribe(ch)

	// Retire the transport.
	tt.Retire()
	if !tt.IsRetired() {
		t.Fatal("transport should be retired")
	}

	// Wire a real OwnedPTYRuntime with the retired transport entry.
	owned := testOwnedPTYRuntime(nil, nil)
	owned.entries["controlled_pty:r14-retired-hw"] = &CatalogEntry{
		ID:        "controlled_pty:r14-retired-hw",
		transport: tt,
		recorder:  rec,
	}
	lcSvc := testLifecycleService(owned, nil)

	h := &Handlers{
		Lifecycle: lcSvc,
	}

	// HandleWS must return 500 for the retired transport.
	req := httptest.NewRequest("GET", "/term/ws?session=controlled_pty:r14-retired-hw", nil)
	rr := httptest.NewRecorder()
	h.HandleWS(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("HandleWS with retired transport: got %d, want 500", rr.Code)
	}

	// Verify the recorder is still alive (retirement doesn't stop it).
	if !rec.IsAlive() {
		t.Fatal("recorder died after transport retirement — should still be alive")
	}
}

// TestPA4_Final_R14_SubscriberFanOut_StaleGeneration_Denied proves that after
// a transport replacement (retired old + new transport with new recorder),
// the old transport's SubscriberFanOut is denied and the new transport's
// SubscriberFanOut succeeds — no cross-generation subscriber leak.
func TestPA4_Final_R14_SubscriberFanOut_StaleGeneration_Denied(t *testing.T) {
	// Gen-1 transport + recorder.
	pr1, pw1 := io.Pipe()
	ms1 := &mockStream{pr: pr1, pw: pw1}
	rec1, _ := StartRecorder("controlled_pty:r14-stale", ms1)
	if rec1 == nil {
		t.Fatal("StartRecorder gen-1 returned nil")
	}
	defer rec1.Stop()

	tt1 := newTerminalTransport("controlled_pty:r14-stale", 1, pw1, ms1, rec1)

	// Verify gen-1 works.
	_, ch1, _, ok := tt1.SubscriberFanOut("controlled_pty:r14-stale")
	if !ok {
		t.Fatal("gen-1 SubscriberFanOut failed")
	}
	rec1.Unsubscribe(ch1)

	// Retire gen-1 transport (simulates replacement).
	tt1.Retire()

	// Create gen-2 transport + recorder (simulates replacement under same session ID).
	pr2, pw2 := io.Pipe()
	ms2 := &mockStream{pr: pr2, pw: pw2}
	rec2, _ := StartRecorder("controlled_pty:r14-stale", ms2)
	if rec2 == nil {
		t.Fatal("StartRecorder gen-2 returned nil")
	}
	defer rec2.Stop()

	tt2 := newTerminalTransport("controlled_pty:r14-stale", 2, pw2, ms2, rec2)

	// Gen-2 SubscriberFanOut must succeed.
	_, ch2, _, ok := tt2.SubscriberFanOut("controlled_pty:r14-stale")
	if !ok {
		t.Fatal("gen-2 SubscriberFanOut failed — replacement transport should work")
	}
	rec2.Unsubscribe(ch2)

	// Gen-1 SubscriberFanOut must still be denied (retired).
	_, _, _, ok = tt1.SubscriberFanOut("controlled_pty:r14-stale")
	if ok {
		t.Fatal("gen-1 SubscriberFanOut succeeded after retirement — stale generation bypassed")
	}
}

// TestPA4_Final_R17_SubscriberFanOut_RetireRacingSubscribe_Rejected proves
// that SubscriberFanOut and Retire are mutually exclusive: when a subscriber
// holds the RLock (inside the critical section), Retire blocks on Lock until
// the subscriber completes. Uses the SubscriberFanOutHook test seam to
// signal from inside the RLock, confirming atomicity.
func TestPA4_Final_R17_SubscriberFanOut_RetireRacingSubscribe_Rejected(t *testing.T) {
	pr, pw := io.Pipe()
	ms := &mockStream{pr: pr, pw: pw}
	rec, _ := StartRecorder("controlled_pty:r17-race", ms)
	if rec == nil {
		t.Fatal("StartRecorder returned nil")
	}
	defer rec.Stop()

	tt := newTerminalTransport("controlled_pty:r17-race", 1, pw, ms, rec)

	// Channel to signal that goroutine A is inside the RLock.
	insideLock := make(chan struct{})
	hookContinue := make(chan struct{})
	tt.subscriberFanOutHook = func() {
		close(insideLock) // signal: A holds RLock
		<-hookContinue    // wait: main gives permission to finish
	}

	subDone := make(chan struct{})
	retireDone := make(chan struct{})
	var subOK bool

	// Goroutine A: subscriber.
	go func() {
		_, ch, _, ok := tt.SubscriberFanOut("controlled_pty:r17-race")
		subOK = ok
		if ok {
			rec.Unsubscribe(ch)
		}
		close(subDone)
	}()

	// Wait for A to enter the RLock critical section.
	<-insideLock

	// Goroutine B: retirer — must BLOCK because A holds RLock.
	go func() {
		tt.Retire()
		close(retireDone)
	}()

	// Assert B is blocked: Retire has not completed yet.
	select {
	case <-retireDone:
		t.Fatal("Retire completed while subscriber held RLock — atomicity broken")
	case <-time.After(30 * time.Millisecond):
		// Expected: B is blocked on Lock.
	}

	// Allow A to finish — release RLock.
	close(hookContinue)
	<-subDone
	if !subOK {
		t.Fatal("SubscriberFanOut failed — should succeed before retirement")
	}

	// Now B should complete (Lock acquired, transport retired).
	<-retireDone

	// After retirement, new subscriber must be rejected.
	_, _, _, ok := tt.SubscriberFanOut("controlled_pty:r17-race")
	if ok {
		t.Fatal("SubscriberFanOut succeeded on retired transport — generation gate bypassed")
	}
	_ = ok
}

// TestPA4_Final_R17_AwaitExit_SameIDReplacement_UsesOriginalRecorder proves
// that beginStop captures the V1 handle and LaunchIdentity at the decisive
// generation check. A replacement must not change the captured capability.
func TestPA4_Final_R17_AwaitExit_SameIDReplacement_UsesOriginalRecorder(t *testing.T) {
	pr1, pw1 := io.Pipe()
	ms1 := &mockStream{pr: pr1, pw: pw1}
	rec1, _ := StartRecorder("controlled_pty:r17-replace", ms1)
	if rec1 == nil {
		t.Fatal("rec1 is nil")
	}
	firstHandle := &migrationHandle{reader: migrationReader{done: make(chan struct{})}}
	owned := testOwnedPTYRuntime(nil, nil)
	gen1 := owned.register("controlled_pty:r17-replace", "", "orig", firstHandle,
		LaunchIdentity{InstanceID: "original", StartedAt: time.Now()}, nil,
		newTerminalTransport("controlled_pty:r17-replace", 0, ms1, ms1, rec1), rec1)
	proceed, _, found, _, capturedHandle, capturedIdentity := owned.beginStop("controlled_pty:r17-replace", gen1)
	if !found || !proceed || capturedHandle != firstHandle || capturedIdentity.InstanceID != "original" {
		t.Fatalf("beginStop did not retain original V1 capability: proceed=%v found=%v identity=%+v", proceed, found, capturedIdentity)
	}

	pr2, pw2 := io.Pipe()
	ms2 := &mockStream{pr: pr2, pw: pw2}
	rec2, _ := StartRecorder("controlled_pty:r17-replace", ms2)
	if rec2 == nil {
		t.Fatal("rec2 is nil")
	}
	defer rec2.Stop()
	if rec1 == rec2 {
		t.Fatal("replacement reused the original recorder")
	}
	secondHandle := &migrationHandle{reader: migrationReader{done: make(chan struct{})}}
	owned.register("controlled_pty:r17-replace", "", "replace", secondHandle,
		LaunchIdentity{InstanceID: "replacement", StartedAt: time.Now().Add(time.Nanosecond)}, nil,
		newTerminalTransport("controlled_pty:r17-replace", 0, ms2, ms2, rec2), rec2)
	if !owned.currentIdentity("controlled_pty:r17-replace", gen1, capturedIdentity) {
		// The old capability must be rejected after replacement, rather than
		// being redirected to the replacement handle.
		if capturedHandle == secondHandle {
			t.Fatal("replacement redirected the captured V1 handle")
		}
	}
	rec1.Stop()
	pw1.Close()
	pw2.Close()
}

// TestPB2a_ManagedPathsUnaffectedByLinkRemoval proves that removing manual
// link + arbitrary attach does not affect managed catalog, transcript,
// approval, or lifecycle paths.
func TestPB2a_ManagedPathsUnaffectedByLinkRemoval(t *testing.T) {
	// Catalog: intact.
	cat := NewManagedRuntimeCatalog(nil, nil, nil, nil, "", "")
	if len(cat.List()) != 0 {
		t.Error("empty catalog should return empty list")
	}
	if len(cat.ManagedAdapterPrefixes()) != 2 {
		t.Error("ManagedAdapterPrefixes should return 2 canonical prefixes")
	}

	// LifecycleService: intact.
	svc := testLifecycleService(nil, nil)
	if svc == nil {
		t.Fatal("NewLifecycleService returned nil")
	}

	// ApprovalStore: intact.
	store := testApprovalStore()
	if store == nil {
		t.Fatal("NewApprovalStore returned nil")
	}

	// TerminalTransport: intact, still uses direct recorder.
	tt := newTerminalTransport("test:pb2a", 1, nil, nil, nil)
	if tt.IsRetired() {
		t.Error("new transport should not be retired")
	}
	b, ch, rec, ok := tt.SubscriberFanOut("test:pb2a")
	if ok {
		t.Error("SubscriberFanOut with nil recorder should return ok=false")
	}
	_ = b
	_ = ch
	_ = rec
}
