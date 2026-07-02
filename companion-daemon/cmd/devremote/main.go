package main
import ("log"; "net/http"; "devremote/companion-daemon/internal/term")
func main() {
	http.HandleFunc("/term/ws", term.HandleWS)
	http.HandleFunc("/term/", term.HandleHTML)
	log.Printf("DevRemote :9171")
	log.Fatal(http.ListenAndServe(":9171", nil))
}
