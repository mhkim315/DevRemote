package term

import (
	"sync"
	"sync/atomic"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// S1.1-R3 (remediation 2) — production wiring of the atomic launch replacement
// transition. The DETERMINISTIC serialization proof (reviewer's reverse-order
// interleaving, strict monotonic publish, gate-blocks-second-transition) lives in
// package transcript (launch_binding_r3_test.go), which can inspect the registry's
// nextGen and published binding directly and uses the launchGateWaitHook seam.
// These term-package tests prove the production boundary
// TelemetryService.RegisterOrReplaceLaunch installs the invalidation high-water
// before publication, isolates sessions, stays monotonic, and is race-clean.

func newTestTelemetry(_ int) *TelemetryService {
	adapter := &stubRegAdapter{name: "controlled_pty"}
	reg := mux.MustNewRegistry(adapter)
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	return NewTelemetryService(reg, NewMemoryEventStore(), nil, nil,
		NewApprovalStore(), NewActivityBuffer(100), ts)
}

func r3Spec(sid string) transcript.LaunchSpec {
	return transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"}
}

// R3-highwater: after a replacement through the production boundary, the moment
// LookupLaunch shows the new generation the store already rejects a prior-launch
// positive write at that generation — proving invalidation preceded publication.
func TestS11R3_LookupNeverAheadOfHighWater(t *testing.T) {
	svc := newTestTelemetry(0)
	sid := "controlled_pty:r3hw"
	defer transcript.RemoveLaunch(sid)
	gen1, _ := svc.RegisterOrReplaceLaunch(r3Spec(sid))
	svc.statusStore.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: gen1, Generation: 5,
		Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if rec, _, _ := svc.statusStore.Current(sid); rec.Status != agent.StatusWorking {
		t.Fatalf("seed: %+v, want working", rec)
	}

	gen2, replaced := svc.RegisterOrReplaceLaunch(r3Spec(sid))
	if !replaced || gen2 <= gen1 {
		t.Fatalf("replace: replaced=%v gen2=%d gen1=%d", replaced, gen2, gen1)
	}
	// Binding shows gen2 AND the store already carries the non-current high-water.
	b := transcript.LookupLaunch(sid)
	if b == nil || b.Generation != gen2 {
		t.Fatalf("binding gen=%v, want %d", b, gen2)
	}
	if rec, _, _ := svc.statusStore.Current(sid); rec.Status != agent.StatusUnknown || !rec.Degraded || rec.LaunchGen != gen2 {
		t.Errorf("post-replace store rec=%+v, want unknown+degraded at gen2", rec)
	}
	// A prior-launch (gen1) positive write at any streamGen is rejected.
	got := svc.statusStore.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: gen1, Generation: 9999,
		Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if got.Status == agent.StatusWorking {
		t.Errorf("prior-launch write committed under new gen: %+v", got)
	}
}

// R3-noskip: two concurrent FIRST registrations of a never-seen session cannot
// both take the no-invalidation path. The gate serializes them; exactly one is a
// first registration (replaced=false), the other a replacement.
func TestS11R3_ConcurrentFirstRegistrationsSerialized(t *testing.T) {
	svc := newTestTelemetry(0)
	sid := "controlled_pty:r3first"
	defer transcript.RemoveLaunch(sid)

	var wg sync.WaitGroup
	var replacedCount int64
	gens := make([]int64, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g, replaced := svc.RegisterOrReplaceLaunch(r3Spec(sid))
			gens[i] = g
			if replaced {
				atomic.AddInt64(&replacedCount, 1)
			}
		}(i)
	}
	wg.Wait()
	if replacedCount != 1 {
		t.Errorf("replacedCount=%d, want exactly 1 (gate serialized first vs replacement)", replacedCount)
	}
	if gens[0] == gens[1] {
		t.Errorf("two registrations shared a generation: %v", gens)
	}
	maxG := gens[0]
	if gens[1] > maxG {
		maxG = gens[1]
	}
	if b := transcript.LookupLaunch(sid); b == nil || b.Generation != maxG {
		t.Errorf("final binding gen=%v, want max %d", b, maxG)
	}
}

// R3-isolate: different sessions replace concurrently and independently — one
// session's transition never blocks or corrupts another's, and each stays monotonic.
func TestS11R3_DifferentSessionsConcurrent(t *testing.T) {
	svc := newTestTelemetry(0)
	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sid := "controlled_pty:r3iso" + string(rune('a'+i))
			defer transcript.RemoveLaunch(sid)
			var last int64
			for j := 0; j < 40; j++ {
				g, _ := svc.RegisterOrReplaceLaunch(r3Spec(sid))
				if g <= last {
					t.Errorf("%s: gen regressed %d → %d", sid, last, g)
				}
				last = g
			}
		}(i)
	}
	wg.Wait()
}

// R3-monotonic: delete/recreate through the boundary stays strictly monotonic.
func TestS11R3_DeleteRecreateMonotonic(t *testing.T) {
	svc := newTestTelemetry(0)
	sid := "controlled_pty:r3mono"
	g1, r1 := svc.RegisterOrReplaceLaunch(r3Spec(sid))
	if r1 {
		t.Error("first registration reported replaced")
	}
	transcript.RemoveLaunch(sid)
	g2, r2 := svc.RegisterOrReplaceLaunch(r3Spec(sid))
	defer transcript.RemoveLaunch(sid)
	if r2 {
		t.Error("recreate after delete reported replaced")
	}
	if g2 <= g1 {
		t.Errorf("recreate gen not higher: g1=%d g2=%d", g1, g2)
	}
}

// R3-race: heavy concurrent replacements + polls; the store's launch generation
// never runs ahead of the published binding (no lower-gen write wins last). Run
// with -race.
func TestS11R3_HeavyConcurrentRace(t *testing.T) {
	svc := newTestTelemetry(0)
	sid := "controlled_pty:r3heavy"
	defer transcript.RemoveLaunch(sid)
	svc.RegisterOrReplaceLaunch(r3Spec(sid))

	var wg sync.WaitGroup
	for r := 0; r < 6; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 60; j++ {
				svc.RegisterOrReplaceLaunch(r3Spec(sid))
			}
		}()
	}
	for p := 0; p < 6; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 60; j++ {
				lg := int64(0)
				if b := transcript.LookupLaunch(sid); b != nil {
					lg = b.Generation
				}
				svc.statusStore.Update(AgentStatusUpdate{SessionID: sid, LaunchGen: lg, Generation: j,
					Adapter: resolvingAdapter{}, Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
				svc.statusStore.Current(sid)
			}
		}()
	}
	wg.Wait()

	b := transcript.LookupLaunch(sid)
	if b == nil {
		t.Fatal("no binding after churn")
	}
	if rec, _, ok := svc.statusStore.Current(sid); ok && rec.LaunchGen > b.Generation {
		t.Errorf("store launchGen=%d ahead of published binding %d", rec.LaunchGen, b.Generation)
	}
}
