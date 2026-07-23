package validation

import (
	"devremote/companion-daemon/internal/workspace"
	"errors"
	"sync"
	"testing"
	"time"
)

func binding() SnapshotBinding {
	return SnapshotBinding{RepositoryID: "repo", Manifest: workspace.SnapshotManifest{BaseSHA: "b", TargetSHA: "t", TreeHash: "tree", IndexHash: "idx", UntrackedManifestDigest: "u", DiffDigest: "d", Clean: true}, SnapshotID: "snap", LeaseEpoch: 1, ValidatorProvider: "codex", ValidatorModel: "m", ValidatorRuntimeID: "r", ValidatorSessionID: "s", ValidatorGeneration: 1, ConfigEpoch: 1, EvidenceDigest: "e", ArtifactDigest: "a", IsolationProfile: workspace.IsolationRepoOnly}
}
func TestAnyIdentityChangeStalesFinding(t *testing.T) {
	base := binding()
	cases := []func(*SnapshotBinding){func(b *SnapshotBinding) { b.Manifest.TargetSHA = "x" }, func(b *SnapshotBinding) { b.LeaseEpoch++ }, func(b *SnapshotBinding) { b.ValidatorGeneration++ }, func(b *SnapshotBinding) { b.ConfigEpoch++ }, func(b *SnapshotBinding) { b.EvidenceDigest = "x" }, func(b *SnapshotBinding) { b.ArtifactDigest = "x" }}
	for _, change := range cases {
		current := base
		change(&current)
		if !(StalenessCheck{Current: current}).Stale(base) {
			t.Fatal("identity change accepted")
		}
	}
}
func TestStaleFindingVisibleButCannotAuthorize(t *testing.T) {
	b := binding()
	f := Finding{ID: "f", Summary: "finding", Binding: b}
	current := b
	current.LeaseEpoch++
	if !errors.Is(f.CanAuthorize(current), ErrStale) {
		t.Fatal("omitted Apply authorized stale finding")
	}
	f = (StalenessCheck{Current: current}).Apply(f)
	if !f.Stale {
		t.Fatal("not stale")
	}
	if !errors.Is(f.CanAuthorize(current), ErrStale) {
		t.Fatal("stale finding authorized")
	}
}

func TestInvalidAndMismatchedBindingsFailClosed(t *testing.T) {
	zero := SnapshotBinding{}
	if !(StalenessCheck{Current: zero}).Stale(zero) {
		t.Fatal("invalid bindings fresh")
	}
	b := binding()
	r := ValidationResult{ID: "r", Binding: b, Findings: []Finding{{ID: "f", Summary: "x", Binding: b}}}
	if r.Validate() != nil {
		t.Fatal("valid result")
	}
	r.Findings[0].Binding.LeaseEpoch++
	if !errors.Is(r.Validate(), ErrInvalid) {
		t.Fatal("mismatched finding accepted")
	}
}

func TestValidationStoreSubmitReadRoundTrip(t *testing.T) {
	store := &ValidationStore{}
	result := ValidationResult{ID: "result", Binding: binding(), Findings: []Finding{{ID: "finding", Summary: "summary", Binding: binding()}}}
	if err := store.Submit(result); err != nil {
		t.Fatal(err)
	}
	result.Findings[0].Summary = "caller mutation"
	got := store.ReadAll()
	if len(got) != 1 || got[0].ID != "result" || got[0].Findings[0].Summary != "summary" {
		t.Fatalf("ReadAll() = %#v", got)
	}
	got[0].Findings[0].Summary = "reader mutation"
	if again := store.ReadAll(); again[0].Findings[0].Summary != "summary" {
		t.Fatalf("store leaked mutable result: %#v", again)
	}
}

func TestValidationStoreSubscribeNotifiesAfterSubmit(t *testing.T) {
	store := NewValidationStore()
	first, second := make(chan ValidationResult, 1), make(chan ValidationResult, 1)
	store.Subscribe(func(result ValidationResult) { first <- result })
	store.Subscribe(func(result ValidationResult) { second <- result })
	result := ValidationResult{ID: "result", Binding: binding(), Findings: []Finding{{ID: "finding", Summary: "summary", Binding: binding()}}}
	if err := store.Submit(result); err != nil {
		t.Fatal(err)
	}
	for _, received := range []<-chan ValidationResult{first, second} {
		select {
		case got := <-received:
			if got.ID != result.ID {
				t.Fatalf("subscriber received %#v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber was not notified")
		}
	}
}

func TestValidationStoreConcurrentReadSafety(t *testing.T) {
	store := NewValidationStore()
	result := ValidationResult{ID: "result", Binding: binding(), Findings: []Finding{{ID: "finding", Summary: "summary", Binding: binding()}}}
	const readers = 32
	var wg sync.WaitGroup
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.ReadAll()
		}()
	}
	if err := store.Submit(result); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}
