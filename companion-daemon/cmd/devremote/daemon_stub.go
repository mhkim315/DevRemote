//go:build !darwin

package main

import (
	"fmt"
	"os"
)

func installDaemon() {
	fmt.Fprintln(os.Stderr, "pokit daemon: LaunchAgent management is only supported on macOS.")
	os.Exit(1)
}

func startDaemon() {
	fmt.Fprintln(os.Stderr, "pokit daemon: LaunchAgent management is only supported on macOS.")
	os.Exit(1)
}

func stopDaemon() {
	fmt.Fprintln(os.Stderr, "pokit daemon: LaunchAgent management is only supported on macOS.")
	os.Exit(1)
}

func statusDaemon() {
	fmt.Fprintln(os.Stderr, "pokit daemon: LaunchAgent management is only supported on macOS.")
	os.Exit(1)
}

func uninstallDaemon(purgeTrust bool) {
	fmt.Fprintln(os.Stderr, "pokit daemon: LaunchAgent management is only supported on macOS.")
	os.Exit(1)
}

func daemonLoaded(plistPath string) (bool, string) {
	return false, ""
}

func daemonPlistPath() string { return "" }
func daemonStateDir() string  { return "" }
func daemonBinPath() string   { return "" }
