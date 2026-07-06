package mux

import (
	"os"
	"testing"
)

func TestCmuxTreeParserRealFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/cmux_tree_real.txt")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	sessions, err := parseCmuxTree(data, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sessions) != 3 {
		t.Errorf("expected 3 terminal sessions, got %d", len(sessions))
	}

	foundS1 := false
	for _, s := range sessions {
		if s.ID() == "surface:1" {
			foundS1 = true
			if s.Title() != "✳ 커밋 내역 및 인수 인계 확인" {
				t.Errorf("unexpected title: %v", s.Title())
			}
		}
		if s.ID() == "surface:5" {
			t.Errorf("browser surface 5 should have been excluded")
		}
	}
	if !foundS1 {
		t.Errorf("did not find surface:1")
	}
}

func TestCmuxTreeParserMissingSurfaces(t *testing.T) {
	// Completely empty output with no tree structure should be an error
	_, err := parseCmuxTree([]byte{}, nil, nil)
	if err == nil {
		t.Fatalf("expected ErrUnexpectedFormat for completely empty output")
	}

	// A tree with valid structure but no surface lines is valid (it means cmux has 0 surfaces).
	// So it should NOT return an error.
	data := []byte(`
window window:1 [current] ◀ active
├── workspace workspace:1 "empty"
│   └── pane pane:1
`)
	sessions, err := parseCmuxTree(data, nil, nil)
	if err != nil {
		t.Fatalf("expected no error for genuinely empty tree, got: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestCmuxTreeParserBrowserOnly(t *testing.T) {
	data := []byte(`
window window:1 [current] ◀ active
├── workspace workspace:1 "browser"
│   └── pane pane:1
│       └── surface surface:5 [browser] "Google" https://google.com
`)
	// Browser surfaces are ignored, so this should return 0 sessions with NO error.
	sessions, err := parseCmuxTree(data, nil, nil)
	if err != nil {
		t.Fatalf("expected no error for browser-only tree, got %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}
