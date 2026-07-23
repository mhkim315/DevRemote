package main

import (
	"fmt"
	"os"
)

// daemonLabel is the deterministic macOS LaunchAgent label.
const daemonLabel = "com.pokit.daemon"

func isDaemonLifecycleVerb(arg string) bool {
	switch arg {
	case "install", "uninstall", "start", "stop", "status":
		return true
	}
	return false
}

// daemonUsage prints the daemon subcommand help.
func daemonUsage() {
	fmt.Fprintf(os.Stderr, `Usage: pokit daemon <command>

Commands:
  install     Install per-user LaunchAgent (versioned path, restrictive permissions)
  uninstall   Remove LaunchAgent (--purge-trust also removes device trust state)
  start       Start the daemon via launchctl (idempotent)
  stop        Stop the daemon via launchctl (idempotent)
  status      Print LaunchAgent load status and daemon health
`)
}

// runDaemonClient routes the "daemon" subcommand.
func runDaemonClient(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("daemon command required")
	}
	switch args[0] {
	case "install":
		return installDaemon()
	case "uninstall":
		purgeTrust := false
		for _, a := range args[1:] {
			if a == "--purge-trust" {
				purgeTrust = true
			}
		}
		return uninstallDaemon(purgeTrust)
	case "start":
		return startDaemon()
	case "stop":
		return stopDaemon()
	case "status":
		return statusDaemon()
	default:
		return fmt.Errorf("unknown daemon command %q", args[0])
	}
}
