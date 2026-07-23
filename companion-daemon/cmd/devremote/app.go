package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"devremote/companion-daemon/internal/cockpit"
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/projection"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/timeline/writer"
	"devremote/companion-daemon/internal/transcript"
	"devremote/companion-daemon/internal/validation"
	"devremote/companion-daemon/internal/watcher"
	"devremote/companion-daemon/internal/workspace"
)

// Config holds immutable daemon configuration parsed from CLI flags.
type Config struct {
	OwnerUUID                   string
	SupabaseProjectRef          string
	ListenAddr                  string // loopback addr for insecure mode (e.g. 127.0.0.1:0); ignored when InsecureLocalOnly=false
	InsecureLocalOnly           bool
	EnableAgentDetection        bool   // Phase A5: default-off agent detection bridge
	EnableManagedCodex          bool   // SP0: default-off native managed Codex runtime
	EnableManagedClaude         bool   // C1D: default-off native managed Claude runtime
	ClaudeDigest                string // C1D: pre-verified SHA-256 of the pinned Claude binary
	EnableTimelineShadow        bool   // STEP4: default-off, fail-open Timeline shadow sink
	TimelineShadowPath          string // optional absolute shadow-file override
	EnableWorkspaceLease        bool   // STEP5: default-off cooperative workspace contract
	EnableFrozenValidation      bool   // STEP7: default-off frozen validation contract
	EnableCockpit               bool   // STEP8: default-off read-only cockpit route
	EnableProjectionConvergence bool   // STEP9.2: default-off read-only Timeline projection convergence
	EnableN1Notifications       bool   // STEP9.3: default-off exact-event locator notifications
}

// insecureLocalListenAddr returns the only listener address permitted for the
// intentionally unauthenticated local-development mode. A caller-supplied
// address must be an IP loopback address; accepting a wildcard or LAN address
// here would expose the insecure daemon beyond the local machine.
func insecureLocalListenAddr(addr string) (string, error) {
	if addr == "" {
		return "127.0.0.1:9171", nil
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid --listen-addr for --insecure-local-only: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("--listen-addr %q must be a loopback address when --insecure-local-only is set", addr)
	}
	return addr, nil
}

// ── Test seam interfaces ──

// ipcResource abstracts *term.IPCServer so tests can inject fakes.
type ipcResource interface {
	Close() error
	Wait(ctx context.Context) error
}

// watcherResource abstracts *watcher.Tailer so tests can inject fakes.
type watcherResource interface {
	Close() error
}

// tunnelResource abstracts a running tunnel process so tests can inject fakes.
type tunnelResource interface {
	Done() <-chan struct{} // closed when the process exits
	Signal(os.Signal) error
}

// Dependencies holds injectable resource factories for testing.
// A nil field means "use the production default".
type Dependencies struct {
	Verifier            term.TokenVerifier // if nil, created from Config in NewAppWithDeps
	Cmds                term.CommandBroker // if nil, NewCommandBroker used
	DeviceSessionConfig *devicetrust.DeviceSessionManagerConfig
	WSTicketConfig      *devicetrust.WSTicketStoreConfig
	Audit               devicetrust.AuditLog // M2.5-5: nil ⇒ NopAuditLog in NewAppWithDeps
	StartWatcher        func() (watcherResource, error)
	StartIPC            func(path string, telemetry *term.TelemetryService, lifecycle *term.LifecycleService) (ipcResource, error)
	StartTunnel         func() tunnelResource
	// Managed injects a pre-built managed Codex service (deterministic-test
	// seam: a fake ManagedLauncher instead of the pinned production spawn).
	// nil ⇒ the production service is constructed when EnableManagedCodex.
	Managed *term.ManagedCodexService
	// ManagedClaude injects a pre-built managed Claude service (test seam).
	// nil ⇒ the production service is constructed when EnableManagedClaude.
	ManagedClaude *term.ManagedClaudeService
	// OpenTimelineShadow constructs the optional fail-open Timeline sink. A
	// failure disables Timeline only; it never prevents daemon construction.
	// auth is the producer authorization store; nil means no producer gating.
	OpenTimelineShadow func(writer.Config, writer.ProducerAuth) (*writer.Writer, error)
}

// ── tunnelProc: production tunnelResource ──

type tunnelProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (t *tunnelProc) Done() <-chan struct{} { return t.done }
func (t *tunnelProc) Signal(sig os.Signal) error {
	if t == nil || t.cmd == nil || t.cmd.Process == nil {
		return os.ErrProcessDone
	}
	return t.cmd.Process.Signal(sig)
}

// ── App ──

// App owns all runtime dependencies and background resources.
// Every resource's lifecycle is explicit — no fire-and-forget goroutines.
type App struct {
	config Config
	deps   Dependencies

	server *http.Server

	// IPC path is owned by App so Shutdown can clean it up.
	ipcPath string

	// Background resources owned by App for lifecycle control.
	telemetry          *term.TelemetryService     // telemetry sampling (owns state machine)
	telemetryCtxCancel context.CancelFunc         // cancels telemetry context
	transcriptSvc      *transcript.Service        // T3: Transcript integration
	lifecycle          *term.LifecycleService     // M2: Stop/Kill/Delete
	managed            *term.ManagedCodexService  // SP0: native managed Codex runtime (nil unless enabled)
	managedClaude      *term.ManagedClaudeService // C1D: native managed Claude runtime (nil unless enabled)
	hostIdentity       *devicetrust.HostIdentity
	deviceRegistry     *devicetrust.DeviceRegistry
	authHandler        *devicetrust.AuthHandler               // M2.5-3
	sessionMgr         *devicetrust.DeviceSessionManager      // M2.5-3
	wsTickets          *devicetrust.WSTicketStore             // M2.5-4
	connRegistry       *devicetrust.AuthenticatedConnRegistry // M2.5-4
	audit              devicetrust.AuditLog                   // M2.5-5: local audit log
	handlers           *term.Handlers                         // set after construction for late wiring
	ipc                ipcResource
	watcher            watcherResource
	tunnel             tunnelResource              // nil in insecure mode
	timelineWriter     *writer.Writer              // nil unless the default-off shadow flag is enabled
	projection         *projection.Projector       // nil unless the default-off convergence flag and writer are enabled
	workspaceLeases    *workspace.Manager          // nil unless the default-off workspace flag is enabled
	validationCheck    *validation.StalenessCheck  // nil unless the default-off validation flag is enabled
	validationStore    *validation.ValidationStore // nil unless the default-off cockpit flag is enabled
	cockpitStore       *cockpit.CockpitStore       // nil unless the default-off cockpit flag is enabled
}

// NewApp creates the App with production defaults.
func NewApp(cfg Config) (*App, error) {
	deps := Dependencies{}
	// M2.5-5: production audit trail lives beside the device-trust store.
	if home, err := os.UserHomeDir(); err == nil {
		deps.Audit = devicetrust.NewFileAuditLog(filepath.Join(home, ".pokit", "audit.jsonl"))
	}
	return NewAppWithDeps(cfg, deps)
}

// NewAppWithDeps creates an App with injectable dependencies for testing.
func NewAppWithDeps(cfg Config, deps Dependencies) (app *App, err error) {
	var timelineWriter *writer.Writer
	var timelineSink term.OperationalEventSink
	var timelineAuth *writer.ProducerStore
	var timelineProjection *projection.Projector
	var workspaceLeases *workspace.Manager
	var validationCheck *validation.StalenessCheck
	// Timeline is constructed before several independent, fallible App
	// components. If any of those components rejects construction, release the
	// optional writer immediately rather than leaking its file descriptor.
	defer func() {
		if err != nil && timelineWriter != nil {
			if cr := timelineWriter.Close(); !cr.WorkerExited {
				log.Printf("Timeline shadow rollback close: workerExited=%v pendingDropped=%d", cr.WorkerExited, cr.PendingDropped)
			}
		}
	}()

	cmds := deps.Cmds
	if cmds == nil {
		cmds = term.NewCommandBroker()
	}
	// Phase A9: approval tracking.
	approvals := term.NewApprovalStore()

	// 2. Handlers carry dependencies as visible struct fields (no context injection).
	verifier := deps.Verifier
	if verifier == nil {
		verifier = term.NewSupabaseVerifier(term.AuthConfig{
			OwnerUUID:          cfg.OwnerUUID,
			SupabaseProjectRef: cfg.SupabaseProjectRef,
			InsecureLocalOnly:  cfg.InsecureLocalOnly,
		})
	}
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	term.SetTranscriptService(transcriptSvc) // T3: wire byte-stream feed into Recorder
	// STEP 9.1: Timeline is an optional, explicitly constructed shadow sink.
	// The composition adapter binds only after a managed runtime emits its
	// post-registration event and revokes after its post-exit event.
	if cfg.EnableTimelineShadow {
		path := cfg.TimelineShadowPath
		if path == "" {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, ".pokit", "timeline-shadow.jsonl")
			}
		}
		openTimeline := deps.OpenTimelineShadow
		if openTimeline == nil {
			openTimeline = writer.Open
		}
		timelineAuth = writer.NewProducerStore()
		w, err := openTimeline(writer.Config{Path: path}, timelineAuth)
		if err != nil {
			log.Printf("WARNING: Timeline shadow disabled (fail-open): %v", err)
		} else {
			timelineWriter = w
		}
	}
	// STEP 9.2 is a read-only, offline/shadow projection. It owns no goroutine
	// and remains nil unless both its flag and the optional writer are enabled.
	if cfg.EnableProjectionConvergence && timelineWriter != nil {
		timelineProjection = projection.NewProjector(timelineWriter)
	}
	// STEP5: this pure cooperative ledger has no filesystem lock, Git command,
	// or callback into authority paths. It is constructed only behind its
	// default-off flag; wiring live workspace producers remains separate work.
	if cfg.EnableWorkspaceLease {
		workspaceLeases = workspace.NewManager(nil)
	}
	if cfg.EnableFrozenValidation {
		validationCheck = &validation.StalenessCheck{}
	}
	// PA2c: the three managed lifecycle owners. OwnedPTYRuntime owns
	// controlled-PTY launch + generation-bound lifecycle (temporary mux spawn
	// seam until PA2d); the LifecycleService is a pure dispatcher with no
	// Registry dependency. Provider owners are wired below once constructed.
	ownedPTY := term.NewOwnedPTYRuntime(term.NewNativePTYLauncher(), transcriptSvc)
	lifecycle := term.NewLifecycleService(ownedPTY, transcriptSvc)

	// SP0: native managed Codex runtime — default-off. The service owns the
	// pinned launcher, the owned-session registry, and every managed child.
	// Identity verification runs fail-closed per create, not at boot.
	var managed *term.ManagedCodexService
	if cfg.EnableManagedCodex {
		if deps.Managed != nil {
			managed = deps.Managed
		} else {
			managed = term.NewManagedCodexService(term.PinnedConfig0x144(), nil)
		}
		// SP1-P1/P2B-R1: configure the ONE canonical approval store. The store
		// is immutable after configuration/installation/first runtime; a
		// service whose store cannot be owned by this composition (a foreign
		// pre-configured store, a pre-existing runtime, a pre-installed
		// activation) is refused — the App is NOT built, so a diverging
		// handler/ingest store pair or an unowned actionable capability can
		// never run.
		if err := managed.SetApprovalStore(approvals); err != nil {
			return nil, fmt.Errorf("managed approval store: %w", err)
		}
	}

	// C1D: native managed Claude runtime — default-off. The service owns the
	// pinned launcher, the owned-session registry, and every managed Claude
	// child. Identity verification runs fail-closed per create, not at boot.
	var managedClaude *term.ManagedClaudeService
	if cfg.EnableManagedClaude {
		if deps.ManagedClaude != nil {
			managedClaude = deps.ManagedClaude
		} else {
			cc := term.PinnedClaudeConfig()
			if cfg.ClaudeDigest != "" {
				cc = term.PinnedClaudeConfigWithDigest(cfg.ClaudeDigest)
			}
			managedClaude = term.NewManagedClaudeService(cc, nil, nil)
		}
		// C1D: configure the ONE canonical approval store as the non-actionable
		// observation sink. Same immutability contract as Codex.
		if err := managedClaude.SetApprovalStore(approvals); err != nil {
			return nil, fmt.Errorf("managed claude approval store: %w", err)
		}
	}

	// Timeline producer authorization and runtime verification are separate
	// capabilities. The writer store starts empty; the verifier reads only the
	// provider-owned registries populated before each post-registration start
	// event. Install the shared adapter before any runtime can be created.
	if timelineWriter != nil && timelineAuth != nil {
		var codexRegistry, claudeRegistry *term.ManagedSessionRegistry
		if managed != nil {
			codexRegistry = managed.Registry()
		}
		if managedClaude != nil {
			claudeRegistry = managedClaude.Registry()
		}
		timelineSink = newTimelineOperationalAdapter(
			timelineAuth,
			newManagedOperationalRuntimeVerifier(codexRegistry, claudeRegistry),
		)
		if managed != nil {
			if err := managed.SetOperationalEventSink(timelineSink); err != nil {
				log.Printf("WARNING: Managed Codex Timeline producer disabled")
			}
		}
		if managedClaude != nil {
			if err := managedClaude.SetOperationalEventSink(timelineSink); err != nil {
				log.Printf("WARNING: Managed Claude Timeline producer disabled")
			}
		}
	}

	// M2.5-3: device challenge auth. Feature-gated: if no device registry is
	// configured yet (first run without pairing) the endpoints return an error.
	bootID, err := devicetrust.NewBootID()
	if err != nil {
		return nil, fmt.Errorf("boot id: %w", err)
	}
	sessionCfg := devicetrust.DeviceSessionManagerConfig{BootID: bootID, Lifetime: 20 * time.Minute}
	if deps.DeviceSessionConfig != nil {
		sessionCfg = *deps.DeviceSessionConfig
		sessionCfg.BootID = bootID
	}
	sessionMgr := devicetrust.NewDeviceSessionManagerWithConfig(sessionCfg)
	ticketCfg := devicetrust.WSTicketStoreConfig{}
	if deps.WSTicketConfig != nil {
		ticketCfg = *deps.WSTicketConfig
	}
	wsTickets := devicetrust.NewWSTicketStoreWithConfig(ticketCfg)
	connRegistry := devicetrust.NewAuthenticatedConnRegistry()
	// M2.5-5: resolve the audit log (nil ⇒ no-op) so every emit point is safe.
	audit := deps.Audit
	if audit == nil {
		audit = devicetrust.NopAuditLog{}
	}
	// Wire session replacement → connection invalidation.
	cb := func(deviceID string) {
		connRegistry.CloseDevice(deviceID)
		wsTickets.RevokeForDevice(deviceID)
	}
	sessionMgr.SetOnReplace(cb)
	sessionMgr.SetOnRevoke(cb)
	challengeStore := devicetrust.NewChallengeStore()

	h := &term.Handlers{Verifier: verifier, Cmds: cmds, Approvals: approvals, InsecureLocalOnly: cfg.InsecureLocalOnly, Transcript: transcriptSvc, Lifecycle: lifecycle,
		WSTickets: wsTickets, ConnRegistry: connRegistry, SessionMgr: sessionMgr, HostIdentity: nil, Audit: audit, Managed: managed, ManagedClaude: managedClaude}
	// A1 R3-C: the default approval delivery boundary is the generation-owned
	// gate. No generic provider delivery channel is proven, so no sink is
	// registered and the gate accepts nothing (returns `unavailable`, writes no
	// bytes); the gate is wired into the real call graph (below, telemetry drives
	// its activation/deactivation) so a future actionable path is linearized
	// against runtime replacement.
	deliveryGate := term.NewRuntimeDeliveryGate()
	gateDelivery := term.NewGatedApprovalDelivery(deliveryGate)
	h.ApprovalDelivery = gateDelivery
	// SP1-P2B: the ONE production-owned activation transition for the managed
	// Codex runtime. On success the certified delivery boundary and the
	// current-runtime resolver are wired together with actionable ingestion
	// (which the install enabled atomically). P2B-R1: an install failure is
	// tolerated ONLY when the service is provably observation-only (store
	// canonically owned above, activation off) — e.g. a non-certified
	// authority version, whose observation path also rejects everything. If
	// the service reports an installed activation this composition does not
	// own, the App is NOT built.
	var codexDelivery term.ApprovalDelivery
	var codexRuntimeOf func(string) (term.RuntimeRef, bool)
	if managed != nil {
		if d, r, err := managed.InstallApprovalExecution(approvals); err == nil {
			codexDelivery, codexRuntimeOf = d, r
		} else if managed.ApprovalExecutionInstalled() {
			return nil, fmt.Errorf("managed approval execution: unowned activation: %w", err)
		} else {
			log.Printf("Managed approval execution not installed (observation only): %v", err)
		}
	}
	// C3D-A: the ONE production-owned activation transition for the managed
	// Claude runtime, under the same tolerance rule. A failed install leaves
	// the observation store binding in place (configured above) with
	// actionability, Claude delivery and RuntimeOf all off.
	var claudeDelivery term.ApprovalDelivery
	var claudeRuntimeOf func(string) (term.RuntimeRef, bool)
	if managedClaude != nil {
		if d, r, err := managedClaude.InstallApprovalExecution(approvals); err == nil {
			claudeDelivery, claudeRuntimeOf = d, r
		} else if managedClaude.ApprovalExecutionInstalled() {
			return nil, fmt.Errorf("managed claude approval execution: unowned activation: %w", err)
		} else {
			log.Printf("Managed claude approval execution not installed (observation only): %v", err)
		}
	}
	// Composition-boundary dispatch (C3D contract §6): Claude occupies the
	// FALLBACK slot of the accepted Codex dispatcher, so Codex routing is
	// unchanged and unknown adapters still terminate at the capacity-0 gate.
	delivery := term.ApprovalDelivery(gateDelivery)
	if claudeDelivery != nil {
		delivery = term.NewClaudeDispatchingApprovalDelivery(claudeDelivery, gateDelivery)
	}
	if codexDelivery != nil {
		delivery = term.NewDispatchingApprovalDelivery(codexDelivery, delivery)
	}
	h.ApprovalDelivery = delivery
	if codexRuntimeOf != nil || claudeRuntimeOf != nil {
		h.RuntimeOf = term.NewCombinedRuntimeResolver(codexRuntimeOf, claudeRuntimeOf)
	}

	// PA1: construct the read-only managed runtime catalog over the two
	// accepted provider-owned registries. The catalog owns no store,
	// cache, or mutation authority. It is the single read path for
	// managed Get/List/RuntimeOf from this point forward.
	var codexReg *term.ManagedSessionRegistry
	var claudeReg *term.ManagedSessionRegistry
	if managed != nil {
		codexReg = managed.Registry()
	}
	if managedClaude != nil {
		claudeReg = managedClaude.Registry()
	}
	if codexReg != nil || claudeReg != nil {
		var codexAV, claudeAV string
		if managed != nil {
			codexAV = managed.AuthorityVersion()
		}
		if managedClaude != nil {
			claudeAV = managedClaude.AuthorityVersion()
		}
		h.Catalog = term.NewManagedRuntimeCatalog(codexReg, claudeReg, codexRuntimeOf, claudeRuntimeOf, codexAV, claudeAV)
		// Catalog.RuntimeOf replaces the combined resolver for
		// approval-runtime dispatch. The provider-specific RuntimeOf
		// methods and their generation/certification validation
		// remain unchanged.
		if codexRuntimeOf != nil || claudeRuntimeOf != nil {
			h.RuntimeOf = h.Catalog.RuntimeOf
		}
	}

	// PA2c: wire the structured-provider lifecycle owners and the read-only
	// catalog into the lifecycle dispatcher. The frozen services are adapted
	// to the closed typed outcome vocabulary via NewManagedProviderOwner
	// (classification from the provider-owned registry record — no error
	// strings). Explicit nil checks keep a disabled provider a TRUE nil
	// interface (never a typed-nil), so the dispatcher fails closed.
	var codexOwner, claudeOwner term.ProviderLifecycleOwner
	if managed != nil {
		codexOwner = term.NewManagedProviderOwner(managed.Registry(), managed.Stop, managed.Kill, managed.Delete)
	}
	if managedClaude != nil {
		claudeOwner = term.NewManagedProviderOwner(managedClaude.Registry(), managedClaude.Stop, managedClaude.Kill, managedClaude.Delete)
	}
	lifecycle.WireManagedOwners(h.Catalog, codexOwner, claudeOwner)

	serveMux := http.NewServeMux()
	var validationStore *validation.ValidationStore
	var cockpitStore *cockpit.CockpitStore
	notifier := newPushNotifier()
	registerPush := func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		deviceID := r.URL.Query().Get("deviceId")
		if token != "" && deviceID != "" {
			notifier.SetToken(deviceID, token)
		} else if token != "" {
			http.Error(w, "deviceId is required", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}

	// Device bootstrap is public by design. Ticket issuance itself always
	// requires an authenticated device bearer.
	authH := &devicetrust.AuthHandler{
		Challenges:  challengeStore,
		Sessions:    sessionMgr,
		RateLimiter: devicetrust.NewChallengeRateLimiter(devicetrust.RateLimiterConfig{}),
		Audit:       audit,
	}
	serveMux.HandleFunc("POST /api/device-auth/challenge", authH.HandleChallenge)
	serveMux.HandleFunc("POST /api/device-auth/verify", authH.HandleVerify)
	serveMux.HandleFunc("POST /api/device-auth/ws-ticket",
		devicetrust.RequirePrincipal(sessionMgr, devicetrust.HandleWSTicket(wsTickets, audit), devicetrust.PermSessionsRead))
	if cfg.EnableCockpit {
		validationStore = validation.NewValidationStore()
		// STEP8: ValidationStore is wired as a cockpit source (cockpit reads
		// via ReadRecent) and exposed on the App struct for future validation
		// producers. No production Submit caller exists yet — Step 7 was
		// contract-only. The store accumulates findings as validation
		// pipelines are added.
		cockpitStore = cockpit.NewCockpitStore(cockpit.Sources{
			Catalog: h.Catalog, Approvals: approvals, Timeline: timelineWriter, Validation: validationStore,
		})
		cockpit.RegisterCockpitHandler(serveMux, cockpitStore, sessionMgr)
		if timelineWriter != nil {
			cockpit.RegisterTimelineStatsHandler(serveMux, timelineWriter, sessionMgr)
		}
	}

	// M2.5-4: explicit auth mode. In remote (production) mode, operational
	// REST routes use device bearer auth with permission enforcement. In
	// insecure-local-only mode, the legacy AuthMiddleware (Supabase/dev-token)
	// is used. One listener, one credential type — no opportunistic mixing.
	if cfg.InsecureLocalOnly {
		serveMux.HandleFunc("/api/sessions", h.AuthMiddleware(h.HandleSessionsAPI))
		serveMux.HandleFunc("POST /api/sessions/{id}/stop", h.AuthMiddleware(h.HandleSessionStop))
		serveMux.HandleFunc("POST /api/sessions/{id}/kill", h.AuthMiddleware(h.HandleSessionKill))
		serveMux.HandleFunc("DELETE /api/sessions/{id}", h.AuthMiddleware(h.HandleSessionDelete))
		serveMux.HandleFunc("GET /api/session-profiles", h.AuthMiddleware(term.HandleSessionProfiles))
		serveMux.HandleFunc("POST /api/sessions/{id}/approvals/{approvalId}", h.AuthMiddleware(h.HandleApprovalAction))
		serveMux.HandleFunc("GET /api/sessions/{id}/native-status", h.AuthMiddleware(h.HandleManagedNativeStatus))
		serveMux.HandleFunc("GET /api/managed-sessions", h.AuthMiddleware(h.HandleManagedSessions))
		serveMux.HandleFunc("GET /api/managed-sessions/{id}/events", h.AuthMiddleware(h.HandleManagedSessionEvents))
		serveMux.HandleFunc("POST /api/managed-sessions/{id}/prompt", h.AuthMiddleware(h.HandleManagedSessionPrompt))
		serveMux.HandleFunc("POST /api/managed-sessions/{id}/stop", h.AuthMiddleware(h.HandleManagedSessionStop))
		serveMux.HandleFunc("POST /api/managed-sessions/{id}/kill", h.AuthMiddleware(h.HandleManagedSessionKill))
		serveMux.HandleFunc("DELETE /api/managed-sessions/{id}", h.AuthMiddleware(h.HandleManagedSessionDelete))
		serveMux.HandleFunc("POST /api/managed-claude-sessions/{id}/stop", h.AuthMiddleware(h.HandleManagedClaudeSessionStop))
		serveMux.HandleFunc("POST /api/managed-claude-sessions/{id}/kill", h.AuthMiddleware(h.HandleManagedClaudeSessionKill))
		serveMux.HandleFunc("DELETE /api/managed-claude-sessions/{id}", h.AuthMiddleware(h.HandleManagedClaudeSessionDelete))
		serveMux.HandleFunc("/term/ws", h.AuthMiddleware(h.HandleWS))
		serveMux.HandleFunc("/term/size", h.AuthMiddleware(h.HandleTermSize))
		serveMux.HandleFunc("/term/", h.AuthMiddleware(h.HandleHTML))
		serveMux.HandleFunc("/push/register", h.AuthMiddleware(registerPush))
		serveMux.HandleFunc("/debug/dump", h.AuthMiddleware(term.HandleDump))
		serveMux.HandleFunc("/debug/cmd", h.AuthMiddleware(h.HandleCmd))
		serveMux.HandleFunc("/debug/diag", h.AuthMiddleware(h.HandleDiagnostic))
		serveMux.HandleFunc("/debug/e8diag", h.AuthMiddleware(term.HandleE8Diag))
		// T3 Transcript: session-scoped read API.
		serveMux.HandleFunc("GET /api/sessions/{id}/transcript", h.AuthMiddleware(transcript.HandleTranscript(transcriptSvc)))
		serveMux.HandleFunc("GET /api/sessions/{id}/transcript/stats", h.AuthMiddleware(transcript.HandleTranscriptStats(transcriptSvc)))
	} else {
		// Method-level permissions: GET→read, POST create→create, DELETE→history:delete.
		// PUT (legacy no-op) is disabled in remote mode.
		serveMux.HandleFunc("GET /api/sessions",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleSessionsV2, devicetrust.PermSessionsRead))
		serveMux.HandleFunc("POST /api/sessions",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleSessionCRUD, devicetrust.PermSessionsCreate))
		serveMux.HandleFunc("DELETE /api/sessions",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleSessionCRUD, devicetrust.PermHistoryDelete))
		serveMux.HandleFunc("POST /api/sessions/{id}/stop",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleSessionStop, devicetrust.PermSessionsStop))
		serveMux.HandleFunc("POST /api/sessions/{id}/kill",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleSessionKill, devicetrust.PermSessionsKill))
		// Path-based session delete (M2 canonical form).
		serveMux.HandleFunc("DELETE /api/sessions/{id}",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleSessionDelete, devicetrust.PermHistoryDelete))
		serveMux.HandleFunc("GET /api/session-profiles",
			devicetrust.RequirePrincipal(sessionMgr, term.HandleSessionProfiles, devicetrust.PermSessionsRead))
		serveMux.HandleFunc("POST /api/sessions/{id}/approvals/{approvalId}",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleApprovalAction, devicetrust.PermTerminalInput))
		serveMux.HandleFunc("GET /api/sessions/{id}/native-status",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedNativeStatus, devicetrust.PermSessionsRead))
		serveMux.HandleFunc("GET /api/managed-sessions",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedSessions, devicetrust.PermSessionsRead))
		serveMux.HandleFunc("GET /api/managed-sessions/{id}/events",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedSessionEvents, devicetrust.PermSessionsRead))
		serveMux.HandleFunc("POST /api/managed-sessions/{id}/prompt",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedSessionPrompt, devicetrust.PermTerminalInput))
		serveMux.HandleFunc("POST /api/managed-sessions/{id}/stop",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedSessionStop, devicetrust.PermSessionsStop))
		serveMux.HandleFunc("POST /api/managed-sessions/{id}/kill",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedSessionKill, devicetrust.PermSessionsKill))
		serveMux.HandleFunc("DELETE /api/managed-sessions/{id}",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedSessionDelete, devicetrust.PermHistoryDelete))
		serveMux.HandleFunc("POST /api/managed-claude-sessions/{id}/stop",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedClaudeSessionStop, devicetrust.PermSessionsStop))
		serveMux.HandleFunc("POST /api/managed-claude-sessions/{id}/kill",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedClaudeSessionKill, devicetrust.PermSessionsKill))
		serveMux.HandleFunc("DELETE /api/managed-claude-sessions/{id}",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleManagedClaudeSessionDelete, devicetrust.PermHistoryDelete))
		serveMux.HandleFunc("GET /term/ws", h.HandleWSTicketAuth)
		serveMux.HandleFunc("/term/size",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleTermSize, devicetrust.PermSessionsRead))
		serveMux.HandleFunc("/term/",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleHTML, devicetrust.PermSessionsRead))
		serveMux.HandleFunc("/push/register",
			devicetrust.RequirePrincipal(sessionMgr, registerPush, devicetrust.PermSessionsRead))
		// T3 Transcript: session-scoped read API with device-auth.
		serveMux.HandleFunc("GET /api/sessions/{id}/transcript",
			devicetrust.RequirePrincipal(sessionMgr, transcript.HandleTranscript(transcriptSvc), devicetrust.PermSessionsRead))
		serveMux.HandleFunc("GET /api/sessions/{id}/transcript/stats",
			devicetrust.RequirePrincipal(sessionMgr, transcript.HandleTranscriptStats(transcriptSvc), devicetrust.PermSessionsRead))
		// Only product-used diagnostics remain remotely reachable. Raw dump and
		// E8 instrumentation are deliberately local-only.
		serveMux.HandleFunc("/debug/cmd",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleCmd, devicetrust.PermTerminalInput))
		serveMux.HandleFunc("GET /debug/diag",
			devicetrust.RequirePrincipal(sessionMgr, h.HandleDiagnostic, devicetrust.PermSessionsRead))
	}

	// 3. Telemetry service owns the state machine and approval detection.
	telemetry := term.NewTelemetryService(notifier, approvals, transcriptSvc)
	telemetry.SetDeliveryGate(deliveryGate)
	h.Telemetry = telemetry
	// S1: the Delete path clears the agent-activity store (owned by telemetry).
	lifecycle.SetStatusClearer(telemetry)

	addr := ":9171"
	if cfg.InsecureLocalOnly {
		var err error
		addr, err = insecureLocalListenAddr(cfg.ListenAddr)
		if err != nil {
			return nil, err
		}
	}

	return &App{
		config:          cfg,
		deps:            deps,
		server:          &http.Server{Addr: addr, Handler: serveMux},
		telemetry:       telemetry,
		transcriptSvc:   transcriptSvc,
		lifecycle:       lifecycle,
		managed:         managed,
		managedClaude:   managedClaude,
		authHandler:     authH,
		sessionMgr:      sessionMgr,
		wsTickets:       wsTickets,
		connRegistry:    connRegistry,
		audit:           audit,
		handlers:        h,
		ipcPath:         "/tmp/pokit.sock",
		timelineWriter:  timelineWriter,
		projection:      timelineProjection,
		workspaceLeases: workspaceLeases,
		validationCheck: validationCheck,
		validationStore: validationStore,
		cockpitStore:    cockpitStore,
	}, nil
}

// Run starts all background resources and the HTTP server.
// Blocks until ctx is cancelled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	// M2.5-1: ensure the persistent host identity + device registry exist.
	// No auth/pairing yet — this only bootstraps the trust root. The App owns
	// the single instances so later phases have one source of truth.
	a.hostIdentity, a.deviceRegistry = initDeviceTrust()
	if a.hostIdentity != nil && a.deviceRegistry != nil {
		term.SetPairingContext(a.hostIdentity, a.deviceRegistry)
	}
	// M2.5-5: wire the local device-admin surface (list/revoke/audit) so the
	// 0600 socket can revoke a device and read the redacted audit trail.
	term.SetDeviceAdminContext(a.sessionMgr, a.audit)
	// Late wiring: HostIdentity is needed by ticket binding in HandleWS paths.
	if a.handlers != nil {
		a.handlers.HostIdentity = a.hostIdentity
	}
	// Wire the host identity + device registry into the auth handler.
	if a.authHandler != nil {
		a.authHandler.Identity = a.hostIdentity
		a.authHandler.Registry = a.deviceRegistry
	}

	// 3. Start background resources.
	telemetryCtx, cancelTelemetry := context.WithCancel(context.Background())
	a.telemetryCtxCancel = cancelTelemetry
	go a.telemetry.Run(telemetryCtx)

	a.watcher = a.startWatcher()

	// M2.5-3: start periodic session purge.
	if a.sessionMgr != nil {
		a.sessionMgr.StartPurgeLoop()
	}

	ipc, err := a.startIPC()
	if err != nil {
		log.Printf("WARNING: IPC server NOT started — local `pokit run` attach unavailable (%s): %v", a.ipcPath, err)
	} else {
		a.ipc = ipc
	}

	if !a.config.InsecureLocalOnly {
		a.tunnel = a.startTunnel()
	}

	// 4. Serve HTTP in background; wait for shutdown signal or HTTP error.
	var ln net.Listener
	if strings.HasSuffix(a.server.Addr, ":0") {
		// Dynamic port: pre-bind so the actual port is known before logging.
		// Tests parse the log line for the assigned port.
		var err error
		ln, err = net.Listen("tcp", a.server.Addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", a.server.Addr, err)
		}
	}
	addrStr := a.server.Addr
	if ln != nil {
		addrStr = ln.Addr().String()
	}
	log.Printf("POKIT daemon %s (owner=%s)", addrStr, a.config.OwnerUUID)

	errCh := make(chan error, 1)
	if ln != nil {
		go func() { errCh <- a.server.Serve(ln) }()
	} else {
		go func() { errCh <- a.server.ListenAndServe() }()
	}

	var serveErr error
	select {
	case <-ctx.Done():
		log.Println("Shutting down (signal)...")
	case serveErr = <-errCh:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", serveErr)
		}
		log.Println("Shutting down (HTTP ended)...")
	}

	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	shutdownErr := a.Shutdown(shutdownCtx)
	return errors.Join(serveErr, shutdownErr)
}

// Shutdown stops all resources in order: HTTP → telemetry → watcher → tunnel
// → IPC (stop accepting) → managed codex children.
// Resources are cleaned up even if earlier steps fail.
// Errors are joined so the caller can inspect each cause with errors.Is / errors.As.
func (a *App) Shutdown(ctx context.Context) error {
	var errs []error

	// 1. Stop HTTP.
	log.Println("Shutdown: stopping HTTP server...")
	if err := a.server.Shutdown(ctx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
		errs = append(errs, fmt.Errorf("http: %w", err))
	}

	// M2.5-3: stop session purge before telemetry.
	if a.sessionMgr != nil {
		a.sessionMgr.StopPurgeLoop()
	}

	// 2. Stop telemetry.
	if a.telemetry != nil && a.telemetryCtxCancel != nil {
		log.Println("Shutdown: stopping telemetry...")
		a.telemetryCtxCancel()
		select {
		case <-a.telemetry.Done():
			log.Println("Shutdown: telemetry stopped")
		case <-ctx.Done():
			log.Println("Shutdown: telemetry stop deadline exceeded")
			errs = append(errs, fmt.Errorf("telemetry: %w", ctx.Err()))
		}
	}

	// 3. Close watcher.
	if a.watcher != nil {
		log.Println("Shutdown: closing watcher...")
		if err := a.watcher.Close(); err != nil {
			log.Printf("Watcher close error: %v", err)
			errs = append(errs, fmt.Errorf("watcher: %w", err))
		}
	}

	// 4. Signal tunnel and wait for exit. Ignore os.ErrProcessDone
	// (process already exited between the check and the signal).
	if a.tunnel != nil {
		log.Println("Shutdown: signalling tunnel...")
		if err := a.tunnel.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Printf("Tunnel signal error: %v", err)
			errs = append(errs, fmt.Errorf("tunnel signal: %w", err))
		}
		select {
		case <-a.tunnel.Done():
			log.Println("Shutdown: tunnel exited")
		case <-ctx.Done():
			log.Println("Shutdown: tunnel wait deadline exceeded")
			errs = append(errs, fmt.Errorf("tunnel wait: %w", ctx.Err()))
		}
	}

	// 5. Close IPC and remove socket. This stops accepting new local create
	// requests BEFORE the managed runtimes are stopped (step 6); a handler
	// goroutine already past accept fails closed on the service's closing
	// state before it can spawn.
	if a.ipc != nil {
		log.Println("Shutdown: closing IPC server...")
		if err := a.ipc.Close(); err != nil {
			log.Printf("IPC close error: %v", err)
			errs = append(errs, fmt.Errorf("ipc close: %w", err))
		}
		if err := a.ipc.Wait(ctx); err != nil {
			log.Printf("IPC wait error: %v", err)
			errs = append(errs, fmt.Errorf("ipc wait: %w", err))
		}
		if err := os.Remove(a.ipcPath); err != nil && !os.IsNotExist(err) {
			log.Printf("IPC socket remove error: %v", err)
			errs = append(errs, fmt.Errorf("ipc remove: %w", err))
		} else {
			log.Println("Shutdown: IPC socket removed")
		}
	}

	// 6. SP0: stop the managed Codex children (kill + bounded reap) AFTER the
	// IPC listener stopped accepting. Shutdown's closing transition makes any
	// still-in-flight create fail closed and roll back its own child.
	if a.managed != nil {
		log.Println("Shutdown: stopping managed codex runtimes...")
		if err := a.managed.Shutdown(ctx); err != nil {
			log.Printf("Managed codex shutdown error: %v", err)
			errs = append(errs, fmt.Errorf("managed codex: %w", err))
		}
	}

	// 7. C1D: stop the managed Claude children.
	if a.managedClaude != nil {
		log.Println("Shutdown: stopping managed claude runtimes...")
		if err := a.managedClaude.Shutdown(ctx); err != nil {
			log.Printf("Managed claude shutdown error: %v", err)
			errs = append(errs, fmt.Errorf("managed claude: %w", err))
		}
	}

	// 8. Timeline is a best-effort shadow sink. Its close result is logged but
	// never joins shutdown errors, so Timeline unavailability cannot block a
	// primary shutdown path.
	if a.cockpitStore != nil {
		a.cockpitStore.Close()
	}
	if a.timelineWriter != nil {
		a.timelineWriter.Close()
	}

	return errors.Join(errs...)
}

// ── Resource starters (respect Dependencies injection) ──

func (a *App) startWatcher() watcherResource {
	if a.deps.StartWatcher != nil {
		w, err := a.deps.StartWatcher()
		if err != nil {
			log.Printf("Watcher start error: %v", err)
			return nil
		}
		return w
	}
	return startWatcherProd()
}

func (a *App) startIPC() (ipcResource, error) {
	if a.deps.StartIPC != nil {
		return a.deps.StartIPC(a.ipcPath, a.telemetry, a.lifecycle)
	}
	return term.StartIPCServer(a.ipcPath, a.telemetry, a.lifecycle, a.managed, a.managedClaude)
}

func (a *App) startTunnel() tunnelResource {
	if a.deps.StartTunnel != nil {
		return a.deps.StartTunnel()
	}
	return startTunnelProd()
}

// ── runDaemon ──

func runDaemon(cfg Config) {
	app, err := NewApp(cfg)
	if err != nil {
		log.Fatalf("Failed to create app: %v", err)
	}

	if cfg.OwnerUUID == "" {
		log.Println("WARN: --owner-uuid not set. All valid Supabase tokens will be accepted (INSECURE).")
	}
	if cfg.SupabaseProjectRef == "" {
		log.Println("WARN: --supabase-ref not set. RS256 JWKS verification disabled. Falling back to HS256 dev mode.")
	}
	if cfg.InsecureLocalOnly {
		log.Println("WARNING: Running in local-only insecure mode. Bound to 127.0.0.1:9171.")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		log.Fatalf("Daemon error: %v", err)
	}
	log.Println("Daemon stopped.")
}

// ── Notifier ──

type pushSender func(token, message, sessionID string)

type pushNotifier struct {
	mu     sync.RWMutex
	tokens map[string]string // deviceID → token; registrations never overwrite peers
	send   pushSender        // injectable for tests
}

func newPushNotifier() *pushNotifier {
	return &pushNotifier{send: sendPushNotification, tokens: make(map[string]string)}
}

func (n *pushNotifier) SetToken(deviceID, token string) {
	if deviceID == "" || token == "" {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.tokens == nil {
		n.tokens = make(map[string]string)
	}
	n.tokens[deviceID] = token
}

func (n *pushNotifier) ApprovalRequired(_ context.Context, sessionID string, _ string) error {
	n.mu.RLock()
	tokens := make([]string, 0, len(n.tokens))
	for _, token := range n.tokens {
		tokens = append(tokens, token)
	}
	n.mu.RUnlock()
	// P1b: redacted push message — no raw screen content.
	summary := "Interaction required"
	log.Printf("PUSH: approval for session=%s", sessionID)
	if n.send != nil {
		for _, token := range tokens {
			go n.send(token, summary, sessionID)
		}
	}
	return nil
}

// ── Production implementations ──

func startWatcherProd() *watcher.Tailer {
	homeDir, _ := os.UserHomeDir()
	claudeLogDir := filepath.Join(homeDir, ".claude")
	if _, err := os.Stat(claudeLogDir); os.IsNotExist(err) {
		claudeLogDir = "."
	}
	t, err := watcher.New(claudeLogDir, func(ev watcher.RawEvent) {
		toolUse := watcher.ExtractToolUse(ev)
		if toolUse != nil && (toolUse.Name == "Replace" || toolUse.Name == "Edit" || toolUse.Name == "Write" || toolUse.Name == "StrReplace" || toolUse.Name == "GlobReplace" || toolUse.Name == "View" || toolUse.Name == "Bash") {
			// PA3 Step 6b: events.Emit removed; Transcript is canonical.
			// File watcher retains tool-use filter for future canonical writes.
		}
	})
	if err == nil {
		t.Start()
	}
	return t
}

func startTunnelProd() tunnelResource {
	cloudflaredPath := "cloudflared"
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		for i := 0; i < 5; i++ {
			p := filepath.Join(dir, "cloudflared")
			if stat, err := os.Stat(p); err == nil && !stat.IsDir() {
				cloudflaredPath = p
				break
			}
			dir = filepath.Dir(dir)
		}
	}

	cmd := exec.Command(cloudflaredPath, "tunnel", "run", "devremote")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start cloudflared: %v", err)
		return nil
	}

	fmt.Println("\n===========================================")
	fmt.Println("🚀 POKIT Daemon Started")
	fmt.Println("===========================================")

	tp := &tunnelProc{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(tp.done)
	}()
	return tp
}
