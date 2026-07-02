package main
import ("log"; "net/http"; "devremote/companion-daemon/internal/term")
func main() {
	http.HandleFunc("/term/ws", term.HandleWS)
	http.HandleFunc("/term/", term.HandleHTML)
	http.HandleFunc("/debug/dump", term.HandleDump)
	http.HandleFunc("/debug/cmd", term.HandleCmd)
	log.Printf("DevRemote :9171")
	log.Fatal(http.ListenAndServe(":9171", nil))
}
