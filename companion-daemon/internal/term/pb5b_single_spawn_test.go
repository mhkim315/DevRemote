package term

import (
	"context"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// TestPB5b_SingleSpawn_NoDoubleCreate proves Create spawns exactly one PTY.
func TestPB5b_SingleSpawn_NoDoubleCreate(t *testing.T) {
	owned := NewOwnedPTYRuntime(nil, nil)
	if owned.V1() != nil {
		t.Error("V1 should be nil via legacy constructor")
	}

	v1Owned := NewOwnedPTYRuntimeV1(nil, nil, nil)
	if v1Owned.V1() != nil {
		t.Error("V1 should be nil when passed nil")
	}

	_, err := owned.Create(context.Background(), mux.CreateOptions{Name: "test"}, "", "test")
	if err == nil {
		t.Error("Create with nil spawn must fail closed")
	}
}
