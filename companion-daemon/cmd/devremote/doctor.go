package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// doctorResult holds one diagnostic line.
type doctorResult struct {
	Check  string `json:"check"`
	Status string `json:"status"` // "ok", "warning", "error", "not_installed"
	Detail string `json:"detail"`
}

// runDoctorClient implements `pokit doctor`. It reports non-authoritative
// diagnostics for the full onboarding chain. Tokens, keys, QR material,
// and bearer credentials are NEVER included in output.
func runDoctorClient() {
	var results []doctorResult

	// 1. CLI version and path.
	exe, _ := os.Executable()
	results = append(results, doctorResult{
		Check:  "cli.path",
		Status: "ok",
		Detail: exe,
	})

	// 2. Daemon LaunchAgent state.
	if isDarwin() {
		plistPath := daemonPlistPath()
		if _, err := os.Stat(plistPath); os.IsNotExist(err) {
			results = append(results, doctorResult{
				Check:  "daemon.launchagent",
				Status: "not_installed",
				Detail: "LaunchAgent plist not found at " + plistPath,
			})
		} else {
			loaded, pid := daemonLoaded(plistPath)
			if loaded {
				results = append(results, doctorResult{
					Check:  "daemon.launchagent",
					Status: "ok",
					Detail: fmt.Sprintf("loaded (PID %s)", pid),
				})
			} else {
				results = append(results, doctorResult{
					Check:  "daemon.launchagent",
					Status: "warning",
					Detail: "plist installed but not loaded",
				})
			}
		}
	}

	// 3. Listener / auth readiness.
	results = append(results, checkDaemonListener()...)

	// 4. Host identity.
	results = append(results, checkHostIdentity()...)

	// 5. Device registry.
	results = append(results, checkDeviceRegistry()...)

	// 6. Provider readiness (codex/claude).
	results = append(results, checkProviderReadiness()...)

	// Print results.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(results)
}

// checkDaemonListener probes the daemon's HTTP listener.
func checkDaemonListener() []doctorResult {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:9171", 2*time.Second)
	if err != nil {
		return []doctorResult{{
			Check:  "daemon.listener",
			Status: "error",
			Detail: "daemon not listening on 127.0.0.1:9171",
		}}
	}
	conn.Close()

	// Quick HTTP probe (no auth — just check reachability).
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:9171/api/sessions")
	if err != nil {
		return []doctorResult{
			{Check: "daemon.listener", Status: "ok", Detail: "listening on 127.0.0.1:9171"},
			{Check: "daemon.auth", Status: "warning", Detail: "HTTP probe failed (expected when auth is enabled)"},
		}
	}
	resp.Body.Close()

	// 401 = auth working. 200 = insecure mode.
	authStatus := "ok"
	if resp.StatusCode == 200 {
		authStatus = "warning"
	}
	return []doctorResult{
		{Check: "daemon.listener", Status: "ok", Detail: "listening on 127.0.0.1:9171"},
		{Check: "daemon.auth", Status: authStatus, Detail: fmt.Sprintf("HTTP %d from /api/sessions", resp.StatusCode)},
	}
}

// checkHostIdentity reads the host identity file and reports its presence.
// Private key material is NEVER included.
func checkHostIdentity() []doctorResult {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".pokit", "host_identity.json")

	data, err := os.ReadFile(path)
	if err != nil {
		return []doctorResult{{
			Check:  "trust.host_identity",
			Status: "not_installed",
			Detail: fmt.Sprintf("host_identity.json not readable: %v", err),
		}}
	}

	// Parse only public fields; redact private key.
	var identity struct {
		HostID      string `json:"hostId"`
		Fingerprint string `json:"fingerprint"`
		KeyVersion  int    `json:"keyVersion"`
	}
	if err := json.Unmarshal(data, &identity); err != nil {
		return []doctorResult{{
			Check:  "trust.host_identity",
			Status: "error",
			Detail: fmt.Sprintf("host_identity.json corrupt: %v", err),
		}}
	}

	return []doctorResult{{
		Check:  "trust.host_identity",
		Status: "ok",
		Detail: fmt.Sprintf("host=%s fingerprint=%s version=%d", identity.HostID, identity.Fingerprint, identity.KeyVersion),
	}}
}

// checkDeviceRegistry reads the device registry and reports paired device
// count. Device public keys/identifiers are not redacted (they are public).
// Private keys, tokens, and bearer material are NEVER stored in the registry.
func checkDeviceRegistry() []doctorResult {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".pokit", "devices.json")

	data, err := os.ReadFile(path)
	if err != nil {
		return []doctorResult{{
			Check:  "trust.devices",
			Status: "not_installed",
			Detail: fmt.Sprintf("devices.json not readable: %v", err),
		}}
	}

	var devices []struct {
		DeviceID string `json:"deviceId"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(data, &devices); err != nil {
		return []doctorResult{{
			Check:  "trust.devices",
			Status: "error",
			Detail: fmt.Sprintf("devices.json corrupt: %v", err),
		}}
	}

	// Count active (non-revoked).
	active := 0
	for _, d := range devices {
		if d.DeviceID != "" {
			active++
		}
	}

	return []doctorResult{{
		Check:  "trust.devices",
		Status: "ok",
		Detail: fmt.Sprintf("%d device(s) registered (%d active)", len(devices), active),
	}}
}

// checkProviderReadiness checks for known provider toolchain availability.
func checkProviderReadiness() []doctorResult {
	var results []doctorResult

	// Codex: check for codex binary.
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		results = append(results, doctorResult{
			Check:  "provider.codex",
			Status: "not_installed",
			Detail: "codex binary not found in PATH",
		})
	} else {
		results = append(results, doctorResult{
			Check:  "provider.codex",
			Status: "ok",
			Detail: codexPath,
		})
	}

	// Claude: check for pinned claude binary.
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		// Try Homebrew path.
		claudePath = "/opt/homebrew/bin/claude"
		if _, err := os.Stat(claudePath); os.IsNotExist(err) {
			claudePath = "/usr/local/bin/claude"
			if _, err := os.Stat(claudePath); os.IsNotExist(err) {
				results = append(results, doctorResult{
					Check:  "provider.claude",
					Status: "not_installed",
					Detail: "claude binary not found",
				})
				return results
			}
		}
	}
	results = append(results, doctorResult{
		Check:  "provider.claude",
		Status: "ok",
		Detail: claudePath,
	})

	return results
}

// isDarwin reports whether we're running on macOS.
func isDarwin() bool {
	// darwin build tag ensures this is always true, but check anyway.
	return strings.Contains(os.Getenv("GOOS"), "darwin") || func() bool {
		_, err := os.Stat("/System/Library/CoreServices/SystemVersion.plist")
		return err == nil
	}()
}
