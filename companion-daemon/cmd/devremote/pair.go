package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

func runPairClient(args []string) {
	duration := 2 * time.Minute
	for len(args) > 0 && args[0] != "" {
		if args[0] == "--duration" && len(args) > 1 {
			if d, err := time.ParseDuration(args[1]); err == nil && d > 0 && d <= 10*time.Minute {
				duration = d
			}
			args = args[2:]
		} else {
			fmt.Fprintf(os.Stderr, "Usage: pokit pair [--duration <time>]\n")
			os.Exit(1)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot resolve home dir: %v", err)
	}
	dir := filepath.Join(home, ".pokit")

	identity, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{
		Path: filepath.Join(dir, "host_identity.json"),
	})
	if err != nil {
		log.Fatalf("host identity unavailable: %v", err)
	}
	reg, err := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{
		Path: filepath.Join(dir, "devices.json"),
	})
	if err != nil {
		log.Fatalf("device registry unavailable: %v", err)
	}

	fmt.Printf("\nPokit Pairing\n")
	fmt.Printf("Host: %s\nFingerprint: %s\n\n", identity.HostID, identity.Fingerprint())

	session, paired, ok := devicetrust.RunPairing(devicetrust.PairingConfig{
		Listen:          "0.0.0.0:13541",
		SessionLifetime: duration,
		Identity:        identity,
		Registry:        reg,
	})

	if session != nil {
		// Display the pairing payload. It can be scanned as literal text or
		// converted to a QR by a terminal/library. The JSON is the canonical
		// payload; a phone client reads the one-time secret from here.
		payload, _ := json.Marshal(session)
		fmt.Printf("Pairing session: %s\n", session.SessionID)
		fmt.Printf("Expires at: %s\n", session.ExpiresAt.Format(time.RFC3339))
		fmt.Printf("%s\n", string(payload))
		fmt.Printf("\n%c[41;97m  Scan this pairing data from the phone  %c[0m\n", 0x1b, 0x1b)
		fmt.Println()
	}

	if ok {
		fmt.Printf("\n✔ Paired: %s\n", paired.DeviceID)
		fmt.Printf("  Fingerprint: %s\n", paired.Fingerprint)
		fmt.Printf("  Role: %s\n", paired.Role)
	} else {
		fmt.Println("No device paired (timeout or error).")
		if paired.DeviceID != "" {
			fmt.Println(paired.DeviceID)
			os.Exit(0)
		}
		os.Exit(1)
	}
}
