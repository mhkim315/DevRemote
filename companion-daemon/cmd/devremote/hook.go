package main

import (
	"fmt"
	"os"
)

// printShellHook outputs the shell alias script.
// Users can add `eval "$(pokit hook)"` to their .zshrc or .bashrc
func printShellHook() {
	hookScript := `
# POKIT Shell Integration (The Invisible Cockpit)
# This intercepts known AI agent commands and transparently wraps them in 'pokit run'

_pokit_wrap() {
    local cmd=$1
    shift
    # If the pokit daemon is running, use it
    if curl -s http://127.0.0.1:9171/api/sessions > /dev/null; then
        pokit run "$cmd" "$@"
    else
        # Fallback to normal execution if daemon is down
        command "$cmd" "$@"
    fi
}

alias claude="_pokit_wrap claude"
alias aider="_pokit_wrap aider"
`
	fmt.Println(hookScript)
	os.Exit(0)
}
