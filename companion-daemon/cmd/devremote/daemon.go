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
func runDaemonClient(args []string) {
	if len(args) == 0 {
		daemonUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "install":
		installDaemon()
	case "uninstall":
		purgeTrust := false
		for _, a := range args[1:] {
			if a == "--purge-trust" {
				purgeTrust = true
			}
		}
		uninstallDaemon(purgeTrust)
	case "start":
		startDaemon()
	case "stop":
		stopDaemon()
	case "status":
		statusDaemon()
	default:
		fmt.Fprintf(os.Stderr, "pokit daemon: unknown command %q\n", args[0])
		daemonUsage()
		os.Exit(1)
	}
}
