//go:build !windows

package wrap

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
)

func resizeLoop(tty *os.File) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if err := pty.InheritSize(os.Stdin, tty); err != nil {
				log.Printf("wrap: resize: %v", err)
			}
		}
	}()
	ch <- syscall.SIGWINCH
}
