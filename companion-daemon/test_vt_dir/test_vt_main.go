package main

import (
	"log"
	"github.com/charmbracelet/x/vt"
)

func main() {
	term := vt.New(80, 24)
	log.Println("Writing OSC 11")
	term.Write([]byte("\x1b]11;?\a"))
	log.Println("Wrote OSC 11")
}
