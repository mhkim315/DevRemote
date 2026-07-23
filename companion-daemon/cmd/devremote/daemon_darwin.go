//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ── Paths ──

// daemonPlistPath returns the path to the per-user LaunchAgent plist.
func daemonPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", daemonLabel+".plist")
}

// daemonStateDir returns the per-user pokit state directory.
func daemonStateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pokit")
}

// daemonBinPath returns the path to the current executable (the pokit CLI
// itself, which also runs as a daemon via `pokit daemon`).
func daemonBinPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// ── Plist definition ──

type plist struct {
	Label             string   `json:"Label"`
	ProgramArguments  []string `json:"ProgramArguments"`
	RunAtLoad         bool     `json:"RunAtLoad"`
	KeepAlive         bool     `json:"KeepAlive"`
	StandardOutPath   string   `json:"StandardOutPath"`
	StandardErrorPath string   `json:"StandardErrorPath"`
	WorkingDirectory  string   `json:"WorkingDirectory"`
}

// ── daemonState tracks the metadata of an installed daemon for upgrade/
// rollback decisions. It is written atomically alongside the plist.
type daemonState struct {
	Version   string `json:"version"`   // CLI version at install time
	BinPath   string `json:"binPath"`   // path to the pokit binary
	PlistPath string `json:"plistPath"` // path to the installed plist
	StateDir  string `json:"stateDir"`  // path to .pokit state directory
}

// ── ops helpers ──

// runLaunchctl runs launchctl with the given arguments. Errors are returned
// for the caller to interpret; idempotent operations (load when already
// loaded, unload when not loaded) may return non-zero.
func runLaunchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// captureLaunchctl runs launchctl and returns stdout as a string.
func captureLaunchctl(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// ── Install ──

// installDaemon installs a per-user LaunchAgent. It creates the plist,
// state directory, and loads the agent. A failed install removes only the
// plist/state it just created; it never removes a known working prior
// installation.
func installDaemon() {
	binPath := daemonBinPath()
	if binPath == "" {
		log.Fatalf("install: cannot determine executable path")
	}
	stateDir := daemonStateDir()
	plistPath := daemonPlistPath()

	// Check for existing installation.
	if _, err := os.Stat(plistPath); err == nil {
		fmt.Println("LaunchAgent already installed. Use 'pokit daemon start' to start it.")
		os.Exit(0)
	}

	// Create state directory with restrictive permissions.
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		log.Fatalf("install: cannot create state directory %s: %v", stateDir, err)
	}

	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		log.Fatalf("install: cannot create log directory %s: %v", logDir, err)
	}

	// Build the plist.
	p := plist{
		Label:             daemonLabel,
		ProgramArguments:  []string{binPath, "daemon"},
		RunAtLoad:         true,
		KeepAlive:         true,
		StandardOutPath:   filepath.Join(logDir, "daemon-stdout.log"),
		StandardErrorPath: filepath.Join(logDir, "daemon-stderr.log"),
		WorkingDirectory:  stateDir,
	}

	plistBytes, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		log.Fatalf("install: cannot marshal plist: %v", err)
	}

	// Atomically write the plist: create temp, rename.
	tmpPath := plistPath + ".tmp"
	if err := os.WriteFile(tmpPath, plistBytes, 0o600); err != nil {
		log.Fatalf("install: cannot write plist: %v", err)
	}
	if err := os.Rename(tmpPath, plistPath); err != nil {
		os.Remove(tmpPath)
		log.Fatalf("install: cannot rename plist: %v", err)
	}

	fmt.Printf("LaunchAgent installed: %s\n", plistPath)
	fmt.Printf("State directory:    %s\n", stateDir)
	fmt.Printf("Binary:             %s\n", binPath)

	// Load the agent.
	if err := runLaunchctl("bootstrap", "gui/"+currentUserUID(), plistPath); err != nil {
		// Bootstrap failed — remove only the plist we just created.
		fmt.Fprintf(os.Stderr, "install: launchctl bootstrap failed: %v\n", err)
		// Do NOT remove a pre-existing plist (there isn't one — we checked above).
		os.Remove(plistPath)
		fmt.Fprintf(os.Stderr, "Removed newly created plist. Prior installation (if any) is untouched.\n")
		os.Exit(1)
	}

	fmt.Println("LaunchAgent loaded and started.")
}

// ── Start ──

// startDaemon starts the daemon via launchctl. Idempotent.
func startDaemon() {
	plistPath := daemonPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		log.Fatalf("start: LaunchAgent not installed. Run 'pokit daemon install' first.")
	}

	isLoaded, _ := daemonLoaded(plistPath)
	if isLoaded {
		fmt.Println("Daemon is already loaded.")
		return
	}

	if err := runLaunchctl("bootstrap", "gui/"+currentUserUID(), plistPath); err != nil {
		log.Fatalf("start: launchctl bootstrap failed: %v", err)
	}
	fmt.Println("Daemon started.")
}

// ── Stop ──

// stopDaemon stops the daemon via launchctl. Idempotent.
func stopDaemon() {
	plistPath := daemonPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Println("LaunchAgent is not installed. Nothing to stop.")
		return
	}

	isLoaded, _ := daemonLoaded(plistPath)
	if !isLoaded {
		fmt.Println("Daemon is not running.")
		return
	}

	if err := runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath); err != nil {
		// bootout may fail if the service is already stopped; not fatal.
		fmt.Fprintf(os.Stderr, "stop: launchctl bootout: %v\n", err)
	}
	fmt.Println("Daemon stopped.")
}

// ── Status ──

// statusDaemon reports the LaunchAgent load status and basic daemon health.
func statusDaemon() {
	plistPath := daemonPlistPath()

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Println("LaunchAgent: NOT INSTALLED")
		return
	}

	isLoaded, pid := daemonLoaded(plistPath)
	if !isLoaded {
		fmt.Println("LaunchAgent: INSTALLED (not loaded)")
		fmt.Printf("Plist: %s\n", plistPath)
		return
	}

	fmt.Printf("LaunchAgent: LOADED")
	if pid != "" {
		fmt.Printf(" (PID %s)", pid)
	}
	fmt.Println()
	fmt.Printf("Plist: %s\n", plistPath)
	fmt.Printf("State: %s\n", daemonStateDir())
	fmt.Printf("Binary: %s\n", daemonBinPath())
}

// ── Uninstall ──

// uninstallDaemon stops the LaunchAgent, removes the plist, and
// optionally purges device trust state.
func uninstallDaemon(purgeTrust bool) {
	plistPath := daemonPlistPath()

	// Stop the agent if running.
	if isLoaded, _ := daemonLoaded(plistPath); isLoaded {
		if err := runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: launchctl bootout: %v\n", err)
		}
		fmt.Println("Daemon stopped.")
	}

	// Remove the plist.
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		log.Fatalf("uninstall: cannot remove plist: %v", err)
	}
	fmt.Println("LaunchAgent removed.")

	// Purge device trust state if requested.
	if purgeTrust {
		stateDir := daemonStateDir()
		fmt.Printf("Purging device trust state in %s...\n", stateDir)
		// Remove only the trust-related files, not logs or other state.
		for _, f := range []string{"host_identity.json", "devices.json"} {
			path := filepath.Join(stateDir, f)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "uninstall: cannot remove %s: %v\n", path, err)
			}
		}
		fmt.Println("Device trust state purged.")
	} else {
		fmt.Println("Device trust state preserved. Use --purge-trust to remove it.")
	}
}

// ── Helpers ──

// daemonLoaded reports whether the LaunchAgent is loaded and its PID.
func daemonLoaded(plistPath string) (bool, string) {
	out, err := captureLaunchctl("print", "gui/"+currentUserUID()+"/"+daemonLabel)
	if err != nil {
		return false, ""
	}
	// Parse PID from launchctl print output (property list format).
	// Look for "state = running" or similar.
	if strings.Contains(out, "state = running") {
		// Try to extract PID.
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "pid = ") {
				pid := strings.TrimPrefix(line, "pid = ")
				return true, pid
			}
		}
		return true, ""
	}
	return false, ""
}

// currentUserUID returns the current user's UID as a string for launchctl.
func currentUserUID() string {
	return fmt.Sprintf("%d", os.Getuid())
}
