package workspace

import (
	"errors"
	"testing"
	"time"
)

func identity() Identity {
	return Identity{RepositoryID: "repo-1", Mode: ModeSharedSequential, BaseSHA: "base", CurrentSHA: "head", TreeHash: "tree-1", SnapshotID: "snapshot-1", IsolationProfile: IsolationRepoOnly}
}

func request() AcquireRequest {
	return AcquireRequest{Identity: identity(), OwnerRuntimeID: "runtime-1", OwnerSessionID: "session-1", OwnerLaunchGeneration: 2, TTL: time.Minute}
}

func TestCleanSnapshotContractAndFindingStaleness(t *testing.T) {
	manifest := SnapshotManifest{BaseSHA: "base", TargetSHA: "head", TreeHash: "tree-1", IndexHash: "index", UntrackedManifestDigest: "untracked", DiffDigest: "diff", Clean: true}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	manifest.Clean = false
	if err := manifest.Validate(); !errors.Is(err, ErrDirtySnapshot) {
		t.Fatalf("dirty manifest = %v", err)
	}
	binding := FindingBinding{RepositoryID: "repo-1", SnapshotID: "snapshot-1", TreeHash: "tree-1"}
	if binding.Stale(identity()) {
		t.Fatal("matching finding marked stale")
	}
	changed := identity()
	changed.TreeHash = "tree-2"
	if !binding.Stale(changed) {
		t.Fatal("tree change did not stale finding")
	}
}

func TestCooperativeAcquireCompareReleaseAndStaleOwner(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	m := NewManager(func() time.Time { return now })
	first, err := m.Acquire(request())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire(request()); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("second acquire = %v", err)
	}
	stale := first
	stale.Epoch++
	if err := m.Release(stale); !errors.Is(err, ErrStaleOwner) {
		t.Fatalf("stale release = %v", err)
	}
	if err := m.Release(first); err != nil {
		t.Fatal(err)
	}
	second, err := m.Acquire(request())
	if err != nil {
		t.Fatal(err)
	}
	if second.Epoch <= first.Epoch {
		t.Fatalf("epoch did not advance: %d <= %d", second.Epoch, first.Epoch)
	}
}

func TestExpiryRecoversDaemonCrashAndRejectsStaleHeartbeat(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	m := NewManager(func() time.Time { return now })
	old, err := m.Acquire(request())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if err := m.Check(old); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("expired check = %v", err)
	}
	newLease, err := m.Acquire(request())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Heartbeat(old, time.Minute); !errors.Is(err, ErrStaleOwner) {
		t.Fatalf("old heartbeat = %v", err)
	}
	if err := m.Check(newLease); err != nil {
		t.Fatal(err)
	}
}

func TestExternalDriftInvalidatesValidationLease(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	m := NewManager(func() time.Time { return now })
	lease, err := m.Acquire(request())
	if err != nil {
		t.Fatal(err)
	}
	changed := identity()
	changed.TreeHash = "tree-2"
	if err := m.DetectDrift(changed); !errors.Is(err, ErrExternalDrift) {
		t.Fatalf("drift = %v", err)
	}
	if err := m.Check(lease); !errors.Is(err, ErrStaleOwner) {
		t.Fatalf("drifted lease check = %v", err)
	}
}

func TestIdentityRejectsUnsupportedModeAndIsolationClaim(t *testing.T) {
	i := identity()
	i.Mode = "unknown"
	if err := i.Validate(); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("mode = %v", err)
	}
	i = identity()
	i.IsolationProfile = "magic"
	if err := i.Validate(); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("profile = %v", err)
	}
}
