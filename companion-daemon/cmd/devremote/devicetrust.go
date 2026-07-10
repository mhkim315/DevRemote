package main

import (
	"log"
	"os"
	"path/filepath"

	"devremote/companion-daemon/internal/devicetrust"
)

// initDeviceTrust bootstraps the persistent host identity and paired-device
// registry (M2.5-1) and returns the single instances the App owns. It is
// non-fatal here: the trust root is not yet used for authorization, so a
// failure logs a warning and returns nil rather than blocking the daemon.
// (When remote authentication is enabled in M2.5-4, the remote path must fail
// closed.) Only the public host fingerprint is logged — never private-key
// material.
func initDeviceTrust() (*devicetrust.HostIdentity, *devicetrust.DeviceRegistry) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("WARNING: device trust: cannot resolve home dir: %v", err)
		return nil, nil
	}
	dir := filepath.Join(home, ".pokit")
	id, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{
		Path: filepath.Join(dir, "host_identity.json"),
	})
	if err != nil {
		log.Printf("WARNING: device trust: host identity unavailable: %v", err)
		return nil, nil
	}
	reg, err := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{
		Path: filepath.Join(dir, "devices.json"),
	})
	if err != nil {
		log.Printf("WARNING: device trust: device registry unavailable: %v", err)
		return id, nil
	}
	log.Printf("device trust: host identity %s (fingerprint %s), %d paired device(s)",
		id.HostID, id.Fingerprint(), len(reg.List()))
	return id, reg
}
