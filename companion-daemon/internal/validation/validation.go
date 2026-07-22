// Package validation defines frozen clean-snapshot finding bindings. It is a
// pure contract: it does not dispatch validators, inspect dirty worktrees, or
// authorize acceptance, merge, or lifecycle transitions.
package validation

import (
	"errors"

	"devremote/companion-daemon/internal/workspace"
)

var (
	ErrInvalid = errors.New("validation: invalid binding")
	ErrStale   = errors.New("validation: finding is stale and cannot authorize")
)

// SnapshotBinding freezes every identity that a validation result may rely on.
// A clean SnapshotManifest is required; dirty-worktree validation is deferred.
type SnapshotBinding struct {
	RepositoryID                                                              string
	Manifest                                                                  workspace.SnapshotManifest
	SnapshotID                                                                string
	LeaseEpoch                                                                uint64
	ValidatorProvider, ValidatorModel, ValidatorRuntimeID, ValidatorSessionID string
	ValidatorGeneration, ConfigEpoch                                          uint64
	EvidenceDigest, ArtifactDigest                                            string
	IsolationProfile                                                          workspace.IsolationProfile
}

func (b SnapshotBinding) Validate() error {
	if b.RepositoryID == "" || b.SnapshotID == "" || b.ValidatorProvider == "" || b.ValidatorModel == "" || b.ValidatorRuntimeID == "" || b.ValidatorSessionID == "" || b.EvidenceDigest == "" || b.ArtifactDigest == "" || b.Manifest.Validate() != nil || b.IsolationProfile != workspace.IsolationRepoOnly {
		return ErrInvalid
	}
	return nil
}

// Finding remains retained history even after it becomes stale.
type Finding struct {
	ID, Summary string
	Binding     SnapshotBinding
	Stale       bool
}
type ValidationResult struct {
	ID       string
	Binding  SnapshotBinding
	Findings []Finding
}

func (r ValidationResult) Validate() error {
	if r.ID == "" || r.Binding.Validate() != nil {
		return ErrInvalid
	}
	for _, f := range r.Findings {
		if f.ID == "" || f.Summary == "" || f.Binding.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

// StalenessCheck compares a finding/result binding against current explicit
// identities. Any change is stale; there is no fallback or automatic replay.
type StalenessCheck struct{ Current SnapshotBinding }

func (s StalenessCheck) Stale(binding SnapshotBinding) bool {
	return binding.RepositoryID != s.Current.RepositoryID || binding.Manifest.BaseSHA != s.Current.Manifest.BaseSHA || binding.Manifest.TargetSHA != s.Current.Manifest.TargetSHA || binding.Manifest.TreeHash != s.Current.Manifest.TreeHash || binding.Manifest.IndexHash != s.Current.Manifest.IndexHash || binding.Manifest.UntrackedManifestDigest != s.Current.Manifest.UntrackedManifestDigest || binding.Manifest.DiffDigest != s.Current.Manifest.DiffDigest || binding.SnapshotID != s.Current.SnapshotID || binding.LeaseEpoch != s.Current.LeaseEpoch || binding.ValidatorProvider != s.Current.ValidatorProvider || binding.ValidatorModel != s.Current.ValidatorModel || binding.ValidatorRuntimeID != s.Current.ValidatorRuntimeID || binding.ValidatorSessionID != s.Current.ValidatorSessionID || binding.ValidatorGeneration != s.Current.ValidatorGeneration || binding.ConfigEpoch != s.Current.ConfigEpoch || binding.EvidenceDigest != s.Current.EvidenceDigest || binding.ArtifactDigest != s.Current.ArtifactDigest || binding.IsolationProfile != s.Current.IsolationProfile
}
func (s StalenessCheck) Apply(f Finding) Finding { f.Stale = s.Stale(f.Binding); return f }

// CanAuthorize is intentionally false for stale findings. Callers still need
// their own authority; a fresh validation result alone is never authorization.
func (f Finding) CanAuthorize() error {
	if f.Stale {
		return ErrStale
	}
	return nil
}
