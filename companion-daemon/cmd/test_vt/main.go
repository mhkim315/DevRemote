package main

import (
	"log"
	"devremote/companion-daemon/internal/term/vt"
)

func main() {
	term := vt.NewSafeEmulator(80, 24)
	log.Println("Writing OSC 11")
	term.Write([]byte("\x1b]11;?\a"))
	log.Println("Wrote OSC 11")
}
