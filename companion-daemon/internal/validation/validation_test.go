package validation

import (
	"devremote/companion-daemon/internal/workspace"
	"errors"
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
	f = (StalenessCheck{Current: current}).Apply(f)
	if !f.Stale {
		t.Fatal("not stale")
	}
	if !errors.Is(f.CanAuthorize(), ErrStale) {
		t.Fatal("stale finding authorized")
	}
}
