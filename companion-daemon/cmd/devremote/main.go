package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "run" {
		runClient(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "link" || os.Args[1] == "unlink" || os.Args[1] == "links") {
		runLinkerClient(os.Args[1], os.Args[2:])
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
