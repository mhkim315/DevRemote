//go:build darwin

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	OldBinPath  string `json:"oldBinPath,omitempty"`
	BackupPath  string `json:"backupPath,omitempty"`
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

// Narrow seams keep lifecycle failure paths deterministic in Darwin tests.
// Production continues to use the concrete helpers below.
type launchctlOps interface {
	Bootstrap(string) error
	Bootout(string) error
	Print(string) (string, error)
}
type fsOps interface {
	WriteFile(string, []byte, os.FileMode) error
	ReadFile(string) ([]byte, error)
	Remove(string) error
	Rename(string, string) error
}

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
func upgradeBackupPath(destination string) string {
	return destination + ".pre-upgrade-" + time.Now().UTC().Format("20060102T150405Z")
}

func replaceBinaryAtomic(source, destination string) (string, error) {
	return replaceBinaryAtomicAt(source, destination, upgradeBackupPath(destination))
}

func replaceBinaryAtomicAt(source, destination, backup string) (string, error) {
	if source == destination {
		return "", nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(destination, data, 0o700); err != nil {
		return backup, err
	}
	return backup, nil
}

func backupBinary(source, backup string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeFileAtomic(backup, data, 0o700)
}

// checkReadiness requires launchd registration plus an HTTP response from the
// daemon. Authentication-required is healthy: it proves the listener and its
// auth boundary are both live without requiring a bearer in the installer.
func checkReadiness(plistPath string) error {
	if loaded, _ := daemonLoaded(plistPath); !loaded {
		return fmt.Errorf("LaunchAgent not registered")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for attempt := 0; attempt < 10; attempt++ {
		resp, err := client.Get("http://127.0.0.1:9171/api/sessions")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("daemon listener/auth readiness failed")
}

func rollbackInstall(plistPath string, priorPlist []byte, hadPriorPlist bool, statePath string, priorState []byte, hadPriorState bool, binaryPath, backupPath string, priorWasLoaded bool) error {
	if backupPath != "" {
		if _, err := os.Stat(backupPath); err == nil {
			if err := restoreBackup(binaryPath, backupPath); err != nil {
				return fmt.Errorf("restore binary: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat rollback backup: %w", err)
		} else if !hadPriorState {
			if err := os.Remove(binaryPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove new binary: %w", err)
			}
		}
	} else if !hadPriorState {
		if err := os.Remove(binaryPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove new binary: %w", err)
		}
	}
	if hadPriorPlist {
		if err := writeFileAtomic(plistPath, priorPlist, 0o600); err != nil {
			return fmt.Errorf("restore plist: %w", err)
		}
	} else {
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove new plist: %w", err)
		}
	}
	if hadPriorState {
		if err := writeFileAtomic(statePath, priorState, 0o600); err != nil {
			return fmt.Errorf("restore daemon state: %w", err)
		}
	} else {
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove new daemon state: %w", err)
		}
	}
	if backupPath != "" {
		if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove rollback backup: %w", err)
		}
	}
	if priorWasLoaded && hadPriorPlist {
		if err := runLaunchctl("bootstrap", "gui/"+currentUserUID(), plistPath); err != nil {
			return fmt.Errorf("restart prior daemon: %w", err)
		}
	}
	return nil
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

// validateDaemonPaths returns an error if any path field in the state
// is not absolute, is outside the expected state directory, or contains
// traversal. A crafted/corrupt state file must never direct the daemon
// to overwrite or delete an arbitrary path.
func validateDaemonPaths(state *daemonState) error {
	if state == nil {
		return fmt.Errorf("daemon state is nil")
	}
	stateDir := daemonStateDir()
	resolve := func(path string) (string, error) {
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("not absolute: %q", path)
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved, nil
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return "", err
		}
		return filepath.Join(parent, filepath.Base(path)), nil
	}
	for _, p := range []struct{ name, value string }{
		{"binPath", state.BinPath}, {"oldBinPath", state.OldBinPath}, {"backupPath", state.BackupPath},
		{"plistPath", state.PlistPath}, {"stateDir", state.StateDir},
	} {
		if p.value == "" {
			return fmt.Errorf("daemon state %s is empty", p.name)
		}
	}
	resolvedStateDir, err := resolve(stateDir)
	if err != nil {
		return err
	}
	managedBin := filepath.Join(resolvedStateDir, "bin", "devremote")
	resolvedBin, err := resolve(state.BinPath)
	if err != nil {
		return fmt.Errorf("daemon state binPath: %w", err)
	}
	currentExe, _ := resolve(daemonBinPath())
	if resolvedBin != managedBin && resolvedBin != currentExe {
		return fmt.Errorf("daemon state binPath is not a managed binary: %q", state.BinPath)
	}
	resolvedOld, err := resolve(state.OldBinPath)
	if err != nil {
		return fmt.Errorf("daemon state oldBinPath: %w", err)
	}
	if resolvedOld != resolvedBin && !strings.HasPrefix(resolvedOld, resolvedBin+".old-") {
		return fmt.Errorf("daemon state oldBinPath is not related to binPath: %q", state.OldBinPath)
	}
	resolvedBackup, err := resolve(state.BackupPath)
	if err != nil {
		return fmt.Errorf("daemon state backupPath: %w", err)
	}
	if !strings.HasPrefix(resolvedBackup, resolvedBin+".pre-upgrade-") {
		return fmt.Errorf("daemon state backupPath is not related to binPath: %q", state.BackupPath)
	}
	resolvedPlist, err := resolve(state.PlistPath)
	if err != nil {
		return err
	}
	expectedPlist, err := resolve(daemonPlistPath())
	if err != nil {
		return err
	}
	if resolvedPlist != expectedPlist {
		return fmt.Errorf("daemon state plistPath %q is not expected plist", state.PlistPath)
	}
	resolvedRecordedState, err := resolve(state.StateDir)
	if err != nil {
		return err
	}
	if resolvedRecordedState != resolvedStateDir {
		return fmt.Errorf("daemon state stateDir %q does not match expected", state.StateDir)
	}
	return nil
}

// ── Install ──

func installDaemon() error {
	binPath := daemonBinPath()
	if binPath == "" {
		return fmt.Errorf("install: cannot determine executable path")
	}
	stateDir := daemonStateDir()
	plistPath := daemonPlistPath()

	// Ensure state directory exists with restrictive permissions.
	// Fix existing permissions if they've drifted.
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("install: cannot create state directory %s: %w", stateDir, err)
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return fmt.Errorf("install: cannot secure state directory: %w", err)
	}

	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("install: cannot create log directory %s: %w", logDir, err)
	}
	if err := os.Chmod(logDir, 0o700); err != nil {
		return fmt.Errorf("install: cannot secure log directory: %w", err)
	}
	binDir := filepath.Join(stateDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return fmt.Errorf("install: cannot create binary directory: %w", err)
	}

	// Preserve every prior artifact before touching it. A corrupt/unreadable
	// state file means we cannot safely upgrade or uninstall — fail closed.
	existingState, stateErr := readDaemonState()
	if stateErr != nil && !os.IsNotExist(stateErr) {
		return fmt.Errorf("install: daemon state is corrupt: %w", stateErr)
	}
	if existingState != nil {
		if err := validateDaemonPaths(existingState); err != nil {
			return fmt.Errorf("install: daemon state path validation: %w", err)
		}
	}
	priorPlist, priorPlistErr := os.ReadFile(plistPath)
	hadPriorPlist := priorPlistErr == nil
	statePath := filepath.Join(stateDir, "daemon_state.json")
	priorState, priorStateErr := os.ReadFile(statePath)
	hadPriorState := priorStateErr == nil

	// ── Upgrade path: backup existing binary before replacing ──
	oldBinPath := ""
	if existingState != nil && existingState.BinPath != "" {
		oldBinPath = existingState.BinPath
	}
	serviceBinPath := filepath.Join(binDir, "devremote")
	if oldBinPath != "" {
		serviceBinPath = oldBinPath
	}
	backupPath := upgradeBackupPath(serviceBinPath)
	if oldBinPath != "" {
		backupPath = upgradeBackupPath(serviceBinPath)
		// Snapshot the old executable before any service definition changes.
		if err := backupBinary(serviceBinPath, backupPath); err != nil {
			return fmt.Errorf("install: cannot back up existing binary: %w", err)
		}
	}

	// The new definition is fully durable before the old service is stopped.
	priorWasLoaded, _ := daemonLoaded(plistPath)
	stdoutPath := filepath.Join(logDir, "daemon-stdout.log")
	stderrPath := filepath.Join(logDir, "daemon-stderr.log")
	plistContent := fmt.Sprintf(plistTemplate, xmlEscapeString(daemonLabel), xmlEscapeString(serviceBinPath), xmlEscapeString(stdoutPath), xmlEscapeString(stderrPath), xmlEscapeString(stateDir))
	if err := writeFileAtomic(plistPath, []byte(plistContent), 0o600); err != nil {
		if backupPath != "" {
			if removeErr := os.Remove(backupPath); removeErr != nil && !os.IsNotExist(removeErr) {
				log.Printf("install: backup cleanup after plist failure failed: %v", removeErr)
			}
		}
		return fmt.Errorf("install: cannot atomically write plist: %w", err)
	}

	// Persist daemon state for upgrade/rollback/uninstall tracking.
	state := &daemonState{
		Version:     "1.0.0", // TODO: embed git version at build time
		BinPath:     serviceBinPath,
		OldBinPath:  serviceBinPath,
		BackupPath:  backupPath,
		PlistPath:   plistPath,
		StateDir:    stateDir,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeDaemonState(state); err != nil {
		if rbErr := rollbackInstall(plistPath, priorPlist, hadPriorPlist, statePath, priorState, hadPriorState, serviceBinPath, backupPath, false); rbErr != nil {
			return errors.Join(fmt.Errorf("install: state write: %w", err), rbErr)
		}
		return fmt.Errorf("install: cannot atomically write daemon state: %w", err)
	}

	// Stop the old agent only after the new plist/state are ready. A bootout
	// failure restores staged files but never attempts a duplicate bootstrap.
	if priorWasLoaded {
		if err := runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath); err != nil {
			if rbErr := rollbackInstall(plistPath, priorPlist, hadPriorPlist, statePath, priorState, hadPriorState, serviceBinPath, backupPath, priorWasLoaded); rbErr != nil {
				return errors.Join(fmt.Errorf("install: bootout old: %w", err), rbErr)
			}
			return fmt.Errorf("install: cannot stop existing daemon for upgrade: %w", err)
		}
		fmt.Println("Stopped existing daemon for upgrade.")
	}

	fmt.Printf("LaunchAgent installed: %s\n", plistPath)
	fmt.Printf("State directory:    %s\n", stateDir)
	fmt.Printf("Binary:             %s\n", binPath)

	// Binary replacement is deliberately last before bootstrap. Until this point
	// the old executable is intact and rollback can restore the prior service.
	if serviceBinPath != binPath {
		actualBackup, err := replaceBinaryAtomicAt(binPath, serviceBinPath, backupPath)
		if err != nil {
			if rbErr := rollbackInstall(plistPath, priorPlist, hadPriorPlist, statePath, priorState, hadPriorState, serviceBinPath, backupPath, priorWasLoaded); rbErr != nil {
				return errors.Join(fmt.Errorf("install: binary replace: %w", err), rbErr)
			}
			return fmt.Errorf("install: atomic binary replacement failed: %w", err)
		}
		backupPath = actualBackup
	}

	// Bootstrap with launchctl.
	if err := runLaunchctl("bootstrap", "gui/"+currentUserUID(), plistPath); err != nil {
		fmt.Fprintf(os.Stderr, "install: launchctl bootstrap failed: %v\n", err)
		if rbErr := rollbackInstall(plistPath, priorPlist, hadPriorPlist, statePath, priorState, hadPriorState, serviceBinPath, backupPath, priorWasLoaded); rbErr != nil {
			return errors.Join(fmt.Errorf("install: bootstrap: %w", err), rbErr)
		}
		fmt.Fprintf(os.Stderr, "Rolled back failed bootstrap; prior installation was restored.\n")
		return fmt.Errorf("install: launchctl bootstrap failed: %w", err)
	}
	if err := checkReadiness(plistPath); err != nil {
		stopErr := runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath)
		if rbErr := rollbackInstall(plistPath, priorPlist, hadPriorPlist, statePath, priorState, hadPriorState, serviceBinPath, backupPath, priorWasLoaded); rbErr != nil {
			return errors.Join(fmt.Errorf("install: readiness: %w", err), stopErr, rbErr)
		}
		return errors.Join(fmt.Errorf("install: readiness check failed: %w", err), stopErr)
	}
	if backupPath != "" {
		if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
			log.Printf("install: degraded: orphan backup retained at %s: %v", backupPath, err)
			return fmt.Errorf("install: upgrade succeeded with orphan backup %s: %w", backupPath, err)
		}
	}

	fmt.Println("LaunchAgent loaded and started.")
	return nil
}

// ── Start ──

func startDaemon() error {
	plistPath := daemonPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return fmt.Errorf("start: LaunchAgent not installed")
	}

	if loaded, pid := daemonLoaded(plistPath); loaded {
		fmt.Printf("Daemon is already loaded (PID %s).\n", pid)
		return nil
	}

	if err := runLaunchctl("bootstrap", "gui/"+currentUserUID(), plistPath); err != nil {
		return fmt.Errorf("start: launchctl bootstrap failed: %w", err)
	}
	fmt.Println("Daemon started.")
	return nil
}

// ── Stop ──

func stopDaemon() error {
	plistPath := daemonPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Println("LaunchAgent is not installed. Nothing to stop.")
		return nil
	}

	if loaded, _ := daemonLoaded(plistPath); !loaded {
		fmt.Println("Daemon is not running.")
		return nil
	}

	if err := runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath); err != nil {
		return fmt.Errorf("stop: launchctl bootout failed: %w", err)
	}
	fmt.Println("Daemon stopped.")
	return nil
}

// ── Status ──

func statusDaemon() error {
	plistPath := daemonPlistPath()

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Println("LaunchAgent: NOT INSTALLED")
		return nil
	}

	loaded, pid := daemonLoaded(plistPath)
	if !loaded {
		fmt.Println("LaunchAgent: INSTALLED (not loaded)")
		fmt.Printf("Plist: %s\n", plistPath)
		return nil
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
	return nil
}

// ── Uninstall ──

func uninstallDaemon(purgeTrust bool) error {
	plistPath := daemonPlistPath()
	// Validate first: a corrupt state must not trigger lifecycle changes or any
	// deletion because it cannot safely identify the owned artifacts.
	state, stateErr := readDaemonState()
	if stateErr != nil && !os.IsNotExist(stateErr) {
		return fmt.Errorf("uninstall: daemon state corrupt: %w", stateErr)
	}
	if state != nil {
		if err := validateDaemonPaths(state); err != nil {
			return fmt.Errorf("uninstall: daemon state path validation: %w", err)
		}
	}

	// Stop the agent if running.
	if loaded, _ := daemonLoaded(plistPath); loaded {
		if err := runLaunchctl("bootout", "gui/"+currentUserUID(), plistPath); err != nil {
			return fmt.Errorf("uninstall: launchctl bootout failed: %w", err)
		}
		fmt.Println("Daemon stopped.")
	}

	// Remove the plist.
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("uninstall: cannot remove plist: %w", err)
	}
	fmt.Println("LaunchAgent removed.")

	// All tracked artifacts must be removed before metadata is discarded. A
	// deletion failure leaves daemon_state intact so uninstall can be retried.
	if state != nil {
		seen := map[string]bool{}
		for _, artifact := range []string{state.BinPath, state.OldBinPath, state.BackupPath} {
			if seen[artifact] {
				continue
			}
			seen[artifact] = true
			if err := os.Remove(artifact); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("uninstall: cannot remove daemon artifact %s: %w", artifact, err)
			}
			fmt.Printf("Removed daemon artifact: %s\n", artifact)
		}
	}

	// Remove daemon state JSON (always removed on uninstall).
	statePath := filepath.Join(daemonStateDir(), "daemon_state.json")
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("uninstall: cannot remove daemon state file: %w", err)
	}

	// Purge device trust state if requested.
	if purgeTrust {
		stateDir := daemonStateDir()
		fmt.Printf("Purging device trust state in %s...\n", stateDir)
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
	return nil
}
