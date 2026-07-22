package cockpit

import "testing"

func TestProjectionIsReadOnly(t *testing.T) {
	if !(Projection{}).ReadOnly() {
		t.Fatal("projection authoritative")
	}
}
