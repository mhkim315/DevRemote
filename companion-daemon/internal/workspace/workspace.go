// Package workspace defines pure, cooperative workspace identity, clean
// snapshot, and write-lease contracts. It does not run Git, take filesystem
// locks, or block external editors and shells.
package workspace

import (
	"errors"
	"fmt"
	"time"
)

const MaxIDBytes = 512

var (
	ErrInvalidIdentity = errors.New("workspace: invalid identity")
	ErrDirtySnapshot   = errors.New("workspace: dirty-worktree validation is deferred")
)

type Mode string

const (
	ModeSharedSequential Mode = "shared_sequential"
	ModeFrozenValidation Mode = "frozen_validation"
)

type IsolationProfile string

const (
	IsolationRepoOnly      IsolationProfile = "repo-only"
	IsolationRepoProcess   IsolationProfile = "repo+process"
	IsolationRepoNetwork   IsolationProfile = "repo+network-policy"
	IsolationContainerized IsolationProfile = "containerized"
	IsolationVM            IsolationProfile = "VM-isolated"
)

// Identity names one repository workspace and the immutable snapshot it is
// presently bound to. The values are supplied by a future authoritative
// workspace controller; this package never queries Git itself.
type Identity struct {
	RepositoryID     string           `json:"repositoryId"`
	Mode             Mode             `json:"mode"`
	BaseSHA          string           `json:"baseSha"`
	CurrentSHA       string           `json:"currentSha"`
	TreeHash         string           `json:"treeHash"`
	SnapshotID       string           `json:"snapshotId"`
	IsolationProfile IsolationProfile `json:"isolationProfile"`
}

func (i Identity) Validate() error {
	if !valid(i.RepositoryID) || !valid(i.BaseSHA) || !valid(i.CurrentSHA) || !valid(i.TreeHash) || !valid(i.SnapshotID) {
		return ErrInvalidIdentity
	}
	switch i.Mode {
	case ModeSharedSequential, ModeFrozenValidation:
	default:
		return ErrInvalidIdentity
	}
	// Only repository-level isolation is currently implemented. The other
	// roadmap vocabulary values remain named for future explicit enforcement,
	// but accepting one today would overpromise isolation we do not provide.
	if i.IsolationProfile != IsolationRepoOnly {
		return ErrInvalidIdentity
	}
	return nil
}

// SnapshotManifest is a clean committed-snapshot claim. Dirty worktrees are
// deliberately rejected until a separately authorized representation exists.
type SnapshotManifest struct {
	BaseSHA                 string `json:"baseSha"`
	TargetSHA               string `json:"targetSha"`
	TreeHash                string `json:"treeHash"`
	IndexHash               string `json:"indexHash"`
	UntrackedManifestDigest string `json:"untrackedManifestDigest"`
	DiffDigest              string `json:"diffDigest"`
	Clean                   bool   `json:"clean"`
}

func (m SnapshotManifest) Validate() error {
	if !m.Clean {
		return ErrDirtySnapshot
	}
	for _, value := range []string{m.BaseSHA, m.TargetSHA, m.TreeHash, m.IndexHash, m.UntrackedManifestDigest, m.DiffDigest} {
		if !valid(value) {
			return ErrInvalidIdentity
		}
	}
	return nil
}

// FindingBinding lets a validator mark its result stale when the repository
// no longer matches the exact immutable snapshot it inspected.
type FindingBinding struct {
	RepositoryID string `json:"repositoryId"`
	SnapshotID   string `json:"snapshotId"`
	TreeHash     string `json:"treeHash"`
}

func (b FindingBinding) Stale(current Identity) bool {
	return b.RepositoryID != current.RepositoryID || b.SnapshotID != current.SnapshotID || b.TreeHash != current.TreeHash
}

func valid(value string) bool { return value != "" && len(value) <= MaxIDBytes }

func validateTTL(ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("%w: non-positive lease ttl", ErrInvalidIdentity)
	}
	return nil
}
