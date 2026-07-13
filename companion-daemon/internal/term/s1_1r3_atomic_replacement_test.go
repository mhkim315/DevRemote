package term

import (
	"sync"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// S1.1-R3 — atomic launch replacement publication. The production boundary
// RegisterOrReplaceLaunch must reserve a strictly-newer generation, invalidate the
// prior status + adapter ingestion AT that generation, and only THEN publish the
// new binding — so a concurrent telemetry poll can never observe the new launch
// generation before the non-current high-water exists, and cannot attach prior
// stream evidence to the new launch.

// R3-order: after RegisterOrReplaceLaunch returns, the published binding's
// generation already has a matching non-current high-water in the store — i.e. the
// invalidation is not deferred to a later poll. A prior-launch positive write is
// rejected immediately.
func TestS11R3_InvalidationPrecedesPublish(t *testing.T) {
	svc := newTestTelemetry(0)
	sid := "controlled_pty:r3o"
	defer transcript.RemoveLaunch(sid)

	spec := transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"}
	gen1, _ := svc.RegisterOrReplaceLaunch(spec)
	svc.statusStore.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: gen1, Generation: 3,
		Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if rec, _, _ := svc.statusStore.Current(sid); rec.Status != agent.StatusWorking {
		t.Fatalf("seed: %+v, want working", rec)
	}

	// Replacement boundary. The moment it returns, the store already reflects the
	// new generation as non-current AND the published binding carries gen2.
	gen2, replaced := svc.RegisterOrReplaceLaunch(spec)
	if !replaced || gen2 <= gen1 {
		t.Fatalf("replace: replaced=%v gen2=%d gen1=%d", replaced, gen2, gen1)
	}
	b := transcript.LookupLaunch(sid)
	if b == nil || b.Generation != gen2 {
		t.Fatalf("published binding gen=%v, want %d", b, gen2)
	}
	rec, _, _ := svc.statusStore.Current(sid)
	if rec.Status != agent.StatusUnknown || !rec.Degraded || rec.LaunchGen != gen2 {
		t.Errorf("published-with-highwater invariant broken: %+v, want unknown+degraded at gen2", rec)
	}

	// Old stream evidence at the PRIOR launch can no longer be committed under the
	// new launch generation, at any streamGen.
	old := svc.statusStore.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: gen1, Generation: 999,
		Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if old.Status == agent.StatusWorking {
		t.Errorf("prior-launch evidence committed under new launch: %+v", old)
	}
}

// R3-race: many concurrent "polls" interleave with a replacement. Each poll models
// what processSession does — it reads the current binding generation and writes a
// positive status at that launchGen. The invariant checked afterward: the store's
// final launch generation is the published one, no prior-launch positive status
// survives, and there is no data race.
func TestS11R3_ConcurrentPollReplacementRace(t *testing.T) {
	svc := newTestTelemetry(0)
	sid := "controlled_pty:r3race"
	defer transcript.RemoveLaunch(sid)
	spec := transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"}

	gen1, _ := svc.RegisterOrReplaceLaunch(spec)
	_ = gen1

	var wg sync.WaitGroup
	// Pollers: read current binding gen, write a positive status at that gen.
	for p := 0; p < 12; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				lg := int64(0)
				if b := transcript.LookupLaunch(sid); b != nil {
					lg = b.Generation
				}
				svc.statusStore.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: lg, Generation: j,
					Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
				svc.statusStore.Current(sid)
			}
		}(p)
	}
	// Replacers: repeatedly run the atomic boundary.
	var lastGen int64
	var genMu sync.Mutex
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				g, _ := svc.RegisterOrReplaceLaunch(spec)
				genMu.Lock()
				if g > lastGen {
					lastGen = g
				}
				genMu.Unlock()
			}
		}()
	}
	wg.Wait()

	// A final replacement establishes a known-latest generation; a prior-launch
	// positive write must not be able to overwrite it.
	finalGen, _ := svc.RegisterOrReplaceLaunch(spec)
	rec, _, ok := svc.statusStore.Current(sid)
	if !ok {
		t.Fatal("no record after churn")
	}
	if rec.LaunchGen != finalGen {
		t.Errorf("final launchGen=%d, want %d (latest replacement)", rec.LaunchGen, finalGen)
	}
	// A stale prior-launch positive write is rejected.
	stale := svc.statusStore.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: finalGen - 1, Generation: 100000,
		Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if stale.LaunchGen != finalGen {
		t.Errorf("stale prior-launch write moved launchGen to %d, want %d", stale.LaunchGen, finalGen)
	}
}

// R3-isolate: replacing session A's launch through the boundary cannot affect B.
func TestS11R3_ReplacementIsolatedPerSession(t *testing.T) {
	svc := newTestTelemetry(0)
	a, b := "controlled_pty:r3a", "controlled_pty:r3b"
	defer transcript.RemoveLaunch(a)
	defer transcript.RemoveLaunch(b)
	specA := transcript.LaunchSpec{SessionID: a, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"}
	specB := transcript.LaunchSpec{SessionID: b, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"}

	genA1, _ := svc.RegisterOrReplaceLaunch(specA)
	genB1, _ := svc.RegisterOrReplaceLaunch(specB)
	svc.statusStore.Update(AgentStatusUpdate{SessionID: a, LaunchGen: genA1, Generation: 1,
		Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(a, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	svc.statusStore.Update(AgentStatusUpdate{SessionID: b, LaunchGen: genB1, Generation: 1,
		Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(b, agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}})

	svc.RegisterOrReplaceLaunch(specA) // replace A only
	if rec, _, _ := svc.statusStore.Current(a); rec.Status != agent.StatusUnknown {
		t.Errorf("A after replacement: status=%q, want unknown", rec.Status)
	}
	if rec, _, _ := svc.statusStore.Current(b); rec.Status != agent.StatusThinking {
		t.Errorf("B affected by A's replacement: status=%q, want thinking", rec.Status)
	}
}

// R3-monotonic: first registration and delete/recreate through the boundary keep
// generations strictly increasing (no silent reuse).
func TestS11R3_BoundaryMonotonicAcrossDeleteRecreate(t *testing.T) {
	svc := newTestTelemetry(0)
	sid := "controlled_pty:r3mono"
	spec := transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"}

	g1, replaced1 := svc.RegisterOrReplaceLaunch(spec)
	if replaced1 {
		t.Error("first registration reported replaced")
	}
	transcript.RemoveLaunch(sid)
	g2, replaced2 := svc.RegisterOrReplaceLaunch(spec)
	defer transcript.RemoveLaunch(sid)
	if replaced2 {
		t.Error("recreation after delete reported replaced (binding was removed)")
	}
	if g2 <= g1 {
		t.Errorf("recreated gen not higher: g1=%d g2=%d", g1, g2)
	}
}

// newTestTelemetry — minimal TelemetryService wiring shared by the R3 tests.
func newTestTelemetry(_ int) *TelemetryService {
	adapter := &stubRegAdapter{name: "controlled_pty"}
	reg := mux.MustNewRegistry(adapter)
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	return NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), nil, nil,
		NewApprovalStore(), NewActivityBuffer(100), ts)
}
