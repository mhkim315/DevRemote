//go:build darwin

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ── Paths ──

func daemonPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", daemonLabel+".plist")
}

func daemonStateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pokit")
}

func daemonBinPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// ── daemonState persisted alongside the plist ──

type daemonState struct {
	Version     string `json:"version"`
	BinPath     string `json:"binPath"`
	PlistPath   string `json:"plistPath"`
	StateDir    string `json:"stateDir"`
	InstalledAt string `json:"installedAt"`
}

func readDaemonState() (*daemonState, error) {
	path := filepath.Join(daemonStateDir(), "daemon_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s daemonState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func writeDaemonState(s *daemonState) error {
	path := filepath.Join(daemonStateDir(), "daemon_state.json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ── XML plist template ──

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
	<key>WorkingDirectory</key>
	<string>%s</string>
</dict>
</plist>`

// ── launchctl helpers ──

func runLaunchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func captureLaunchctl(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func currentUserUID() string {
	return fmt.Sprintf("%d", os.Getuid())
}

// daemonLoaded reports whether the LaunchAgent is registered with launchd.
// Uses "launchctl print gui/UID/LABEL" which works for both running and
// loaded-but-stopped jobs. Returns (loaded, pid).
func daemonLoaded(plistPath string) (bool, string) {
	out, err := captureLaunchctl("print", "gui/"+currentUserUID()+"/"+daemonLabel)
	if err != nil {
		// Also try "launchctl list" as fallback.
		out2, err2 := captureLaunchctl("list", daemonLabel)
		if err2 != nil {
			return false, ""
		}
		out = out2
	}
	if out == "" {
		return false, ""
	}
	// "launchctl list LABEL" prints PID on first line when running, "-" when not.
	if pid := extractPID(out); pid != "" {
		return true, pid
	}
	// "launchctl print" succeeds even for loaded-but-not-running jobs.
	return strings.Contains(out, daemonLabel), ""
}

func extractPID(out string) string {
	lines := strings.Split(out, "\n")
	if len(lines) > 0 {
		fields := strings.Fields(lines[0])
		if len(fields) >= 2 && fields[0] != "-" {
			return fields[0]
		}
	}
	return ""
}

// ── randomSuffix generates an unpredictable 8-char hex suffix.
func randomSuffix() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Install ──

func installDaemon() {
	binPath := daemonBinPath()
	if binPath == "" {
		log.Fatalf("install: cannot determine executable path")
	}
	stateDir := daemonStateDir()
	plistPath := daemonPlistPath()

	// Ensure state directory exists with restrictive permissions.
	// Fix existing permissions if they've drifted.
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		log.Fatalf("install: cannot create state directory %s: %v", stateDir, err)
	}
	os.Chmod(stateDir, 0o700)

	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		log.Fatalf("install: cannot create log directory %s: %v", logDir, err)
	}
	os.Chmod(logDir, 0o700)

	// Check for existing installation.
	existingState, _ := readDaemonState()
	if _, err := os.Stat(plistPath); err == nil && existingState != nil {
		fmt.Println("LaunchAgent already installed. Use 'pokit daemon start' to start it.")
		fmt.Printf("Installed at: %s\n", existingState.InstalledAt)
		fmt.Printf("Binary: %s\n", existingState.BinPath)
		os.Exit(0)
	}

	// ── Upgrade path: backup existing binary before replacing ──
	oldBinPath := ""
	if existingState != nil && existingState.BinPath != "" {
		oldBinPath = existingState.BinPath
	}
	if oldBinPath != "" && oldBinPath != binPath {
		backupPath := oldBinPath + ".pre-upgrade-" + time.Now().UTC().Format("20060102T150405Z")
		if data, err := os.ReadFile(oldBinPath); err == nil {
			if err := os.WriteFile(backupPath, data, 0o700); err != nil {
				fmt.Fprintf(os.Stderr, "install: could not backup previous binary: %v\n", err)
			} else {
				fmt.Printf("Previous binary backed up to: %s\n", backupPath)
			}
		}
	}

	// Stop old daemon if running, before replacing plist.
	if loaded, _ := daemonLoaded(plistPath); loaded {
		runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath)
		fmt.Println("Stopped existing daemon for upgrade.")
	}

	// Build XML plist.
	stdoutPath := filepath.Join(logDir, "daemon-stdout.log")
	stderrPath := filepath.Join(logDir, "daemon-stderr.log")
	plistContent := fmt.Sprintf(plistTemplate, daemonLabel, binPath, stdoutPath, stderrPath, stateDir)

	// Atomic write with unpredictable tmp name.
	tmpPath := plistPath + ".tmp." + randomSuffix()
	if err := os.WriteFile(tmpPath, []byte(plistContent), 0o600); err != nil {
		log.Fatalf("install: cannot write plist: %v", err)
	}
	if err := os.Rename(tmpPath, plistPath); err != nil {
		os.Remove(tmpPath)
		log.Fatalf("install: cannot rename plist: %v", err)
	}

	// Persist daemon state for upgrade/rollback/uninstall tracking.
	state := &daemonState{
		Version:     "1.0.0", // TODO: embed git version at build time
		BinPath:     binPath,
		PlistPath:   plistPath,
		StateDir:    stateDir,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeDaemonState(state); err != nil {
		fmt.Fprintf(os.Stderr, "install: warning: could not write daemon state: %v\n", err)
	}

	fmt.Printf("LaunchAgent installed: %s\n", plistPath)
	fmt.Printf("State directory:    %s\n", stateDir)
	fmt.Printf("Binary:             %s\n", binPath)

	// Bootstrap with launchctl.
	if err := runLaunchctl("bootstrap", "gui/"+currentUserUID(), plistPath); err != nil {
		fmt.Fprintf(os.Stderr, "install: launchctl bootstrap failed: %v\n", err)
		// Rollback: remove plist we just created, restore old binary.
		os.Remove(plistPath)
		if oldBinPath != "" && oldBinPath != binPath {
			backupPath := oldBinPath + ".pre-upgrade-" + time.Now().UTC().Format("20060102T150405Z")
			if data, err := os.ReadFile(backupPath); err == nil {
				os.WriteFile(oldBinPath, data, 0o700)
				os.Remove(backupPath)
				fmt.Fprintf(os.Stderr, "Rolled back to previous binary: %s\n", oldBinPath)
			}
		}
		fmt.Fprintf(os.Stderr, "Removed newly created plist. Prior installation is untouched.\n")
		os.Exit(1)
	}

	fmt.Println("LaunchAgent loaded and started.")
}

// ── Start ──

func startDaemon() {
	plistPath := daemonPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		log.Fatalf("start: LaunchAgent not installed. Run 'pokit daemon install' first.")
	}

	if loaded, pid := daemonLoaded(plistPath); loaded {
		fmt.Printf("Daemon is already loaded (PID %s).\n", pid)
		return
	}

	if err := runLaunchctl("bootstrap", "gui/"+currentUserUID(), plistPath); err != nil {
		log.Fatalf("start: launchctl bootstrap failed: %v", err)
	}
	fmt.Println("Daemon started.")
}

// ── Stop ──

func stopDaemon() {
	plistPath := daemonPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Println("LaunchAgent is not installed. Nothing to stop.")
		return
	}

	if loaded, _ := daemonLoaded(plistPath); !loaded {
		fmt.Println("Daemon is not running.")
		return
	}

	if err := runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath); err != nil {
		fmt.Fprintf(os.Stderr, "stop: launchctl bootout: %v\n", err)
	}
	fmt.Println("Daemon stopped.")
}

// ── Status ──

func statusDaemon() {
	plistPath := daemonPlistPath()

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Println("LaunchAgent: NOT INSTALLED")
		return
	}

	loaded, pid := daemonLoaded(plistPath)
	if !loaded {
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

	if s, err := readDaemonState(); err == nil {
		fmt.Printf("Version: %s\n", s.Version)
		fmt.Printf("Installed: %s\n", s.InstalledAt)
	}
}

// ── Uninstall ──

func uninstallDaemon(purgeTrust bool) {
	plistPath := daemonPlistPath()

	// Stop the agent if running.
	if loaded, _ := daemonLoaded(plistPath); loaded {
		runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath)
		fmt.Println("Daemon stopped.")
	}

	// Read state to know which binary to remove.
	state, _ := readDaemonState()

	// Remove the plist.
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		log.Fatalf("uninstall: cannot remove plist: %v", err)
	}
	fmt.Println("LaunchAgent removed.")

	// Remove tracked executable (only if we installed it).
	if state != nil && state.BinPath != "" {
		currentBin := daemonBinPath()
		if state.BinPath == currentBin {
			fmt.Println("Note: current binary is the installed daemon. Remove manually if desired.")
		} else if _, err := os.Stat(state.BinPath); err == nil {
			if err := os.Remove(state.BinPath); err != nil {
				fmt.Fprintf(os.Stderr, "uninstall: cannot remove daemon binary: %v\n", err)
			} else {
				fmt.Printf("Removed daemon binary: %s\n", state.BinPath)
			}
		}
	}

	// Purge device trust state if requested.
	if purgeTrust {
		stateDir := daemonStateDir()
		fmt.Printf("Purging device trust state in %s...\n", stateDir)
		for _, f := range []string{"host_identity.json", "devices.json", "daemon_state.json"} {
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
