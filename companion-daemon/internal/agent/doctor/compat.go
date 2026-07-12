package doctor

import (
	"strings"

	"devremote/companion-daemon/internal/agent/contract"
)

// CheckCompatibility compares the observed agent version against an adapter's
// declared SupportedVersions and ContractVersion. It is a pure function — no
// I/O, no side effects, no harness dependency.
//
// Logic:
//  1. Empty observed version → CompatUnknownShape, summary "no version observed".
//  2. Contract version mismatch → CompatVersionDrift.
//  3. Observed version not in SupportedVersions → CompatVersionDrift.
//  4. Exact match → CompatOK.
func (d *Doctor) CheckCompatibility(
	desc contract.AgentAdapterDescriptor,
	observedVersion string,
	observedVersionSource string,
) CompatibilityReport {
	report := CompatibilityReport{
		AdapterName:       desc.Name,
		Provider:          desc.Provider,
		ContractVersion:   desc.ContractVersion,
		SupportedVersions: copyStrings(desc.SupportedVersions),
		ObservedVersion:   contract.SanitizeDiagnostic(observedVersion),
		DriftEvidence: DriftEvidence{
			ObservedVersion:    contract.SanitizeDiagnostic(observedVersion),
			AgentVersionSource: contract.SanitizeDiagnostic(observedVersionSource),
		},
	}

	// Empty observed version → unknown shape.
	if observedVersion == "" {
		report.Compatible = false
		report.DriftCode = CompatUnknownShape
		report.DriftSummary = "no version observed in agent records"
		return report
	}

	// Contract version mismatch.
	if desc.ContractVersion != contract.ContractVersion {
		report.Compatible = false
		report.DriftCode = CompatVersionDrift
		report.DriftSummary = "contract version mismatch: adapter " +
			desc.ContractVersion + " vs current " + contract.ContractVersion
		return report
	}

	// Exact match in SupportedVersions.
	for _, sv := range desc.SupportedVersions {
		if observedVersion == sv {
			report.Compatible = true
			report.DriftCode = CompatOK
			report.DriftSummary = "version " + contract.SanitizeDiagnostic(observedVersion) + " exactly matched"
			return report
		}
	}

	// Observed version not in supported set.
	report.Compatible = false
	report.DriftCode = CompatVersionDrift
	report.DriftSummary = "observed version " + contract.SanitizeDiagnostic(observedVersion) +
		" not in supported versions [" + strings.Join(boundedVersions(desc.SupportedVersions), ", ") + "]"
	return report
}

// boundedVersions caps the number of version strings in a diagnostic message.
func boundedVersions(versions []string) []string {
	if len(versions) <= 8 {
		return versions
	}
	return versions[:8]
}

// copyStrings returns a shallow copy of a string slice, nil-safe.
func copyStrings(src []string) []string {
	if src == nil {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}
