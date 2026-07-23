package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
)

// validSubcommands is the closed set of recognized subcommands. Unknown
// subcommands fail with a nonzero exit rather than silently starting the
// daemon. PA2a removed "link", "unlink", and "links" from this set.
var validSubcommands = map[string]bool{
	"pair":    true,
	"run":     true,
	"devices": true,
	"audit":   true,
	"hook":    true,
	"daemon":  true,
	"doctor":  true,
}

// dispatchSubcommand routes recognized subcommands. Returns false when
// the subcommand is unknown (caller must print an error and exit).
// Extracted for testability.
func dispatchSubcommand(cmd string, args []string) bool {
	switch cmd {
	case "pair":
		runPairClient(args)
		return true
	case "run":
		runClient(args)
		return true
	case "devices":
		runDevicesClient(args)
		return true
	case "audit":
		runAuditClient(args)
		return true
	case "hook":
		printShellHook()
		return true
	case "daemon":
		runDaemonClient(args)
		return true
	case "doctor":
		runDoctorClient()
		return true
	default:
		return false
	}
}

func main() {
	// "pokit daemon install|start|stop|status|uninstall" = CLI dispatch.
	// "pokit daemon" (no subcommand) = foreground daemon serve.
	if len(os.Args) > 1 && os.Args[1] == "daemon" && len(os.Args) > 2 {
		dispatchSubcommand(os.Args[1], os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] != "daemon" {
		if !validSubcommands[os.Args[1]] {
			log.Printf("unknown command: %s", os.Args[1])
			os.Exit(1)
		}
		dispatchSubcommand(os.Args[1], os.Args[2:])
		return
	}

	ownerUUID := flag.String("owner-uuid", "", "Supabase user UUID that owns this daemon (required for auth)")
	supabaseRef := flag.String("supabase-ref", "", "Supabase project reference for JWKS (e.g. abcdefghijklmnop)")
	listenAddr := flag.String("listen-addr", "", "Loopback listen address for insecure mode (e.g. 127.0.0.1:0); ignored without --insecure-local-only")
	insecureLocalOnly := flag.Bool("insecure-local-only", false, "Disable authentication (DANGEROUS)")
	enableAgentDetection := flag.Bool("enable-agent-detection", false, "Enable agent detection bridge (experimental)")
	enableManagedCodex := flag.Bool("enable-managed-codex", false, "Enable native managed Codex runtime (SP0, experimental)")
	enableManagedClaude := flag.Bool("enable-managed-claude", false, "Enable native managed Claude runtime (C1D, experimental)")
	claudeDigest := flag.String("claude-digest", "", "Pre-verified SHA-256 digest of pinned Claude binary (required for managed Claude)")
	enableTimelineShadow := flag.Bool("enable-timeline-shadow", false, "Enable fail-open Operational Canonical Timeline shadow writer (STEP4, experimental)")
	timelineShadowPath := flag.String("timeline-shadow-path", "", "Absolute path for Timeline shadow writer (optional)")
	enableWorkspaceLease := flag.Bool("enable-workspace-lease", false, "Enable cooperative workspace lease contract (STEP5, experimental)")
	enableFrozenValidation := flag.Bool("enable-frozen-validation", false, "Enable frozen clean-snapshot validation contract (STEP7, experimental)")
	enableCockpit := flag.Bool("enable-cockpit", false, "Enable read-only operational cockpit route (STEP8, experimental)")
	enableProjectionConvergence := flag.Bool("enable-projection-convergence", false, "Enable read-only Timeline projection convergence (STEP9.2, experimental)")
	enableN1Notifications := flag.Bool("enable-n1-notifications", false, "Enable N1 exact-event locator notifications (STEP9.3, experimental)")

	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		flag.CommandLine.Parse(os.Args[2:])
	} else {
		flag.Parse()
	}

	runDaemon(Config{
		OwnerUUID:                   *ownerUUID,
		SupabaseProjectRef:          *supabaseRef,
		ListenAddr:                  *listenAddr,
		InsecureLocalOnly:           *insecureLocalOnly,
		EnableAgentDetection:        *enableAgentDetection,
		EnableManagedCodex:          *enableManagedCodex,
		EnableManagedClaude:         *enableManagedClaude,
		ClaudeDigest:                *claudeDigest,
		EnableTimelineShadow:        *enableTimelineShadow,
		TimelineShadowPath:          *timelineShadowPath,
		EnableWorkspaceLease:        *enableWorkspaceLease,
		EnableProjectionConvergence: *enableProjectionConvergence,
		EnableN1Notifications:       *enableN1Notifications,
		EnableFrozenValidation:      *enableFrozenValidation,
		EnableCockpit:               *enableCockpit,
	})
}

func sendPushNotification(token, message, sessionID string) {
	payloadMap := map[string]interface{}{
		"to":    token,
		"title": "Pokit",
		"body":  message,
		"data": map[string]string{
			"sessionId": sessionID,
			"type":      "approval_required",
			"url":       "pokit://session/" + url.PathEscape(sessionID),
		},
	}
	payloadBytes, _ := json.Marshal(payloadMap)

	resp, err := http.Post("https://exp.host/--/api/v2/push/send", "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Printf("Failed to send push: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("Push sent for session=%s (Status: %s)", sessionID, resp.Status)
}
