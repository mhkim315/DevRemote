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
	"syscall"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/watcher"
)

// Config holds immutable daemon configuration parsed from CLI flags.
type Config struct {
	OwnerUUID          string
	SupabaseProjectRef string
	InsecureLocalOnly  bool
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
	StartIPC     func(path string, reg *mux.Registry, events term.EventStore) (ipcResource, error)
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
	telemetryDone   <-chan struct{}    // closed when telemetry goroutine exits
	telemetryCancel context.CancelFunc // cancels telemetry context
	ipc             ipcResource
	watcher         watcherResource
	tunnel          tunnelResource // nil in insecure mode
}

// NewApp creates the App with production defaults.
func NewApp(cfg Config) (*App, error) {
	return NewAppWithDeps(cfg, Dependencies{})
}

// NewAppWithDeps creates an App with injectable dependencies for testing.
func NewAppWithDeps(cfg Config, deps Dependencies) (*App, error) {
	// 1. Registry — instance-owned, no package global.
	reg := mux.NewRegistry()
	cmuxAdapter, err := mux.NewCmuxAdapter(reg)
	if err != nil {
		return nil, fmt.Errorf("cmux adapter: %w", err)
	}
	reg.Register(cmuxAdapter)
	reg.Register(mux.NewTmuxAdapter())

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

	// 2. Handlers carry dependencies as visible struct fields (no context injection).
	verifier := deps.Verifier
	if verifier == nil {
		verifier = term.NewSupabaseVerifier(term.AuthConfig{
			OwnerUUID:          cfg.OwnerUUID,
			SupabaseProjectRef: cfg.SupabaseProjectRef,
			InsecureLocalOnly:  cfg.InsecureLocalOnly,
		})
	}
	h := &term.Handlers{Registry: reg, Verifier: verifier, Events: events, Links: links, Cmds: cmds}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/api/sessions", h.AuthMiddleware(h.HandleSessionsAPI))
	serveMux.HandleFunc("/api/v2/links", h.AuthMiddleware(h.HandleLinksAPI))
	serveMux.HandleFunc("/term/ws", h.AuthMiddleware(h.HandleWS))
	serveMux.HandleFunc("/term/", h.AuthMiddleware(h.HandleHTML))

	var pushToken string
	serveMux.HandleFunc("/push/register", h.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token != "" {
			pushToken = token
			log.Printf("📱 Push token registered: %s", token)
		}
		w.WriteHeader(200)
	}))

	// term.OnApproval is a package global (deferred to Phase 5).
	term.OnApproval = func(msg string) {
		log.Printf("🚨 APPROVAL DETECTED: %s", msg)
		if pushToken != "" {
			go sendPushNotification(pushToken, msg)
		}
	}

	serveMux.HandleFunc("/debug/dump", h.AuthMiddleware(term.HandleDump))
	serveMux.HandleFunc("/debug/cmd", h.AuthMiddleware(h.HandleCmd))

	addr := ":9171"
	if cfg.InsecureLocalOnly {
		addr = "127.0.0.1:9171"
	}

	return &App{
		config:   cfg,
		deps:     deps,
		registry: reg,
		server:   &http.Server{Addr: addr, Handler: serveMux},
		events:   events,
		links:    links,
		ipcPath:  "/tmp/pokit.sock",
	}, nil
}

// Run starts all background resources and the HTTP server.
// Blocks until ctx is cancelled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	// 3. Start background resources.
	telemetryCtx, cancelTelemetry := context.WithCancel(context.Background())
	a.telemetryCancel = cancelTelemetry
	a.telemetryDone = term.StartTelemetryLoop(telemetryCtx, a.registry, a.events, a.links)

	a.watcher = a.startWatcher()

	ipc, err := a.startIPC()
	if err != nil {
		log.Printf("Failed to start IPC server: %v", err)
	}
	a.ipc = ipc

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

	// 2. Stop telemetry.
	if a.telemetryCancel != nil {
		log.Println("Shutdown: stopping telemetry...")
		a.telemetryCancel()
		select {
		case <-a.telemetryDone:
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
		return a.deps.StartIPC(a.ipcPath, a.registry, a.events)
	}
	return term.StartIPCServer(a.ipcPath, a.registry, a.events, a.links)
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
