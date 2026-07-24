package term

import "testing"

func TestHandlersHaveNoContextRegistryDependency(t *testing.T) {
	t.Parallel()
	if (&Handlers{}).Lifecycle != nil {
		t.Fatal("zero handler must not invent lifecycle authority")
	}
}

func TestHandlersKeepOwnedLifecycleIndependent(t *testing.T) {
	t.Parallel()
	a := &Handlers{Lifecycle: testLifecycleService(nil, nil)}
	b := &Handlers{Lifecycle: testLifecycleService(nil, nil)}
	if a.Lifecycle == b.Lifecycle {
		t.Fatal("handlers unexpectedly share lifecycle authority")
	}
}
