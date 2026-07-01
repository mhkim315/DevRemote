//go:build windows

package wrap

import "os"

// ConPTY handles resize automatically; no Unix SIGWINCH equivalent needed.
func resizeLoop(tty *os.File) {}
