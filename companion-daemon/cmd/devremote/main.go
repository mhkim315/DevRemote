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
	default:
		return false
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] != "daemon" && !validSubcommands[os.Args[1]] {
		log.Printf("unknown command: %s", os.Args[1])
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] != "daemon" {
		dispatchSubcommand(os.Args[1], os.Args[2:])
		return
	}

	ownerUUID := flag.String("owner-uuid", "", "Supabase user UUID that owns this daemon (required for auth)")
	supabaseRef := flag.String("supabase-ref", "", "Supabase project reference for JWKS (e.g. abcdefghijklmnop)")
	insecureLocalOnly := flag.Bool("insecure-local-only", false, "Disable authentication (DANGEROUS)")
	enableAgentDetection := flag.Bool("enable-agent-detection", false, "Enable agent detection bridge (experimental)")
	enableManagedCodex := flag.Bool("enable-managed-codex", false, "Enable native managed Codex runtime (SP0, experimental)")
	enableManagedClaude := flag.Bool("enable-managed-claude", false, "Enable native managed Claude runtime (C1D, experimental)")
	claudeDigest := flag.String("claude-digest", "", "Pre-verified SHA-256 digest of pinned Claude binary (required for managed Claude)")

	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		flag.CommandLine.Parse(os.Args[2:])
	} else {
		flag.Parse()
	}

	runDaemon(Config{
		OwnerUUID:            *ownerUUID,
		SupabaseProjectRef:   *supabaseRef,
		InsecureLocalOnly:    *insecureLocalOnly,
		EnableAgentDetection: *enableAgentDetection,
		EnableManagedCodex:   *enableManagedCodex,
		EnableManagedClaude:  *enableManagedClaude,
		ClaudeDigest:         *claudeDigest,
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
