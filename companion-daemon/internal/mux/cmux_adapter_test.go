package mux

import (
	"strings"
	"testing"
	"strconv"
)

func TestCmuxTopParser(t *testing.T) {
	fixture := `
tag	tag:abcd_claude_code_xyz	workspace:1	Idle
process	9836	tag:abcd_claude_code_xyz	2.1.198
process	9836	surface:1	2.1.198
process	9837	surface:1	bash
`

	lines := strings.Split(fixture, "\n")

	type TopProcess struct {
		PID    int
		Parent string
		Name   string
	}
	type TopTag struct {
		Ref       string
		Workspace string
		Provider  string
	}

	var processes []TopProcess
	var tags []TopTag

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" { continue }
		parts := strings.Split(line, "\t")
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if p == "tag" && i+3 < len(parts) {
				tags = append(tags, TopTag{
					Ref:       strings.TrimSpace(parts[i+1]),
					Workspace: strings.TrimSpace(parts[i+2]),
					Provider:  strings.TrimSpace(parts[i+3]),
				})
				break
			} else if p == "process" && i+3 < len(parts) {
				if pid, err := strconv.Atoi(strings.TrimSpace(parts[i+1])); err == nil {
					processes = append(processes, TopProcess{
						PID:    pid,
						Parent: strings.TrimSpace(parts[i+2]),
						Name:   strings.TrimSpace(parts[i+3]),
					})
				}
				break
			}
		}
	}

	pidParents := make(map[int][]string)
	for _, p := range processes {
		pidParents[p.PID] = append(pidParents[p.PID], p.Parent)
	}

	var targetTagRef string
	for _, tg := range tags {
		if strings.Contains(strings.ToLower(tg.Ref), "claude_code") {
			targetTagRef = tg.Ref
			break
		}
	}

	if targetTagRef != "tag:abcd_claude_code_xyz" {
		t.Fatalf("expected targetTagRef to be tag:abcd_claude_code_xyz, got %s", targetTagRef)
	}

	var agentPID int
	for pid, parents := range pidParents {
		isOnSurface := false
		hasAgentTag := false
		for _, parent := range parents {
			if parent == "surface:1" { isOnSurface = true }
			if targetTagRef != "" && parent == targetTagRef { hasAgentTag = true }
		}
		if isOnSurface && hasAgentTag {
			agentPID = pid
		}
	}

	if agentPID != 9836 {
		t.Fatalf("expected agentPID 9836, got %d", agentPID)
	}
}
