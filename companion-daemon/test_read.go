package main
import (
	"log"
	"io"
	"github.com/charmbracelet/x/vt"
)
func main() {
	term := vt.NewSafeEmulator(80, 24)
	go io.Copy(io.Discard, term)
	log.Println("Writing OSC 11")
	term.Write([]byte("\x1b]11;?\a"))
	log.Println("Wrote OSC 11")
}
