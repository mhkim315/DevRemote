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
		g, _, _ := RegisterOrReplaceLaunch(rSpec(sid), func(gen int64) {
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
		g, _, _ := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
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

	g1, _, _ := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
	g2, _, _ := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
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
	published, replaced, _ := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
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
			g, _, _ := RegisterOrReplaceLaunch(rSpec(sid), func(int64) {})
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

// C1: thousands of unique register/remove cycles leave the transition-
// synchronization state at a FIXED bound (the striped array), never growing with
// the historical session count. The stripe array is a compile-time constant, so we
// assert the invariant structurally: the registry holds no per-session dynamic gate
// state, and the binding map returns to empty after removals.
func TestS11R3_C1_UniqueChurnBoundedSyncState(t *testing.T) {
	resetRegistry(t)
	for i := 0; i < 5000; i++ {
		sid := "controlled_pty:churn-" + itoaFast(i)
		if _, ok := RegisterFirstLaunch(rSpec(sid)); !ok {
			t.Fatalf("first registration %d failed", i)
		}
		RemoveLaunch(sid)
	}
	// Bindings return to empty (bounded); the gate set is a fixed-size array and
	// cannot have grown — there is no per-session map to leak.
	globalLaunchRegistry.mu.Lock()
	nBindings := len(globalLaunchRegistry.bindings)
	globalLaunchRegistry.mu.Unlock()
	if nBindings != 0 {
		t.Errorf("bindings not bounded after churn: %d live (want 0)", nBindings)
	}
	if got := len(globalLaunchRegistry.gates); got != launchGateStripes {
		t.Errorf("gate stripe count = %d, want constant %d (must not grow with sessions)", got, launchGateStripes)
	}
}

// C1: distinct session IDs still map to the same stripe under collision, which
// only serializes them (never corrupts). Two IDs on the same stripe both register
// and remove correctly.
func TestS11R3_C1_StripeCollisionStillCorrect(t *testing.T) {
	resetRegistry(t)
	// Find two distinct session IDs that collide on a stripe.
	var a, b string
	base := launchGateStripe("controlled_pty:collide-0")
	for i := 1; i < 100000 && b == ""; i++ {
		cand := "controlled_pty:collide-" + itoaFast(i)
		if launchGateStripe(cand) == base {
			a, b = "controlled_pty:collide-0", cand
		}
	}
	if b == "" {
		t.Skip("no stripe collision found in range (unexpected but not a failure)")
	}
	defer RemoveLaunch(a)
	defer RemoveLaunch(b)
	if _, ok := RegisterFirstLaunch(rSpec(a)); !ok {
		t.Fatal("register a failed")
	}
	if _, ok := RegisterFirstLaunch(rSpec(b)); !ok {
		t.Fatal("register b failed (collision must not block first registration of a different id)")
	}
	if LookupLaunch(a) == nil || LookupLaunch(b) == nil {
		t.Error("colliding sessions must both have bindings")
	}
}

// C2: a nil-invalidation REPLACEMENT is refused — the existing binding is left
// unchanged and ok=false is returned. A nil callback is never permission to replace.
func TestS11R3_C2_NilInvalidationReplacementRefused(t *testing.T) {
	resetRegistry(t)
	sid := "controlled_pty:c2nil"
	defer RemoveLaunch(sid)

	gen1, ok1 := RegisterFirstLaunch(rSpec(sid))
	if !ok1 {
		t.Fatal("first registration failed")
	}
	// Attempt a replacement through the raw registry with a nil callback.
	published, replaced, ok := globalLaunchRegistry.RegisterOrReplace(
		LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1", PID: 999}, nil)
	if ok {
		t.Error("nil-invalidation replacement was allowed (must fail closed)")
	}
	if !replaced {
		t.Error("expected replaced=true (a binding existed)")
	}
	if published != gen1 {
		t.Errorf("published=%d, want unchanged gen1=%d", published, gen1)
	}
	// The binding is untouched.
	b := LookupLaunch(sid)
	if b == nil || b.Generation != gen1 || b.PID != 0 {
		t.Errorf("binding changed by refused replacement: %+v (want gen %d, PID 0)", b, gen1)
	}
}

// C2: RegisterFirstLaunch fails closed if a binding already exists (it is
// first-registration-only; it never replaces).
func TestS11R3_C2_FirstLaunchFailsClosedOnExisting(t *testing.T) {
	resetRegistry(t)
	sid := "controlled_pty:c2first"
	defer RemoveLaunch(sid)

	gen1, ok1 := RegisterFirstLaunch(rSpec(sid))
	if !ok1 {
		t.Fatal("first registration failed")
	}
	// Second first-registration on the same id must fail closed.
	_, ok2 := RegisterFirstLaunch(LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1", PID: 555})
	if ok2 {
		t.Error("RegisterFirstLaunch replaced an existing binding (must fail closed)")
	}
	if b := LookupLaunch(sid); b == nil || b.Generation != gen1 || b.PID != 0 {
		t.Errorf("existing binding changed: %+v, want gen %d / PID 0", b, gen1)
	}
}

// C2: a normal replacement WITH an invalidation callback still invalidates before
// publishing (callback observed before the new binding is visible).
func TestS11R3_C2_CallbackReplacementInvalidatesBeforePublish(t *testing.T) {
	resetRegistry(t)
	sid := "controlled_pty:c2cb"
	defer RemoveLaunch(sid)
	gen1, _ := RegisterFirstLaunch(rSpec(sid))

	var sawBindingDuringCallback int64 = -1
	gen2, replaced, ok := RegisterOrReplaceLaunch(
		LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1", PID: 42},
		func(gen int64) {
			// At callback time the OLD binding is still published (not yet replaced).
			if b := LookupLaunch(sid); b != nil {
				sawBindingDuringCallback = b.Generation
			}
		})
	if !ok || !replaced || gen2 <= gen1 {
		t.Fatalf("replacement: ok=%v replaced=%v gen2=%d gen1=%d", ok, replaced, gen2, gen1)
	}
	if sawBindingDuringCallback != gen1 {
		t.Errorf("callback saw binding gen %d, want old gen %d (invalidation must precede publish)", sawBindingDuringCallback, gen1)
	}
	if b := LookupLaunch(sid); b == nil || b.Generation != gen2 {
		t.Errorf("post-replace binding gen=%v, want %d", b, gen2)
	}
}

// itoaFast is a tiny base-10 formatter (avoids importing strconv just for tests
// and keeps churn allocation-light).
func itoaFast(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
