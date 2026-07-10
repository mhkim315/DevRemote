package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/watcher"
)

// Config holds immutable daemon configuration parsed from CLI flags.
type Config struct {
	OwnerUUID            string
	SupabaseProjectRef   string
	InsecureLocalOnly    bool
	EnableLocalPTY       bool // Phase 6: default-off feature flag
	EnableAgentDetection bool // Phase A5: default-off agent detection bridge
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
	Verifier     term.TokenVerifier // if nil, created from Config in NewAppWithDeps
	Events       term.EventStore    // if nil, NewMemoryEventStore used
	Links        term.LinkStore     // if nil, NewFileLinkStore used
	Cmds         term.CommandBroker // if nil, NewCommandBroker used
	StartWatcher func() (watcherResource, error)
	StartIPC     func(path string, reg *mux.Registry, events term.EventStore, telemetry *term.TelemetryService) (ipcResource, error)
	StartTunnel  func() tunnelResource
}

// ── tunnelProc: production tunnelResource ──

type tunnelProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (t *tunnelProc) Done() <-chan struct{}      { return t.done }
func (t *tunnelProc) Signal(sig os.Signal) error { return t.cmd.Process.Signal(sig) }

// ── App ──

// App owns all runtime dependencies and background resources.
// Every resource's lifecycle is explicit — no fire-and-forget goroutines.
type App struct {
	config Config
	deps   Dependencies

	registry *mux.Registry
	server   *http.Server
	events   term.EventStore // agent event storage
	links    term.LinkStore  // session link storage

	// IPC path is owned by App so Shutdown can clean it up.
	ipcPath string

	// Background resources owned by App for lifecycle control.
	telemetry          *term.TelemetryService // telemetry sampling (owns state machine)
	telemetryCtxCancel context.CancelFunc     // cancels telemetry context
	activity           *term.ActivityBuffer   // E10b: IPC replay
	lifecycle          *term.LifecycleService // M2: Stop/Kill/Delete
	hostIdentity       *devicetrust.HostIdentity
	deviceRegistry     *devicetrust.DeviceRegistry
	authHandler        *devicetrust.AuthHandler               // M2.5-3
	sessionMgr         *devicetrust.DeviceSessionManager      // M2.5-3
	wsTickets          *devicetrust.WSTicketStore             // M2.5-4
	connRegistry       *devicetrust.AuthenticatedConnRegistry // M2.5-4
	handlers           *term.Handlers                         // set after construction for late wiring
	ipc                ipcResource
	watcher            watcherResource
	tunnel             tunnelResource // nil in insecure mode
}

// NewApp creates the App with production defaults.
func NewApp(cfg Config) (*App, error) {
	return NewAppWithDeps(cfg, Dependencies{})
}

// NewAppWithDeps creates an App with injectable dependencies for testing.
func NewAppWithDeps(cfg Config, deps Dependencies) (*App, error) {
	// 1. Registry — instance-owned, no package global.
	reg, err := mux.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	cmuxAdapter, err := mux.NewCmuxAdapter(reg)
	if err != nil {
		return nil, fmt.Errorf("cmux adapter: %w", err)
	}
	if err := reg.Register(cmuxAdapter); err != nil {
		return nil, fmt.Errorf("register cmux: %w", err)
	}
	if err := reg.Register(mux.NewTmuxAdapter()); err != nil {
		return nil, fmt.Errorf("register tmux: %w", err)
	}
	if cfg.EnableLocalPTY {
		if err := reg.Register(mux.NewLocalPTYAdapter()); err != nil {
			return nil, fmt.Errorf("register localpty: %w", err)
		}
	}
	// E9: Controlled PTY Runtime — first-class Control Adapter, always available.
	if err := reg.Register(mux.NewControlledPTYAdapter()); err != nil {
		return nil, fmt.Errorf("register controlled_pty: %w", err)
	}

	events := deps.Events
	if events == nil {
		events = term.NewMemoryEventStore()
	}
	cmds := deps.Cmds
	if cmds == nil {
		cmds = term.NewCommandBroker()
	}
	links := deps.Links
	if links == nil {
		var linkErr error
		links, linkErr = term.NewFileLinkStore()
		if linkErr != nil {
			log.Printf("Failed to create link store: %v (links disabled)", linkErr)
			links = term.NewNopLinkStore()
		}
	}
	if err := term.LoadLinks(reg, links); err != nil {
		log.Printf("Failed to load session links: %v", err)
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
	activity := term.NewActivityBuffer(2000)
	lifecycle := term.NewLifecycleService(reg, activity)

	// M2.5-3: device challenge auth. Feature-gated: if no device registry is
	// configured yet (first run without pairing) the endpoints return an error.
	bootID, err := devicetrust.NewBootID()
	if err != nil {
		return nil, fmt.Errorf("boot id: %w", err)
	}
	sessionMgr := devicetrust.NewDeviceSessionManager(bootID, 20*time.Minute)
	wsTickets := devicetrust.NewWSTicketStore()
	connRegistry := devicetrust.NewAuthenticatedConnRegistry()
	// Wire session replacement → connection invalidation.
	cb := func(deviceID string) {
		connRegistry.CloseDevice(deviceID)
		wsTickets.RevokeForDevice(deviceID)
	}
	sessionMgr.SetOnReplace(cb)
	sessionMgr.SetOnRevoke(cb)
	challengeStore := devicetrust.NewChallengeStore()

	h := &term.Handlers{Registry: reg, Verifier: verifier, Events: events, Links: links, Cmds: cmds, Approvals: approvals, InsecureLocalOnly: cfg.InsecureLocalOnly, Activity: activity, Lifecycle: lifecycle,
		WSTickets: wsTickets, ConnRegistry: connRegistry, SessionMgr: sessionMgr, HostIdentity: nil}

	serveMux := http.NewServeMux()
	// M2.5-4: explicit auth mode. In remote (production) mode, operational
	// REST routes use device bearer auth with permission enforcement. In
	// insecure-local-only mode, the legacy AuthMiddleware (Supabase/dev-token)
	// is used. One listener, one credential type — no opportunistic mixing.
	if cfg.InsecureLocalOnly {
		serveMux.HandleFunc("/api/sessions", h.AuthMiddleware(h.HandleSessionsAPI))
		serveMux.HandleFunc("POST /api/sessions/{id}/stop", h.AuthMiddleware(h.HandleSessionStop))
		serveMux.HandleFunc("POST /api/sessions/{id}/kill", h.AuthMiddleware(h.HandleSessionKill))
		serveMux.HandleFunc("DELETE /api/sessions/{id}", h.AuthMiddleware(h.HandleSessionDelete))
	} else {
		// Remote device-auth mode: fail closed if no session manager.
		if sessionMgr == nil {
			log.Fatalf("device session manager required in remote mode")
		}
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
	}

	// M2.5-3: device challenge-auth endpoints (ungated — fail gracefully
	// when no host identity / device registry is configured).
	authH := &devicetrust.AuthHandler{
		Challenges:  challengeStore,
		Sessions:    sessionMgr,
		RateLimiter: devicetrust.NewChallengeRateLimiter(devicetrust.RateLimiterConfig{}),
	}
	// Identity and Registry are nil here and wired in Run() after initDeviceTrust.
	// Endpoints check for nil and return 503 if not configured.
	serveMux.HandleFunc("POST /api/device-auth/challenge", authH.HandleChallenge)
	serveMux.HandleFunc("POST /api/device-auth/verify", authH.HandleVerify)
	// WS ticket: authenticated endpoint (bearer token required).
	serveMux.HandleFunc("POST /api/device-auth/ws-ticket",
		devicetrust.RequirePrincipal(sessionMgr, devicetrust.HandleWSTicket(wsTickets), devicetrust.PermSessionsRead))
	serveMux.HandleFunc("GET /api/session-profiles", h.AuthMiddleware(term.HandleSessionProfiles))
	serveMux.HandleFunc("POST /api/sessions/{id}/approvals/{approvalId}", h.AuthMiddleware(h.HandleApprovalAction))
	// M2: managed-session lifecycle (registered above in mode-dependent block)
	serveMux.HandleFunc("/api/v2/links", h.AuthMiddleware(h.HandleLinksAPI))
	if cfg.InsecureLocalOnly {
		serveMux.HandleFunc("/term/ws", h.AuthMiddleware(h.HandleWS))
	} else {
		// Remote mode: WS ticket is the only credential.
		serveMux.HandleFunc("GET /term/ws", h.HandleWSTicketAuth)
	}
	serveMux.HandleFunc("GET /term/size", h.AuthMiddleware(term.HandleTermSize))
	serveMux.HandleFunc("/term/", h.AuthMiddleware(h.HandleHTML))

	notifier := newPushNotifier()
	serveMux.HandleFunc("/push/register", h.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token != "" {
			notifier.SetToken(token)
			log.Printf("📱 Push token registered: %s", token)
		}
		w.WriteHeader(200)
	}))

	// 3. Telemetry service owns the state machine and approval detection.
	var agentDetector term.AgentDetector
	if cfg.EnableAgentDetection {
		agentDetector = agent.NewTermAgentDetector()
	}
	telemetry := term.NewTelemetryService(reg, events, links, notifier, agentDetector, approvals, activity)
	h.Telemetry = telemetry

	serveMux.HandleFunc("/debug/dump", h.AuthMiddleware(term.HandleDump))
	serveMux.HandleFunc("/debug/cmd", h.AuthMiddleware(h.HandleCmd))
	serveMux.HandleFunc("/debug/diag", h.AuthMiddleware(h.HandleDiagnostic))
	serveMux.HandleFunc("/debug/e8diag", h.AuthMiddleware(term.HandleE8Diag))

	addr := ":9171"
	if cfg.InsecureLocalOnly {
		addr = "127.0.0.1:9171"
	}

	return &App{
		config:       cfg,
		deps:         deps,
		registry:     reg,
		server:       &http.Server{Addr: addr, Handler: serveMux},
		events:       events,
		links:        links,
		telemetry:    telemetry,
		activity:     activity,
		lifecycle:    lifecycle,
		authHandler:  authH,
		sessionMgr:   sessionMgr,
		wsTickets:    wsTickets,
		connRegistry: connRegistry,
		handlers:     h,
		ipcPath:      "/tmp/pokit.sock",
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

	log.Printf("POKIT daemon %s (owner=%s)", a.server.Addr, a.config.OwnerUUID)

	// 4. Serve HTTP in background; wait for shutdown signal or HTTP error.
	errCh := make(chan error, 1)
	go func() {
		errCh <- a.server.ListenAndServe()
	}()

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

// Shutdown stops all resources in order: HTTP → telemetry → watcher → tunnel → IPC.
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

	// 5. Close IPC and remove socket.
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
	return startWatcherProd(a.events)
}

func (a *App) startIPC() (ipcResource, error) {
	if a.deps.StartIPC != nil {
		return a.deps.StartIPC(a.ipcPath, a.registry, a.events, a.telemetry)
	}
	return term.StartIPCServer(a.ipcPath, a.registry, a.events, a.links, a.telemetry, a.activity, a.lifecycle)
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
	mu    sync.RWMutex
	token string
	send  pushSender // injectable for tests
}

func newPushNotifier() *pushNotifier {
	return &pushNotifier{send: sendPushNotification}
}

func (n *pushNotifier) SetToken(token string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.token = token
}

func (n *pushNotifier) ApprovalRequired(_ context.Context, sessionID string, _ string) error {
	n.mu.RLock()
	token := n.token
	n.mu.RUnlock()
	// P1b: redacted push message — no raw screen content.
	summary := "Interaction required"
	log.Printf("PUSH: approval for session=%s", sessionID)
	if token != "" && n.send != nil {
		go n.send(token, summary, sessionID)
	}
	return nil
}

// ── Production implementations ──

func startWatcherProd(events term.EventStore) *watcher.Tailer {
	homeDir, _ := os.UserHomeDir()
	claudeLogDir := filepath.Join(homeDir, ".claude")
	if _, err := os.Stat(claudeLogDir); os.IsNotExist(err) {
		claudeLogDir = "."
	}
	t, err := watcher.New(claudeLogDir, func(ev watcher.RawEvent) {
		toolUse := watcher.ExtractToolUse(ev)
		if toolUse != nil && (toolUse.Name == "Replace" || toolUse.Name == "Edit" || toolUse.Name == "Write" || toolUse.Name == "StrReplace" || toolUse.Name == "GlobReplace" || toolUse.Name == "View" || toolUse.Name == "Bash") {
			file := "file"
			if f, ok := toolUse.Input["file_path"].(string); ok {
				file = f
			} else if f, ok := toolUse.Input["path"].(string); ok {
				file = f
			} else if f, ok := toolUse.Input["command"].(string); ok {
				file = f
			}
			session := ev.SessionID
			if session == "" {
				session = "devremote"
			}
			events.Emit(session, "file_edit", toolUse.Name, file)
		}
	})
	if err == nil {
		t.Start()
	}
	return t
}

func startTunnelProd() *tunnelProc {
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
