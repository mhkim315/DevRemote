//go:build !darwin

package main

import (
	"fmt"
)

func unsupportedDaemon() error {
	return fmt.Errorf("pokit daemon: LaunchAgent management is only supported on macOS")
}

func installDaemon() error                  { return unsupportedDaemon() }
func startDaemon() error                    { return unsupportedDaemon() }
func stopDaemon() error                     { return unsupportedDaemon() }
func statusDaemon() error                   { return unsupportedDaemon() }
func uninstallDaemon(purgeTrust bool) error { return unsupportedDaemon() }

func daemonLoaded(plistPath string) (bool, string) {
	return false, ""
}

func daemonPlistPath() string { return "" }
func daemonStateDir() string  { return "" }
func daemonBinPath() string   { return "" }
