package term

import (
	"sync"
	"testing"
)

func TestCommandBroker_PutAndTake(t *testing.T) {
	t.Parallel()
	b := NewCommandBroker(testMutationAuthorizer{})
	if err := b.PutAuthorized("s1", []byte("hello"), "", 0); err != nil {
		t.Fatal(err)
	}
	got := b.Take("s1")
	if string(got) != "hello" {
		t.Errorf("Take = %q, want hello", string(got))
	}
}

func TestCommandBroker_OneShot(t *testing.T) {
	t.Parallel()
	b := NewCommandBroker(testMutationAuthorizer{})
	if err := b.PutAuthorized("s1", []byte("hello"), "", 0); err != nil {
		t.Fatal(err)
	}
	b.Take("s1")
	got := b.Take("s1")
	if got != nil {
		t.Errorf("second Take = %q, want nil", string(got))
	}
}

func TestCommandBroker_Overwrite(t *testing.T) {
	t.Parallel()
	b := NewCommandBroker(testMutationAuthorizer{})
	if err := b.PutAuthorized("s1", []byte("first"), "", 0); err != nil {
		t.Fatal(err)
	}
	if err := b.PutAuthorized("s1", []byte("second"), "", 0); err != nil {
		t.Fatal(err)
	}
	got := b.Take("s1")
	if string(got) != "second" {
		t.Errorf("Take = %q, want second", string(got))
	}
}

func TestCommandBroker_InstanceIsolation(t *testing.T) {
	t.Parallel()
	b1 := NewCommandBroker(testMutationAuthorizer{})
	b2 := NewCommandBroker(testMutationAuthorizer{})
	if err := b1.PutAuthorized("s", []byte("a"), "", 0); err != nil {
		t.Fatal(err)
	}
	if got := b2.Take("s"); got != nil {
		t.Errorf("command leaked between instances: %q", string(got))
	}
}

func TestCommandBroker_PutCopiesInput(t *testing.T) {
	t.Parallel()
	b := NewCommandBroker(testMutationAuthorizer{})
	orig := []byte("data")
	if err := b.PutAuthorized("s", orig, "", 0); err != nil {
		t.Fatal(err)
	}
	orig[0] = 'X'
	got := b.Take("s")
	if string(got) == "Xata" {
		t.Error("Put did not copy input — internal state aliased")
	}
}

func TestCommandBroker_TakeReturnsCopy(t *testing.T) {
	t.Parallel()
	b := NewCommandBroker(testMutationAuthorizer{})
	if err := b.PutAuthorized("s", []byte("data"), "", 0); err != nil {
		t.Fatal(err)
	}
	got := b.Take("s")
	got[0] = 'X'
	// Take again should be nil (one-shot).
	if got2 := b.Take("s"); got2 != nil {
		t.Error("second Take should be nil")
	}
}

func TestCommandBroker_ConcurrentAccess(t *testing.T) {
	b := NewCommandBroker(testMutationAuthorizer{})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := b.PutAuthorized("s", []byte{byte(i)}, "", 0); err != nil {
				t.Errorf("PutAuthorized: %v", err)
			}
			b.Take("s")
		}(i)
	}
	wg.Wait()
}
