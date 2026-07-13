package transcript

import (
	"sync"
	"sync/atomic"
	"testing"
)

// S1.1-R3 (remediation 2) — deterministic proof that the per-session transition
// gate serializes reserve→invalidate→publish, so the reviewer's failing
// interleaving (a lower reserved generation publishing after a higher one) cannot
// occur. These live in package transcript so they can inspect nextGen and the
// published binding directly, and use the launchGateWaitHook seam to force the
// contested ordering WITHOUT sleeps.

func rSpec(sid string) LaunchSpec {
	return LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"}
}

// R3-serialized: A enters its transition and is held INSIDE the invalidation
// callback (gate held, generation already allocated). B then starts; the gate-wait
// hook fires when B reaches the gate, proving B is blocked. While B is blocked we
// assert:
//   - nextGen has NOT advanced for B (B has not allocated its generation yet — it
//     allocates only after acquiring the gate, which A holds);
//   - the published binding still carries A's generation is not yet set (A hasn't
//     published), and once A completes, B allocates a STRICTLY HIGHER generation
//     and publishes it. The binding never regresses.
func TestS11R3_Registry_GateSerializesReserveThenPublish(t *testing.T) {
	resetRegistry(t)
	sid := "controlled_pty:reg-r3"
	defer RemoveLaunch(sid)

	// Seed a first binding so the next two calls are replacements (invalidate runs).
	RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})

	// bAtGate is closed by the hook when B reaches the gate.
	bAtGate := make(chan struct{})
	var hookOnce sync.Once
	// aInside: A is inside its invalidation callback (holds the gate, gen allocated).
	aInside := make(chan struct{})
	releaseA := make(chan struct{})

	var aGen int64
	var genAtBGate int64 = -1
	// armed becomes true only once A is confirmed holding the gate, so the hook
	// ignores the seed call and A's own gate-arrival and fires solely for B.
	var armed int32

	// Install the gate-wait hook: it fires for EVERY RegisterOrReplace before it
	// blocks on the gate. We act only once armed (i.e. only for B, which arrives
	// while A holds the gate).
	launchGateWaitHook = func(s string) {
		if s != sid || atomic.LoadInt32(&armed) == 0 {
			return
		}
		hookOnce.Do(func() {
			// Capture nextGen at the instant B is about to block on the gate. A has
			// already allocated its generation (aGen); B must NOT have allocated yet.
			globalLaunchRegistry.mu.Lock()
			genAtBGate = globalLaunchRegistry.nextGen
			globalLaunchRegistry.mu.Unlock()
			close(bAtGate)
		})
	}
	defer func() { launchGateWaitHook = nil }()

	// A: replacement that blocks inside its invalidation callback.
	go func() {
		g, _ := RegisterOrReplaceLaunch(rSpec(sid), func(gen int64) {
			aGen = gen
			atomic.StoreInt32(&armed, 1) // A now holds the gate; arm the hook for B
			close(aInside)
			<-releaseA // hold the gate
		})
		_ = g
	}()

	<-aInside // A holds the gate with its generation allocated

	// B: a second replacement. It will fire the hook when it reaches the gate, then
	// block because A holds it.
	bDone := make(chan int64, 1)
	go func() {
		g, _ := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
		bDone <- g
	}()

	<-bAtGate // B has reached the gate (deterministic; no sleep)

	// While A holds the gate: B must not have allocated its generation. nextGen at
	// B's gate-arrival equals A's generation (A allocated last; B blocked before
	// its own allocation).
	if genAtBGate != aGen {
		t.Errorf("nextGen at B's gate-arrival = %d, want %d (B must not allocate before acquiring the gate)", genAtBGate, aGen)
	}
	// B must not have published yet.
	select {
	case g := <-bDone:
		t.Fatalf("B published gen %d while A held the gate (not serialized)", g)
	default:
	}

	close(releaseA) // let A finish its transition and publish
	bGen := <-bDone

	// B's generation is strictly newer than A's, and the live binding is B's.
	if bGen <= aGen {
		t.Errorf("B gen=%d not strictly newer than A gen=%d", bGen, aGen)
	}
	b := LookupLaunch(sid)
	if b == nil || b.Generation != bGen {
		t.Fatalf("final binding=%v, want gen %d (B, the latest)", b, bGen)
	}
}

// R3-reverse-reservation: even if we could allocate generations out of order, the
// strict monotonic publish check rejects a stale publish. This drives the check
// directly: publish gen N, then attempt to publish a LOWER generation via a
// hand-rolled transition, and confirm the binding does not regress. (Belt-and-
// suspenders behind the gate serialization proven above.)
func TestS11R3_Registry_StrictMonotonicPublishRejectsStale(t *testing.T) {
	resetRegistry(t)
	sid := "controlled_pty:reg-mono"
	defer RemoveLaunch(sid)

	g1, _ := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
	g2, _ := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
	if g2 <= g1 {
		t.Fatalf("g2=%d not > g1=%d", g2, g1)
	}
	// Directly force the publish path with a stale generation by manipulating the
	// map to a high generation, then confirm a normal transition (which allocates
	// nextGen, now LOWER than the forced value) does not regress it.
	globalLaunchRegistry.mu.Lock()
	bind := globalLaunchRegistry.bindings[sid]
	bind.Generation = globalLaunchRegistry.nextGen + 100 // artificially ahead
	globalLaunchRegistry.bindings[sid] = bind
	forced := bind.Generation
	globalLaunchRegistry.mu.Unlock()

	// A fresh transition allocates nextGen+1 (< forced) and must be REJECTED by the
	// strict check, leaving the higher published generation intact.
	published, replaced := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
	if !replaced {
		t.Error("expected replaced=true")
	}
	if published != forced {
		t.Errorf("stale publish won: published=%d, want %d (no regression)", published, forced)
	}
	if b := LookupLaunch(sid); b == nil || b.Generation != forced {
		t.Errorf("binding regressed to %v, want gen %d", b, forced)
	}
}

// R3-concurrent-replacements: N concurrent replacements of one session; the
// published generation is strictly monotonic in the order transitions complete,
// and the final binding is the maximum generation. No regression at any point —
// verified by snapshotting the binding after every completion.
func TestS11R3_Registry_ConcurrentReplacementsMonotonic(t *testing.T) {
	resetRegistry(t)
	sid := "controlled_pty:reg-conc"
	defer RemoveLaunch(sid)
	RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})

	const n = 24
	var wg sync.WaitGroup
	var mu sync.Mutex
	var maxSeen int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, _ := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
			// Immediately after this transition returns, the live binding must be
			// >= the generation we just published (never below).
			b := LookupLaunch(sid)
			mu.Lock()
			if g > maxSeen {
				maxSeen = g
			}
			cur := int64(0)
			if b != nil {
				cur = b.Generation
			}
			mu.Unlock()
			if cur < g {
				t.Errorf("binding gen %d below our just-published gen %d (regression)", cur, g)
			}
		}()
	}
	wg.Wait()
	b := LookupLaunch(sid)
	if b == nil || b.Generation != maxSeen {
		t.Errorf("final binding=%v, want max published %d", b, maxSeen)
	}
}
