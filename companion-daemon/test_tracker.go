package main

import (
	"fmt"
	"devremote/companion-daemon/internal/mux"
)

func main() {
	panels, err := mux.TrackCmuxPanels()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	for _, p := range panels {
		fmt.Printf("Panel: %+v\n", p)
		uuid, err := mux.FindSessionUUIDByTTY(p.TTY)
		if err == nil {
			fmt.Printf("  -> Found UUID: %s\n", uuid)
		} else {
			fmt.Printf("  -> No UUID: %v\n", err)
		}
	}
}
