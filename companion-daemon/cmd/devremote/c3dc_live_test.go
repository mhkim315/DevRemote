// C3D-C — live proof against the exact pinned Claude Code 2.1.209 artifact.
//
// Gated by POKIT_CLAUDE_DIGEST (must equal the recorded pinned SHA-256, same
// convention as the accepted C1D live proof). Runs the accepted activation
// note §8 evidence: exactly two live positives (allow once, deny) through the
// FULL production composition — NewAppWithDeps, production install
// transition, real pinned binary, real hooks, authenticated composed-route
// claim. Witness AUTHORITY is untouched production code; the test launcher
// replicates the production spawn byte-for-byte and only TEES stdout into a
// bounded buffer for §8's bounded/redacted corroboration captures.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

const c3dcProbeToken = "pokitclaudeapprovalprobe"
const c3dcPinnedDigest = "59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c"

// ── Tee launcher: production spawn semantics + bounded stdout capture ──

const c3dcCaptureCap = 4 << 20 // 4 MiB per launch

type c3dcCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *c3dcCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c3dcCaptureCap - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil // never backpressure the production pump
}

func (c *c3dcCapture) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]byte, c.buf.Len())
	copy(out, c.buf.Bytes())
	return out
}

// c3dcTeeProc replicates term's production execProcess exactly (direct exec,
// own process group, group TERM/KILL, once-guarded reap, proc-<pid>-<nano>
// opaque ID); Stdout is the production pipe teed into the bounded capture.
type c3dcTeeProc struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.Reader
	started  time.Time
	waitOnce sync.Once
	waitErr  error
}

func (p *c3dcTeeProc) Stdin() io.Writer  { return p.stdin }
func (p *c3dcTeeProc) Stdout() io.Reader { return p.stdout }

func (p *c3dcTeeProc) Term() error {
	if p.cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM); err != nil {
		return p.cmd.Process.Signal(syscall.SIGTERM)
	}
	return nil
}

func (p *c3dcTeeProc) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

func (p *c3dcTeeProc) Wait() error {
	p.waitOnce.Do(func() { p.waitErr = p.cmd.Wait() })
	return p.waitErr
}

func (p *c3dcTeeProc) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *c3dcTeeProc) OpaqueID() string {
	pid := 0
	if p.cmd.Process != nil {
		pid = p.cmd.Process.Pid
	}
	return "proc-" + itoa(pid) + "-" + itoa64(p.started.UnixNano())
}

func itoa(n int) string { return itoa64(int64(n)) }
func itoa64(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

type c3dcLaunch struct {
	argv []string
	cap  *c3dcCapture
	pid  int
}

type c3dcTeeLauncher struct {
	mu       sync.Mutex
	launches []*c3dcLaunch
}

func (l *c3dcTeeLauncher) Launch(exe string, argv []string) (term.ManagedProcess, error) {
	return l.LaunchInDir(exe, argv, "")
}

// LaunchInDir mirrors term's execLauncherWithDir.LaunchInDir: direct exec of
// exe with the exact argv, cwd, own process group, stdio pipes. The single
// addition is the stdout tee into the bounded capture.
func (l *c3dcTeeLauncher) LaunchInDir(exe string, argv []string, cwd string) (term.ManagedProcess, error) {
	cmd := exec.Command(exe, argv...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	capture := &c3dcCapture{}
	p := &c3dcTeeProc{
		cmd: cmd, stdin: stdin,
		stdout:  io.TeeReader(stdout, capture),
		started: time.Now(),
	}
	l.mu.Lock()
	l.launches = append(l.launches, &c3dcLaunch{
		argv: append([]string(nil), argv...), cap: capture, pid: p.PID(),
	})
	l.mu.Unlock()
	return p, nil
}

func (l *c3dcTeeLauncher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.launches)
}

func (l *c3dcTeeLauncher) slice(from int) []*c3dcLaunch {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*c3dcLaunch(nil), l.launches[from:]...)
}


// ── Redacted structural projection (committed-safe; no raw provider text) ──

type c3dcLineProj struct {
	Seq                int    `json:"seq"`
	Type               string `json:"type"`
	Subtype            string `json:"subtype,omitempty"`
	ToolUse            string `json:"toolUse,omitempty"`            // pseudonym tu-N
	ToolUseInputProbe  bool   `json:"toolUseInputProbe,omitempty"`  // input carries the probe command
	ToolResult         string `json:"toolResult,omitempty"`         // pseudonym of the answered tool_use
	ToolResultHasToken bool   `json:"toolResultHasToken,omitempty"` // execution output carries the token
	Deferred           bool   `json:"deferred,omitempty"`
	PermissionDenials  bool   `json:"permissionDenials,omitempty"`
}

type c3dcRunProjection struct {
	Schema              string         `json:"schema"`
	Run                 string         `json:"run"`
	Launch              string         `json:"launch"` // "initial" | "resume"
	PinnedVersion       string         `json:"pinnedVersion"`
	PinnedSHA256        string         `json:"pinnedSha256"`
	Lines               []c3dcLineProj `json:"lines"`
	ToolResultTokenHits int            `json:"toolResultTokenHits"`
	CaptureBytes        int            `json:"captureBytes"`
}

var c3dcKnownTypes = map[string]bool{
	"system": true, "assistant": true, "user": true, "result": true,
	"stream_event": true, "control_request": true, "control_response": true,
}

// c3dcProject walks one captured stream and emits ONLY structural facts:
// closed type/subtype vocabulary, pseudonymized tool_use ids, and booleans.
// Raw text, commands, paths, ids and tokens never enter the projection.
func c3dcProject(run, launch string, capture []byte, pseudo map[string]string) c3dcRunProjection {
	proj := c3dcRunProjection{
		Schema: "pokit.c3dc.live.v1", Run: run, Launch: launch,
		PinnedVersion: "2.1.209", PinnedSHA256: c3dcPinnedDigest,
		CaptureBytes: len(capture),
	}
	alias := func(id string) string {
		if id == "" {
			return ""
		}
		if p, ok := pseudo[id]; ok {
			return p
		}
		p := "tu-" + itoa(len(pseudo)+1)
		pseudo[id] = p
		return p
	}
	seq := 0
	for _, raw := range bytes.Split(capture, []byte("\n")) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		seq++
		var line struct {
			Type       string `json:"type"`
			Subtype    string `json:"subtype"`
			StopReason string `json:"stop_reason"`
			Message    struct {
				Content []struct {
					Type      string          `json:"type"`
					ID        string          `json:"id"`
					ToolUseID string          `json:"tool_use_id"`
					Input     json.RawMessage `json:"input"`
					Content   json.RawMessage `json:"content"`
				} `json:"content"`
			} `json:"message"`
			DeferredToolUse   json.RawMessage `json:"deferred_tool_use"`
			PermissionDenials json.RawMessage `json:"permission_denials"`
		}
		p := c3dcLineProj{Seq: seq, Type: "other"}
		if err := json.Unmarshal(raw, &line); err == nil {
			if c3dcKnownTypes[line.Type] {
				p.Type = line.Type
			}
			if line.Subtype != "" && len(line.Subtype) <= 32 {
				p.Subtype = line.Subtype
			}
			if line.StopReason == "tool_deferred" || len(line.DeferredToolUse) > 0 {
				p.Deferred = true
			}
			if len(line.PermissionDenials) > 0 && string(line.PermissionDenials) != "null" {
				p.PermissionDenials = true
			}
			for _, c := range line.Message.Content {
				switch c.Type {
				case "tool_use":
					p.ToolUse = alias(c.ID)
					p.ToolUseInputProbe = strings.Contains(string(c.Input), c3dcProbeToken)
				case "tool_result":
					p.ToolResult = alias(c.ToolUseID)
					if strings.Contains(string(c.Content), c3dcProbeToken) {
						p.ToolResultHasToken = true
						proj.ToolResultTokenHits++
					}
				}
			}
		}
		proj.Lines = append(proj.Lines, p)
	}
	return proj
}

// ── The live proof ──

func TestC3DC_LiveAllowDenyProof(t *testing.T) {
	digest := os.Getenv("POKIT_CLAUDE_DIGEST")
	if digest == "" {
		t.Skip("POKIT_CLAUDE_DIGEST not set")
	}
	if digest != c3dcPinnedDigest {
		t.Fatalf("digest mismatch: refusing to run against a drifted artifact")
	}
	evidenceDir := os.Getenv("POKIT_C3DC_EVIDENCE_DIR")

	// Recorded artifact identity: exact pinned path, realpath, digest.
	cfg := term.PinnedClaudeConfigWithDigest(digest)
	cfg.Bin = cfg.PinnedPath
	if _, err := os.Stat(cfg.PinnedPath); err != nil {
		t.Fatalf("pinned artifact missing: %v", err)
	}

	launcher := &c3dcTeeLauncher{}
	svc := term.NewManagedClaudeService(cfg, launcher, term.NewClaudeAttestor(cfg))
	fx := newRemoteFixtureWith(t, nil, nil, func(c *Config, deps *Dependencies) {
		c.EnableManagedClaude = true
		deps.ManagedClaude = svc
	})
	// The production install transition must have succeeded at App
	// construction — the live sessions are actionable-capable from birth.
	if !fx.app.managedClaude.ApprovalExecutionInstalled() {
		t.Fatal("production install transition did not succeed")
	}
	store := fx.app.handlers.Approvals
	ownerPriv, ownerID := fx.pairDevice(t, "owner")
	tok := fx.token(t, ownerID, ownerPriv)

	pseudo := map[string]string{}
	var projections []c3dcRunProjection

	runOne := func(run, action, wantState string, wantTokenHits int) {
		t.Logf("── live %s ──", run)
		launchFloor := launcher.count()

		cwd, err := os.MkdirTemp("/tmp", "c3dc-"+run)
		if err != nil {
			t.Fatalf("mkdtemp: %v", err)
		}
		defer os.RemoveAll(cwd)

		sid, err := svc.CreateDetached(cwd)
		if err != nil {
			t.Fatalf("%s CreateDetached: %v", run, err)
		}
		rec, ok := svc.Registry().Get(sid)
		if !ok {
			t.Fatalf("%s: session not in registry", run)
		}
		if rec.CertifiedDigest != digest || rec.Version != "2.1.209" {
			t.Fatalf("%s launch certification: digest/version mismatch in record", run)
		}

		// Bounded poll: exactly ONE pending ACTIONABLE catalog record.
		deadline := time.Now().Add(100 * time.Second)
		var dto term.SafeApprovalDTO
		found := false
		for time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			safe := store.ListSafe(sid)
			if len(safe) == 1 && safe[0].Actionable && safe[0].State == "pending" {
				dto = safe[0]
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s: timed out waiting for the pending actionable catalog record (ListSafe=%v)", run, store.ListSafe(sid))
		}
		if dto.Summary != "Run Claude approval verification probe" {
			t.Fatalf("%s: summary = %q", run, dto.Summary)
		}
		if len(dto.Options) != 2 {
			t.Fatalf("%s: options = %d, want 2", run, len(dto.Options))
		}
		t.Logf("%s: actionable catalog record observed (options=%d)", run, len(dto.Options))

		// The accepted claim window is the JOINED-DEFERRED EXIT (activation
		// note §6): wait for the initial process to finish exiting before
		// claiming, exactly like a human mobile tap would. Resuming the
		// same Claude session while the initial process still holds it
		// fails the delivery closed (observed live as a non-witnessed
		// terminal → 409 conflict) — safe, but not the §8 happy path.
		svc.WaitExited(sid)
		t.Logf("%s: initial process exited — joined-deferred claim window open", run)

		// Authenticated claim through the composed production route.
		code, body := fx.doJSON(t, "POST",
			"/api/sessions/"+sid+"/approvals/"+dto.ID, tok,
			`{"action":"`+action+`","idempotencyKey":"c3dc.live.`+run+`"}`)
		if code != 200 {
			// Full diagnostic dump.
			if snap, ok := store.LookupRecord(sid, dto.ID); ok {
				t.Logf("%s: record state=%s actionable=%v", run, snap.State, snap.Actionable)
			}
			coord := fx.app.managedClaude.Coordinator()
			t.Logf("%s: coordinator identities=%d entries=%d pending=%d",
				run, coord.IdentityCount(), coord.EntryCount(), coord.PendingCount())
			// Dump coordinator identity IDs for cross-reference.
			// The coordinator's internal maps aren't exported, but
			// we can check HasIdentityForRuntime as a proxy.
			t.Logf("%s: HasIdentityForRuntime(sid)=%v",
				run, coord.HasIdentityForRuntime(sid, 1))
			// Dump structural projections AND the raw capture to the
			// evidence dir so the stream format can be debugged.
			for i, ln := range launcher.slice(launchFloor) {
				diag := c3dcProject(run, "diag-"+itoa(i), ln.cap.bytes(), pseudo)
				t.Logf("%s diag launch %d: lines=%d tokenHits=%d", run, i, len(diag.Lines), diag.ToolResultTokenHits)
				for _, lp := range diag.Lines {
					t.Logf("%s diag launch %d line: seq=%d type=%s subtype=%s deferred=%v denials=%v", run, i, lp.Seq, lp.Type, lp.Subtype, lp.Deferred, lp.PermissionDenials)
				}
				if evidenceDir != "" {
					projPath := filepath.Join(evidenceDir, "c3dc_"+run+"_launch"+itoa(i)+"_proj_diag.json")
					blob, _ := json.MarshalIndent(diag, "", "  ")
					os.WriteFile(projPath, blob, 0644)
				}
			}
			t.Fatalf("%s claim: code=%d body=%s", run, code, body)
		}
		snap, ok := store.LookupRecord(sid, dto.ID)
		if !ok || string(snap.State) != wantState {
			t.Fatalf("%s: committed state=%v ok=%v, want %s", run, snap.State, ok, wantState)
		}
		t.Logf("%s: composed route 200, committed state=%s", run, wantState)

		// Clean exit: the production waiter, then Stop for teardown parity.
		svc.WaitExited(sid)
		if err := svc.Stop(sid, rec.Epoch); err != nil {
			t.Logf("%s: Stop after exit: %v (tolerated when already exited)", run, err)
		}

		// Corroboration (non-authority): probe token in EXECUTION output.
		// The delivery's natural-exit drain (5s bounded wait for <-rt.exited
		// after witness commit, claude_approval_delivery.go) ensures the
		// resume process stdout is fully drained through the tee reader
		// before Deliver returns and the claim POST completes.
		launches := launcher.slice(launchFloor)
		if len(launches) != 2 {
			t.Fatalf("%s: expected exactly initial+resume launches, got %d", run, len(launches))
		}
		if !argvHas(launches[1].argv, "--resume") {
			t.Fatalf("%s: second launch is not the resume spawn", run)
		}
		tokenHits := 0
		for i, ln := range launches {
			label := "initial"
			if i == 1 {
				label = "resume"
			}
			proj := c3dcProject(run, label, ln.cap.bytes(), pseudo)
			tokenHits += proj.ToolResultTokenHits
			projections = append(projections, proj)
			// Write structural projection to evidence dir.
			if evidenceDir != "" {
				projPath := filepath.Join(evidenceDir, "c3dc_"+run+"_"+label+"_proj.json")
				blob, _ := json.MarshalIndent(proj, "", "  ")
				os.WriteFile(projPath, blob, 0644)
			}
		}
		if tokenHits != wantTokenHits {
			t.Fatalf("%s: tool_result probe-token hits = %d, want %d (execution corroboration)", run, tokenHits, wantTokenHits)
		}
		t.Logf("%s: execution corroboration tool_result token hits = %d", run, tokenHits)

		// No pending/executing residue for this session.
		for _, s := range store.ListSafe(sid) {
			if s.State == "pending" || s.State == "executing" {
				t.Fatalf("%s: live record left after commit: %s state=%s", run, s.ID, s.State)
			}
		}
	}

	runOne("allow", "allow_once", "approved", 1)
	runOne("deny", "deny", "rejected", 0)

	// Global teardown: shutdown, then every spawned group must be reaped.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	for _, ln := range launcher.slice(0) {
		if ln.pid <= 0 {
			continue
		}
		if err := syscall.Kill(-ln.pid, syscall.Signal(0)); err != syscall.ESRCH {
			t.Errorf("process group %d not reaped: err=%v (want ESRCH)", ln.pid, err)
		}
	}

	// Projections already written to evidence dir above (one set per
	// run, always written when POKIT_C3DC_EVIDENCE_DIR is set).
}

func argvHas(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}
