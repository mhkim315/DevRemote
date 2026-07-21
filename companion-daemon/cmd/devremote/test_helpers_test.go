package main

import "devremote/companion-daemon/internal/term"

// testDeps is the shared V1 test composition. It intentionally supplies no
// adapter registry or legacy launcher seam.
func testDeps() Dependencies {
	return Dependencies{Cmds: term.NewCommandBroker()}
}
