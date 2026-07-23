package validation

import (
	"devremote/companion-daemon/internal/workspace"
	"errors"
	"sync"
	"testing"
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
	store := NewValidationStore()
	defer store.Close()
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

func TestValidationStoreReadRecentRingBuffer(t *testing.T) {
	store := NewValidationStore()
	defer store.Close()
	result := ValidationResult{ID: "result", Binding: binding(), Findings: []Finding{{ID: "finding", Summary: "summary", Binding: binding()}}}
	for i := range 5 {
		r := result
		r.ID = string(rune('a' + i))
		store.Submit(r)
	}
	recent := store.ReadRecent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3, got %d", len(recent))
	}
	if recent[0].ID != "c" || recent[1].ID != "d" || recent[2].ID != "e" {
		t.Fatalf("wrong order: %v %v %v", recent[0].ID, recent[1].ID, recent[2].ID)
	}
	if got := store.ReadRecent(0); got != nil {
		t.Fatal("n=0 should return nil")
	}
	if got := store.ReadRecent(10); len(got) != 5 {
		t.Fatal("n=10 should return 5 items")
	}
}

func TestValidationStoreReadRecentClamped(t *testing.T) {
	store := NewValidationStore()
	defer store.Close()
	result := ValidationResult{ID: "result", Binding: binding(), Findings: []Finding{{ID: "finding", Summary: "summary", Binding: binding()}}}
	for range 100 {
		store.Submit(result)
	}
	recent := store.ReadRecent(500)
	if len(recent) != recentResults {
		t.Fatalf("expected %d (buffer cap), got %d", recentResults, len(recent))
	}
}

func TestValidationStoreConcurrentReadSafety(t *testing.T) {
	store := NewValidationStore()
	defer store.Close()
	result := ValidationResult{ID: "result", Binding: binding(), Findings: []Finding{{ID: "finding", Summary: "summary", Binding: binding()}}}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(2)
		go func() { defer wg.Done(); store.Submit(result) }()
		go func() { defer wg.Done(); store.ReadRecent(5); store.ReadAll() }()
	}
	wg.Wait()
}

func TestValidationCloseIdempotent(t *testing.T) {
	store := NewValidationStore()
	store.Close()
	store.Close()
}
