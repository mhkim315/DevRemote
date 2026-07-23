//go:build darwin

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
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
	tmp, err := os.CreateTemp(filepath.Dir(path), ".daemon_state.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func xmlEscapeString(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
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
	for _, line := range lines {
		fields := strings.Fields(line)
		// `launchctl list LABEL`: PID, status, label. Do not interpret the
		// first token of `launchctl print` as a PID.
		if len(fields) == 3 && fields[2] == daemonLabel && fields[0] != "-" {
			allDigits := true
			for _, r := range fields[0] {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return fields[0]
			}
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

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pokit-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// replaceBinaryAtomic copies a new executable beside the tracked destination
// and renames it into place. The source is never modified.
func replaceBinaryAtomic(source, destination string) (string, error) {
	if source == destination {
		return "", nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	backup := destination + ".pre-upgrade-" + time.Now().UTC().Format("20060102T150405Z")
	if old, err := os.ReadFile(destination); err == nil {
		if err := writeFileAtomic(backup, old, 0o700); err != nil {
			return "", err
		}
	}
	if err := writeFileAtomic(destination, data, 0o700); err != nil {
		return backup, err
	}
	return backup, nil
}

func restoreBackup(destination, backup string) error {
	if backup == "" {
		return nil
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	return writeFileAtomic(destination, data, 0o700)
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
	if err := os.Chmod(stateDir, 0o700); err != nil {
		log.Fatalf("install: cannot secure state directory: %v", err)
	}

	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		log.Fatalf("install: cannot create log directory %s: %v", logDir, err)
	}
	if err := os.Chmod(logDir, 0o700); err != nil {
		log.Fatalf("install: cannot secure log directory: %v", err)
	}

	// Preserve every prior artifact before touching it. Corrupt/missing state is
	// not permission to discard a pre-existing plist on a failed bootstrap.
	existingState, _ := readDaemonState()
	priorPlist, priorPlistErr := os.ReadFile(plistPath)
	hadPriorPlist := priorPlistErr == nil

	// ── Upgrade path: backup existing binary before replacing ──
	oldBinPath := ""
	if existingState != nil && existingState.BinPath != "" {
		oldBinPath = existingState.BinPath
	}
	backupPath := ""
	if oldBinPath != "" && oldBinPath != binPath {
		var err error
		backupPath, err = replaceBinaryAtomic(binPath, oldBinPath)
		if err != nil {
			log.Fatalf("install: atomic binary replacement failed: %v", err)
		}
	}

	// Stop old daemon if running, before replacing plist.
	if loaded, _ := daemonLoaded(plistPath); loaded {
		if err := runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath); err != nil {
			log.Fatalf("install: cannot stop existing daemon for upgrade: %v", err)
		}
		fmt.Println("Stopped existing daemon for upgrade.")
	}

	// Build XML plist.
	stdoutPath := filepath.Join(logDir, "daemon-stdout.log")
	stderrPath := filepath.Join(logDir, "daemon-stderr.log")
	plistContent := fmt.Sprintf(plistTemplate, xmlEscapeString(daemonLabel), xmlEscapeString(binPath), xmlEscapeString(stdoutPath), xmlEscapeString(stderrPath), xmlEscapeString(stateDir))
	if err := writeFileAtomic(plistPath, []byte(plistContent), 0o600); err != nil {
		log.Fatalf("install: cannot atomically write plist: %v", err)
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
		log.Fatalf("install: cannot atomically write daemon state: %v", err)
	}

	fmt.Printf("LaunchAgent installed: %s\n", plistPath)
	fmt.Printf("State directory:    %s\n", stateDir)
	fmt.Printf("Binary:             %s\n", binPath)

	// Bootstrap with launchctl.
	if err := runLaunchctl("bootstrap", "gui/"+currentUserUID(), plistPath); err != nil {
		fmt.Fprintf(os.Stderr, "install: launchctl bootstrap failed: %v\n", err)
		if hadPriorPlist {
			_ = writeFileAtomic(plistPath, priorPlist, 0o600)
		} else {
			_ = os.Remove(plistPath)
		}
		if err := restoreBackup(oldBinPath, backupPath); err != nil {
			fmt.Fprintf(os.Stderr, "install: rollback binary failed: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "Rolled back failed bootstrap; prior installation was restored.\n")
		os.Exit(1)
	}
	if loaded, _ := daemonLoaded(plistPath); !loaded {
		_ = runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath)
		if hadPriorPlist {
			_ = writeFileAtomic(plistPath, priorPlist, 0o600)
		} else {
			_ = os.Remove(plistPath)
		}
		_ = restoreBackup(oldBinPath, backupPath)
		log.Fatalf("install: readiness check failed after bootstrap; rollback completed")
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
		if err := runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath); err != nil {
			log.Fatalf("uninstall: launchctl bootout failed; refusing to remove installed files: %v", err)
		}
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
		if _, err := os.Stat(state.BinPath); err == nil {
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
