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

	"devremote/companion-daemon/internal/models"
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

// Dependencies holds injectable resource factories for testing.
// Production defaults are used when a field is nil.
type Dependencies struct {
	StartWatcher func() (*watcher.Tailer, error)
	StartIPC     func(path string, reg *mux.Registry) (*term.IPCServer, error)
	StartTunnel  func() *exec.Cmd
}

// App owns all runtime dependencies and background resources.
// Every resource's lifecycle is explicit — no fire-and-forget goroutines.
type App struct {
	config Config
	deps   Dependencies

	registry *mux.Registry
	server   *http.Server

	// IPC path is owned by App so Shutdown can clean it up.
	ipcPath string

	// Background resources owned by App for lifecycle control.
	telemetryDone   <-chan struct{}    // closed when telemetry goroutine exits
	telemetryCancel context.CancelFunc // cancels telemetry context
	ipc             *term.IPCServer
	watcher         *watcher.Tailer
	tunnelCmd       *exec.Cmd // cloudflared process (nil in insecure mode)
}

// NewApp creates the App, registers adapters, wires HTTP routes.
// All runtime dependencies are explicitly assembled here.
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

	if err := term.LoadLinks(reg); err != nil {
		log.Printf("Failed to load session links: %v", err)
	}

	// 2. Handlers carry dependencies as visible struct fields (no context injection).
	h := &term.Handlers{Registry: reg}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/api/sessions", term.AuthMiddleware(h.HandleSessionsAPI))
	serveMux.HandleFunc("/api/v2/links", term.AuthMiddleware(h.HandleLinksAPI))
	serveMux.HandleFunc("/term/ws", term.AuthMiddleware(h.HandleWS))
	serveMux.HandleFunc("/term/", term.AuthMiddleware(h.HandleHTML))

	var pushToken string
	serveMux.HandleFunc("/push/register", term.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token != "" {
			pushToken = token
			log.Printf("📱 Push token registered: %s", token)
		}
		w.WriteHeader(200)
	}))

	// term.OnApproval is a package global (deferred to Phase 5).
	// This means two parallel Apps share the same callback — a known limitation.
	term.OnApproval = func(msg string) {
		log.Printf("🚨 APPROVAL DETECTED: %s", msg)
		if pushToken != "" {
			go sendPushNotification(pushToken, msg)
		}
	}

	serveMux.HandleFunc("/debug/dump", term.AuthMiddleware(term.HandleDump))
	serveMux.HandleFunc("/debug/cmd", term.AuthMiddleware(term.HandleCmd))

	// 3. Address selection.
	addr := ":9171"
	if cfg.InsecureLocalOnly {
		addr = "127.0.0.1:9171"
	}

	return &App{
		config:   cfg,
		deps:     deps,
		registry: reg,
		server:   &http.Server{Addr: addr, Handler: serveMux},
		ipcPath:  "/tmp/pokit.sock",
	}, nil
}

// Run starts all background resources and the HTTP server.
// Blocks until ctx is cancelled, then shuts down gracefully.
// Returns any HTTP serve error (except http.ErrServerClosed) joined with shutdown errors.
func (a *App) Run(ctx context.Context) error {
	// 4. Start background resources.
	telemetryCtx, cancelTelemetry := context.WithCancel(context.Background())
	a.telemetryCancel = cancelTelemetry
	a.telemetryDone = term.StartTelemetryLoop(telemetryCtx, a.registry)

	a.watcher = a.startWatcher()

	ipc, err := a.startIPC()
	if err != nil {
		log.Printf("Failed to start IPC server: %v", err)
	}
	a.ipc = ipc

	if !a.config.InsecureLocalOnly {
		a.tunnelCmd = a.startTunnel()
	}

	log.Printf("POKIT daemon %s (owner=%s)", a.server.Addr, a.config.OwnerUUID)

	// 5. Serve HTTP in background; wait for shutdown signal or HTTP error.
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

	// Normalize: ErrServerClosed is expected shutdown.
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}

	// 6. Graceful shutdown with deadline.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	shutdownErr := a.Shutdown(shutdownCtx)
	return errors.Join(serveErr, shutdownErr)
}

// Shutdown stops all resources in order: HTTP → telemetry → watcher → IPC socket.
// Resources are cleaned up even if earlier steps fail.
// Errors are joined so the caller can inspect each cause with errors.Is / errors.As.
func (a *App) Shutdown(ctx context.Context) error {
	var errs []error

	// 1. Stop accepting new HTTP connections.
	log.Println("Shutdown: stopping HTTP server...")
	if err := a.server.Shutdown(ctx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
		errs = append(errs, fmt.Errorf("http: %w", err))
	}

	// 2. Stop telemetry and wait for goroutine exit.
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

	// 3. Close watcher (filesystem tailing).
	if a.watcher != nil {
		log.Println("Shutdown: closing watcher...")
		if err := a.watcher.Close(); err != nil {
			log.Printf("Watcher close error: %v", err)
			errs = append(errs, fmt.Errorf("watcher: %w", err))
		}
	}

	// 4. Close tunnel process if running.
	if a.tunnelCmd != nil && a.tunnelCmd.Process != nil {
		log.Println("Shutdown: signalling tunnel...")
		if err := a.tunnelCmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("Tunnel signal error: %v", err)
			errs = append(errs, fmt.Errorf("tunnel signal: %w", err))
		}
		// Wait for tunnel to exit with deadline.
		tunnelDone := make(chan error, 1)
		go func() { tunnelDone <- a.tunnelCmd.Wait() }()
		select {
		case <-tunnelDone:
			log.Println("Shutdown: tunnel exited")
		case <-ctx.Done():
			log.Println("Shutdown: tunnel wait deadline exceeded")
			errs = append(errs, fmt.Errorf("tunnel wait: %w", ctx.Err()))
		}
	}

	// 5. Close IPC listener and wait for accept loop exit.
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
	}

	// 6. Remove IPC socket pathname after listener is fully stopped.
	if a.ipc != nil {
		if err := os.Remove(a.ipcPath); err != nil && !os.IsNotExist(err) {
			log.Printf("IPC socket remove error: %v", err)
			errs = append(errs, fmt.Errorf("ipc remove: %w", err))
		} else {
			log.Println("Shutdown: IPC socket removed")
		}
	}

	return errors.Join(errs...)
}

// startWatcher initialises filesystem watching for JSONL agent logs.
func (a *App) startWatcher() *watcher.Tailer {
	if a.deps.StartWatcher != nil {
		t, err := a.deps.StartWatcher()
		if err != nil {
			log.Printf("Watcher start error: %v", err)
			return nil
		}
		return t
	}
	return startWatcherProd()
}

// startIPC creates the Unix socket listener.
func (a *App) startIPC() (*term.IPCServer, error) {
	if a.deps.StartIPC != nil {
		return a.deps.StartIPC(a.ipcPath, a.registry)
	}
	return term.StartIPCServer(a.ipcPath, a.registry)
}

// startTunnel launches cloudflared for remote access.
func (a *App) startTunnel() *exec.Cmd {
	if a.deps.StartTunnel != nil {
		return a.deps.StartTunnel()
	}
	return startTunnelProd()
}

// runDaemon is the entry point called from main().
// It only does config wiring, object assembly, and execution.
func runDaemon(cfg Config) {
	app, err := NewApp(cfg)
	if err != nil {
		log.Fatalf("Failed to create app: %v", err)
	}

	// Auth globals — Phase 3 will encapsulate these inside a verifier.
	term.OwnerUUID = cfg.OwnerUUID
	term.SupabaseProjectRef = cfg.SupabaseProjectRef
	term.InsecureLocalOnly = cfg.InsecureLocalOnly

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

// startWatcherProd is the production watcher implementation.
func startWatcherProd() *watcher.Tailer {
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
			models.EmitEvent(session, "file_edit", toolUse.Name, file)
		}
	})
	if err == nil {
		t.Start()
	}
	return t
}

// startTunnelProd is the production cloudflared tunnel implementation.
// A background goroutine calls cmd.Wait() to reap the child process.
func startTunnelProd() *exec.Cmd {
	cloudflaredPath := "cloudflared" // assume in PATH first

	// Search upwards from executable dir up to 4 levels
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

	// Reap the child process in background so it doesn't become a zombie.
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("cloudflared tunnel exited: %v", err)
		}
	}()

	return cmd
}
