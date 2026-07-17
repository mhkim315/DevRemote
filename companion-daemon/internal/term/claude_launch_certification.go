// Package term — C3D §12: per-incarnation Claude launch-certification tuple.
//
// The frozen extension plan (§4) requires, before actionability, a
// platform-specific spawned-process identity or an equivalent non-reusable
// launch binding. The accepted C3D contract note (§12) freezes the latter as
// a per-incarnation certification tuple built from identity the daemon
// already owns: the exact pre-exec artifact certification plus the
// launcher-derived incarnation identity (OpaqueID/PID/spawn time) plus the
// POKIT session/epoch allocated for THIS incarnation.
//
// The tuple is produced at EVERY spawn site (CreateDetached AND every
// ResumeForApproval) and is keyed by (PokitSessionID, Epoch, ProcessID), so
// a tuple from one incarnation can never certify another incarnation,
// session, or epoch. It is explicitly NOT process-image attestation and is
// never called that.
//
// OS-neutral seam: the observable contract is exactly
// {OS, arch, attestor kind/version, opaque artifact identity, opaque launch
// identity, certification result/reason}. The macOS specifics (--version
// probe, realpath pinning, file SHA-256) stay inside the attestor/launcher
// implementations (claude_attestor.go); a future Windows implementation
// supplies the same observable tuple through this same seam.
package term

import (
	goruntime "runtime"
	"time"
)

// claudeLaunchAttestorKind is the compiled versioned identity of the Claude
// pre-launch attestor implementation. It is part of the certification tuple
// and the closed platform contract; changing the attestor semantics requires
// a new kind/version and a contract review.
const claudeLaunchAttestorKind = "pokit.claude.prelaunch.v1"

// Closed certification-result vocabulary.
const (
	claudeCertCertified = "certified"
	claudeCertFailed    = "failed"
)

// certifiedClaudePlatform is one closed OS/architecture tuple entry.
type certifiedClaudePlatform struct {
	OS   string
	Arch string
}

// certifiedClaudePlatforms is the compiled closed set of C0D-certified
// platforms. The single entry is the exact tuple the C0D evidence was
// produced on. Adding an entry requires a separate contract review (same
// rule as adding a catalog entry).
var certifiedClaudePlatforms = []certifiedClaudePlatform{
	{OS: "darwin", Arch: "arm64"},
}

// launchOS / launchArch are the process's build platform, captured once.
// They are package variables ONLY as a deterministic-test seam (the same
// accepted pattern as clockNow/entropyReader); production never mutates them.
var (
	launchOS   = goruntime.GOOS
	launchArch = goruntime.GOARCH
)

// claudePlatformCertified reports whether the OS/arch pair is a member of
// the compiled closed platform set.
func claudePlatformCertified(os, arch string) bool {
	for _, p := range certifiedClaudePlatforms {
		if p.OS == os && p.Arch == arch {
			return true
		}
	}
	return false
}

// ClaudeLaunchCertification is the per-incarnation launch binding (C3D §12).
// Exported for composition tests; never projected into any DTO.
type ClaudeLaunchCertification struct {
	AttestorKind    string // compiled attestor kind/version
	OS              string
	Arch            string
	ArtifactVersion string // pinned provider version
	ArtifactDigest  string // pinned SHA-256, verified pre-exec by Certify
	ProcessID       string // opaque launch identity (launcher OpaqueID)
	PID             int
	SpawnedAt       time.Time
	PokitSessionID  string
	Epoch           int64
	Result          string // claudeCertCertified | claudeCertFailed
	Reason          string // exact failure reason; empty when certified
}

// buildClaudeLaunchCertification produces the tuple for one just-spawned
// incarnation. The artifact digest/realpath/version verification itself
// already ran fail-closed in ClaudeAttestor.Certify immediately before exec;
// this binds that certified artifact identity to the concrete incarnation
// and evaluates the closed platform membership. Any non-certified result
// must fail the actionable create/resume closed at the call site.
func buildClaudeLaunchCertification(cfg ClaudeEntryConfig, proc ManagedProcess, pokitSessionID string, epoch int64, at time.Time) ClaudeLaunchCertification {
	c := ClaudeLaunchCertification{
		AttestorKind:    claudeLaunchAttestorKind,
		OS:              launchOS,
		Arch:            launchArch,
		ArtifactVersion: cfg.Version,
		ArtifactDigest:  cfg.PinnedDigest,
		ProcessID:       proc.OpaqueID(),
		PID:             proc.PID(),
		SpawnedAt:       at,
		PokitSessionID:  pokitSessionID,
		Epoch:           epoch,
	}
	switch {
	case !claudePlatformCertified(launchOS, launchArch):
		c.Result, c.Reason = claudeCertFailed, "platform not certified"
	case len(cfg.PinnedDigest) != 64 || !allHex(cfg.PinnedDigest):
		c.Result, c.Reason = claudeCertFailed, "pinned digest not configured"
	case cfg.Version != certifiedClaudeAuthorityVersion:
		c.Result, c.Reason = claudeCertFailed, "version not certified"
	default:
		c.Result = claudeCertCertified
	}
	return c
}
