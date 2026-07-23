package main

import (
	"crypto/sha256"
	"encoding/hex"
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

// These are set at build time with -ldflags. If unset, report "dev".
var (
	cliVersion   = "dev"
	cliGitSHA    = "unknown"
	cliBuildTime = "unknown"
)

type doctorResult struct {
	Check  string `json:"check"`
	Status string `json:"status"` // "ok", "warning", "error", "not_installed"
	Detail string `json:"detail"`
}

func runDoctorClient() {
	var results []doctorResult

	// 1. CLI version and build metadata.
	results = append(results, doctorResult{
		Check:  "cli.version",
		Status: "ok",
		Detail: fmt.Sprintf("%s (sha=%s built=%s)", cliVersion, cliGitSHA, cliBuildTime),
	})
	exe, _ := os.Executable()
	results = append(results, doctorResult{
		Check:  "cli.path",
		Status: "ok",
		Detail: exe,
	})

	// 2. Daemon LaunchAgent state (darwin only).
	if isDarwin() {
		plistPath := daemonPlistPath()
		if _, err := os.Stat(plistPath); os.IsNotExist(err) {
			results = append(results, doctorResult{
				Check:  "daemon.launchagent",
				Status: "not_installed",
				Detail: "LaunchAgent plist not found at " + plistPath,
			})
		} else {
			loaded, err := daemonLoaded(plistPath)
			if err != nil {
				results = append(results, doctorResult{
					Check:  "daemon.launchagent",
					Status: "warning",
					Detail: fmt.Sprintf("LaunchAgent state unknown: %v", err),
				})
			} else if loaded {
				results = append(results, doctorResult{
					Check:  "daemon.launchagent",
					Status: "ok",
					Detail: "loaded",
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

	// 6. Provider readiness.
	results = append(results, checkProviderReadiness()...)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(results)
}

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

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:9171/api/sessions")
	if err != nil {
		return []doctorResult{
			{Check: "daemon.listener", Status: "ok", Detail: "listening on 127.0.0.1:9171"},
			{Check: "daemon.auth", Status: "warning", Detail: "HTTP probe failed (expected when auth is enabled)"},
		}
	}
	resp.Body.Close()

	// 401/403 = auth working (expected in remote/production mode).
	// 200 = insecure mode (no auth).
	// Anything else is unexpected.
	var authStatus, authDetail string
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		authStatus = "ok"
		authDetail = fmt.Sprintf("auth required (HTTP %d)", resp.StatusCode)
	case resp.StatusCode == 200:
		authStatus = "warning"
		authDetail = "insecure mode — no authentication required"
	default:
		authStatus = "warning"
		authDetail = fmt.Sprintf("unexpected HTTP %d from /api/sessions", resp.StatusCode)
	}

	return []doctorResult{
		{Check: "daemon.listener", Status: "ok", Detail: "listening on 127.0.0.1:9171"},
		{Check: "daemon.auth", Status: authStatus, Detail: authDetail},
	}
}

// hostIdentityDTO matches the schema written by devicetrust.HostIdentity.
// Only public fields are decoded; privateKey is omitted from the JSON.
type hostIdentityDTO struct {
	HostID     string `json:"hostId"`
	KeyVersion int    `json:"keyVersion"`
	CreatedAt  string `json:"createdAt"`
	PublicKey  []byte `json:"publicKey"`
}

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

	var identity hostIdentityDTO
	if err := json.Unmarshal(data, &identity); err != nil {
		return []doctorResult{{
			Check:  "trust.host_identity",
			Status: "error",
			Detail: fmt.Sprintf("host_identity.json corrupt: %v", err),
		}}
	}

	if identity.HostID == "" || identity.KeyVersion < 1 || len(identity.PublicKey) == 0 {
		return []doctorResult{{Check: "trust.host_identity", Status: "error", Detail: "host_identity.json missing required public identity fields"}}
	}
	sum := sha256.Sum256(identity.PublicKey)
	fp := hex.EncodeToString(sum[:])
	if len(fp) > 16 {
		fp = fp[:16] + "..." // redacted fingerprint for safety
	}

	return []doctorResult{{
		Check:  "trust.host_identity",
		Status: "ok",
		Detail: fmt.Sprintf("host=%s fingerprint=%s version=%d created=%s",
			identity.HostID, fp, identity.KeyVersion, identity.CreatedAt),
	}}
}

// deviceRecordDTO matches the registry schema.
type deviceRecordDTO struct {
	DeviceID    string `json:"deviceId"`
	Fingerprint string `json:"fingerprint"`
	Role        string `json:"role"`
	RevokedAt   string `json:"revokedAt,omitempty"`
}

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

	var devices []deviceRecordDTO
	if err := json.Unmarshal(data, &devices); err != nil {
		return []doctorResult{{
			Check:  "trust.devices",
			Status: "error",
			Detail: fmt.Sprintf("devices.json corrupt: %v", err),
		}}
	}

	active := 0
	revoked := 0
	for _, d := range devices {
		if d.RevokedAt != "" {
			revoked++
		} else {
			active++
		}
	}

	return []doctorResult{{
		Check:  "trust.devices",
		Status: "ok",
		Detail: fmt.Sprintf("%d total, %d active, %d revoked", len(devices), active, revoked),
	}}
}

func checkProviderReadiness() []doctorResult {
	var results []doctorResult

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

	claudePath, err := exec.LookPath("claude")
	if err != nil {
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

func isDarwin() bool {
	return strings.Contains(os.Getenv("GOOS"), "darwin") || func() bool {
		_, err := os.Stat("/System/Library/CoreServices/SystemVersion.plist")
		return err == nil
	}()
}
