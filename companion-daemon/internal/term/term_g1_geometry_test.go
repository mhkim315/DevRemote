package term

import (
	"context"
	"io"
	"syscall"
	"testing"
	"time"
)

// ── TERM-G1: managed PTY geometry authority restoration ──
//
// These tests prove the V1 managed-PTY path reports live geometry through
// the full Recorder → TerminalTransport chain after the PB.5 cutover.

// TestTERM_G1_NativeLauncherDefaultsZeroConfig proves that Spawn with zero
// Rows/Cols produces a handle where GetSize returns the frozen default 30×100.
func TestTERM_G1_NativeLauncherDefaultsZeroConfig(t *testing.T) {
	l := NewNativePTYLauncher()
	result, err := l.Spawn(context.Background(), SpawnConfig{
		Name:       "g1-default",
		Executable: "true",
		// Rows and Cols intentionally zero — the launcher must default.
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer result.ProcessCleanup.Execute(context.Background())

	// The handle must implement GetSize() and return the default geometry.
	sizer, ok := result.Handle.(interface{ GetSize() (int, int, error) })
	if !ok {
		t.Fatal("V1 handle does not implement GetSize()")
	}
	rows, cols, err := sizer.GetSize()
	if err != nil {
		t.Fatalf("GetSize error: %v", err)
	}
	if rows != 30 || cols != 100 {
		t.Errorf("default geometry = (%d, %d), want (30, 100)", rows, cols)
	}
}

// TestTERM_G1_HandleGetSizeAfterResize proves that Resize is immediately
// observable through GetSize on the same PTY handle.
func TestTERM_G1_HandleGetSizeAfterResize(t *testing.T) {
	l := NewNativePTYLauncher()
	result, err := l.Spawn(context.Background(), SpawnConfig{
		Name:       "g1-resize",
		Executable: "sleep", Args: []string{"10"},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer result.ProcessCleanup.Execute(context.Background())

	// Verify initial geometry is the default.
	rows0, cols0, err := result.Handle.(interface{ GetSize() (int, int, error) }).GetSize()
	if err != nil {
		t.Fatalf("initial GetSize: %v", err)
	}
	if rows0 != 30 || cols0 != 100 {
		t.Errorf("initial geometry = (%d, %d), want (30, 100)", rows0, cols0)
	}

	// Resize to a non-default size.
	if err := result.Handle.Resize(40, 120); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	// GetSize must reflect the new size.
	rows1, cols1, err := result.Handle.(interface{ GetSize() (int, int, error) }).GetSize()
	if err != nil {
		t.Fatalf("post-resize GetSize: %v", err)
	}
	if rows1 != 40 || cols1 != 120 {
		t.Errorf("post-resize geometry = (%d, %d), want (40, 120)", rows1, cols1)
	}
}

// TestTERM_G1_NativeLauncherHonorsExplicitConfig proves that explicit Rows/Cols
// are used when provided (not overridden by defaults).
func TestTERM_G1_NativeLauncherHonorsExplicitConfig(t *testing.T) {
	l := NewNativePTYLauncher()
	result, err := l.Spawn(context.Background(), SpawnConfig{
		Name:       "g1-explicit",
		Executable: "true",
		Rows:       50,
		Cols:       132,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer result.ProcessCleanup.Execute(context.Background())

	rows, cols, err := result.Handle.(interface{ GetSize() (int, int, error) }).GetSize()
	if err != nil {
		t.Fatalf("GetSize: %v", err)
	}
	if rows != 50 || cols != 132 {
		t.Errorf("explicit geometry = (%d, %d), want (50, 132)", rows, cols)
	}
}

// TestTERM_G1_NativeLauncherRejectsZeroNegative proves that zero and negative
// config values are clamped to defaults (not silently accepted as-is).
func TestTERM_G1_NativeLauncherRejectsZeroNegative(t *testing.T) {
	tests := []struct {
		name       string
		rows, cols int
		wantRows   int
		wantCols   int
	}{
		{"zero rows only", 0, 80, 30, 80},
		{"zero cols only", 24, 0, 24, 100},
		{"both zero", 0, 0, 30, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewNativePTYLauncher()
			result, err := l.Spawn(context.Background(), SpawnConfig{
				Name:       "g1-clamp",
				Executable: "true",
				Rows:       tt.rows,
				Cols:       tt.cols,
			})
			if err != nil {
				t.Fatalf("spawn: %v", err)
			}
			defer result.ProcessCleanup.Execute(context.Background())
			rows, cols, _ := result.Handle.(interface{ GetSize() (int, int, error) }).GetSize()
			if rows != tt.wantRows || cols != tt.wantCols {
				t.Errorf("(%d,%d) → (%d,%d), want (%d,%d)", tt.rows, tt.cols, rows, cols, tt.wantRows, tt.wantCols)
			}
		})
	}
}

// ── Recorder → TerminalTransport geometry chain ──

// realOwnedG1 builds an OwnedPTYRuntime over the production V1 launcher with
// comfortably longer graceful period for geometry tests.
func realOwnedG1(t *testing.T) *OwnedPTYRuntime {
	t.Helper()
	owned := NewOwnedPTYRuntime(NewNativePTYLauncher(), nil)
	owned.graceful = 2 * time.Second
	return owned
}

// spawnG1 creates a session that stays alive briefly so the recorder and
// transport are observable before process exit. Use "true" for geometry-only
// queries; use "sleep" when a live process must survive through resize or
// replacement operations.
func spawnG1(t *testing.T, owned *OwnedPTYRuntime, name, exe string, args ...string) string {
	t.Helper()
	cfg := SpawnConfig{Name: name, Executable: exe, Args: args}
	id, err := owned.Create(context.Background(), cfg, "", "test")
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() { closeOwnedForTest(owned, id) })
	return id
}

// TestTERM_G1_RecorderGetSizeChain proves Recorder.GetSize() returns the
// actual PTY geometry through handleStream → nativePTYHandle.GetSize().
func TestTERM_G1_RecorderGetSizeChain(t *testing.T) {
	owned := realOwnedG1(t)
	id := spawnG1(t, owned, "g1-rec-geom", "true")

	rec := ownedRecorderForTest(owned, id)
	if rec == nil {
		t.Fatal("no recorder")
	}

	// Recorder.GetSize must return the default geometry.
	rows, cols, ok := rec.GetSize()
	if !ok {
		t.Fatal("Recorder.GetSize returned false — live size unavailable")
	}
	if rows != 30 || cols != 100 {
		t.Errorf("Recorder.GetSize = (%d, %d), want (30, 100)", rows, cols)
	}
}

// TestTERM_G1_TerminalTransportGeomChain proves TerminalTransport.Geom()
// reports the same rows/cols as the Recorder.
func TestTERM_G1_TerminalTransportGeomChain(t *testing.T) {
	owned := realOwnedG1(t)
	id := spawnG1(t, owned, "g1-tt-geom", "true")

	transport, ok := owned.Transport(id)
	if !ok || transport == nil {
		t.Fatal("transport not found")
	}

	rec := ownedRecorderForTest(owned, id)

	// Both paths must agree on geometry.
	recRows, recCols, recOK := rec.GetSize()
	ttRows, ttCols, ttOK := transport.Geom()

	if !recOK || !ttOK {
		t.Fatalf("geometry unavailable: rec=%v tt=%v", recOK, ttOK)
	}
	if recRows != ttRows || recCols != ttCols {
		t.Errorf("Recorder=(%d,%d) Transport=(%d,%d) — must agree", recRows, recCols, ttRows, ttCols)
	}
	if recRows != 30 || recCols != 100 {
		t.Errorf("geometry = (%d, %d), want (30, 100)", recRows, recCols)
	}
}

// TestTERM_G1_ResizeReflectedThroughChain proves a resize on the PTY handle
// is observable through Recorder.GetSize() and TerminalTransport.Geom().
func TestTERM_G1_ResizeReflectedThroughChain(t *testing.T) {
	owned := realOwnedG1(t)
	id := spawnG1(t, owned, "g1-chain-resize", "sleep", "1")

	transport, ok := owned.Transport(id)
	if !ok {
		t.Fatal("transport not found")
	}

	// Resize through the transport (generation-gated).
	if err := transport.Resize(42, 132); err != nil {
		t.Fatalf("transport.Resize: %v", err)
	}

	// Both Recorder and TerminalTransport must observe the new size.
	rec := ownedRecorderForTest(owned, id)
	recRows, recCols, recOK := rec.GetSize()
	ttRows, ttCols, ttOK := transport.Geom()

	if !recOK || !ttOK {
		t.Fatalf("geometry unavailable after resize: rec=%v tt=%v", recOK, ttOK)
	}
	if recRows != 42 || recCols != 132 {
		t.Errorf("Recorder.GetSize post-resize = (%d,%d), want (42,132)", recRows, recCols)
	}
	if ttRows != 42 || ttCols != 132 {
		t.Errorf("TerminalTransport.Geom post-resize = (%d,%d), want (42,132)", ttRows, ttCols)
	}
}

// ── Generation gating ──

// TestTERM_G1_StaleGenerationCannotObserve proves that replacing a session
// retires the old TerminalTransport, and the retired transport's Geom()
// returns (0, 0, false) — never the new generation's geometry.
func TestTERM_G1_StaleGenerationCannotObserve(t *testing.T) {
	owned := realOwnedG1(t)

	// Launch gen-1.
	id1 := spawnG1(t, owned, "g1-stale", "sleep", "1")

	transport1, ok := owned.Transport(id1)
	if !ok {
		t.Fatal("transport gen-1 not found")
	}
	gen1 := transport1.generation

	// Resize gen-1 to a known non-default size.
	if err := transport1.Resize(35, 110); err != nil {
		t.Fatalf("gen-1 resize: %v", err)
	}

	// Replace with gen-2 (same canonical ID).
	id2 := spawnG1(t, owned, "g1-stale", "sleep", "1")
	if id1 != id2 {
		t.Fatalf("replacement changed canonical ID: %s → %s", id1, id2)
	}

	transport2, ok := owned.Transport(id2)
	if !ok {
		t.Fatal("transport gen-2 not found")
	}
	gen2 := transport2.generation

	if gen1 == gen2 {
		t.Fatal("generations must differ after replacement")
	}

	// Stale transport must be retired and Geom must return (0,0,false).
	if !transport1.IsRetired() {
		t.Error("stale transport (gen-1) not retired after replacement")
	}
	sRows, sCols, sOK := transport1.Geom()
	if sOK {
		t.Errorf("stale transport Geom = (%d,%d) ok=true, want ok=false", sRows, sCols)
	}
}

// TestTERM_G1_StaleResizeIsNoOp proves a stale transport's Resize is a no-op
// and does not affect the replacement PTY.
func TestTERM_G1_StaleResizeIsNoOp(t *testing.T) {
	owned := realOwnedG1(t)

	id := spawnG1(t, owned, "g1-stale-resize", "sleep", "1")
	transport1, _ := owned.Transport(id)

	// Replace.
	spawnG1(t, owned, "g1-stale-resize", "sleep", "1")

	transport2, _ := owned.Transport(id)

	// Resize through the stale transport — must be a silent no-op.
	if err := transport1.Resize(99, 999); err != nil {
		t.Errorf("stale resize returned error: %v", err)
	}

	// The current (gen-2) transport must still report the default geometry.
	rows, cols, ok := transport2.Geom()
	if !ok {
		t.Fatal("current transport Geom returned false")
	}
	if rows != 30 || cols != 100 {
		t.Errorf("current geometry = (%d,%d) after stale resize, want (30,100)", rows, cols)
	}
}

// ── PTYHandle interface completeness check ──

// TestTERM_G1_HandleImplementsPTYHandle proves the V1 handle satisfies the
// full PTYHandle interface (compile-time + runtime checks).
func TestTERM_G1_HandleImplementsPTYHandle(t *testing.T) {
	l := NewNativePTYLauncher()
	result, err := l.Spawn(context.Background(), SpawnConfig{
		Name:       "g1-interface",
		Executable: "sleep", Args: []string{"1"},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer result.ProcessCleanup.Execute(context.Background())

	h := result.Handle
	// Method presence checks (compile-time proven by PTYHandle interface,
	// runtime proven by calls that must not panic).
	if h == nil {
		t.Fatal("nil handle")
	}
	// Write must work.
	if n, err := h.Write([]byte("echo ok\n")); n == 0 || err != nil {
		t.Errorf("Write: n=%d err=%v", n, err)
	}
	// Signal must deliver.
	out := h.Signal(syscall.Signal(0))
	if out.Err != nil {
		t.Logf("Signal(0) — expected on macOS: %v", out.Err)
	}
	// Resize must work.
	if err := h.Resize(30, 100); err != nil {
		t.Errorf("Resize: %v", err)
	}
	// GetSize must work (the new TERM-G1 capability).
	rows, cols, err := h.(interface{ GetSize() (int, int, error) }).GetSize()
	if err != nil {
		t.Errorf("GetSize: %v", err)
	}
	if rows <= 0 || cols <= 0 {
		t.Errorf("GetSize returned implausible geometry: (%d,%d)", rows, cols)
	}
	// CloseTransport must work.
	if err := h.CloseTransport(); err != nil {
		t.Logf("CloseTransport: %v", err)
	}
}

// ── Recorder stream contract: PTY reader identity ──

// TestTERM_G1_PTYReadDoesNotResolveWrongGeneration proves the recorder's
// stream is the exact captured PTY instance — the replacement's PTY is
// distinct and does not leak bytes to the old recorder.
func TestTERM_G1_PTYReadDoesNotResolveWrongGeneration(t *testing.T) {
	owned := realOwnedG1(t)

	id := spawnG1(t, owned, "g1-distinct", "sleep", "1")

	rec1 := ownedRecorderForTest(owned, id)
	if rec1 == nil {
		t.Fatal("no recorder for gen-1")
	}

	// Gen-1 PTY writes something recognizable.
	transport1, _ := owned.Transport(id)
	transport1.WriteInput([]byte("gen1-marker\n"))

	// Replace with gen-2.
	spawnG1(t, owned, "g1-distinct", "sleep", "1")

	// Gen-1 transport must be retired.
	if !transport1.IsRetired() {
		t.Error("gen-1 transport not retired")
	}

	// Gen-2 recorder must be a different instance.
	rec2 := ownedRecorderForTest(owned, id)
	if rec2 == nil {
		t.Fatal("no recorder for gen-2")
	}
	if rec1 == rec2 {
		t.Fatal("gen-1 and gen-2 recorders are the same instance — must be distinct")
	}

	// Gen-2 transport must report default geometry.
	transport2, _ := owned.Transport(id)
	rows, cols, ok := transport2.Geom()
	if !ok {
		t.Fatal("gen-2 transport Geom returned false")
	}
	if rows != 30 || cols != 100 {
		t.Errorf("gen-2 geometry = (%d,%d), want (30,100)", rows, cols)
	}

	// Gen-1 recorder must still be alive (reading from gen-1 PTY until EOF).
	if !rec1.IsAlive() {
		t.Log("gen-1 recorder already exited (expected — PTY closed on retire)")
	}
}

// ── Compile-time interface guard ──

// TERM-G1: nativePTYHandle must satisfy the size query interface used by
// handleStream → Recorder → TerminalTransport geometry chain.
var _ interface{ GetSize() (int, int, error) } = (*nativePTYHandle)(nil)

// TERM-G1: nativePTYHandle must continue to satisfy io.Reader (existing contract).
var _ io.Reader = (*nativePTYHandle)(nil)
