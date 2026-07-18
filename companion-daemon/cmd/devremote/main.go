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

func main() {
	if len(os.Args) > 1 && os.Args[1] == "pair" {
		runPairClient(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "run" {
		runClient(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "devices" {
		runDevicesClient(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "audit" {
		runAuditClient(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		printShellHook()
		return
	}

	ownerUUID := flag.String("owner-uuid", "", "Supabase user UUID that owns this daemon (required for auth)")
	supabaseRef := flag.String("supabase-ref", "", "Supabase project reference for JWKS (e.g. abcdefghijklmnop)")
	insecureLocalOnly := flag.Bool("insecure-local-only", false, "Disable authentication (DANGEROUS)")
	enableLocalPTY := flag.Bool("enable-localpty", false, "Enable LocalPTY adapter (experimental)")
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
		EnableLocalPTY:       *enableLocalPTY,
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
