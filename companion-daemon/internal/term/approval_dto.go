package term

import "time"

// A1 remediation (B6) — bounded, redacted public approval DTO.
//
// The external (mobile-facing) approval projection is a strict structural allowlist.
// It NEVER carries a raw provider prompt, terminal output, provider/delivery
// payload, full command, token/secret, absolute path, arbitrary provider label or
// placeholder, internal claim token, or ActionDigest source material. Display text
// is Pokit-owned and bounded; delivery payloads stay server-side. Backend and mobile
// enforce the same closed shape and byte bounds.

const (
	safeSummaryMaxLen     = 200
	safeLabelMaxLen       = 64
	safePlaceholderMaxLen = 128
	safeMaxOptions        = 8
)

// SafeOptionDTO is the redacted projection of one actionable option. It exposes a
// safe option ID, a Pokit-owned bounded label, the closed semantic kind, and
// bounded required-input metadata only — never a payload or an arbitrary provider
// label.
type SafeOptionDTO struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Kind             string `json:"kind"`
	RequiresInput    bool   `json:"requiresInput"`
	InputPlaceholder string `json:"inputPlaceholder,omitempty"`
}

// SafeApprovalDTO is the bounded public approval projection.
type SafeApprovalDTO struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionId"`
	Summary    string          `json:"summary"`
	State      string          `json:"state"`
	Actionable bool            `json:"actionable"`
	Options    []SafeOptionDTO `json:"options"`
	CreatedAt  string          `json:"createdAt"`
	ExpiresAt  string          `json:"expiresAt"`
}

// pokitApprovalSummary is the Pokit-owned, bounded, provider-neutral summary. It is
// derived from the accepted provider identity ONLY — never from the raw provider
// prompt/message — so no untrusted provider text can leak or forge display.
func pokitApprovalSummary(provider string) string {
	switch provider {
	case "codex", "claude":
		return "Agent requested an approval"
	default:
		return "Approval requested"
	}
}

// safeOptionLabel maps a closed semantic kind to a Pokit-owned display label.
// Arbitrary provider labels are never passed through.
func safeOptionLabel(kind string) string {
	switch kind {
	case "approve":
		return "Approve"
	case "reject":
		return "Reject"
	case "cancel":
		return "Cancel"
	case "open":
		return "Open Terminal"
	default:
		return "Respond"
	}
}

// projectSafeApproval builds the bounded redacted DTO from an internal record.
// Options are projected ONLY for an actionable record; a non-actionable record
// (no proven action mapping) exposes an empty option set and Actionable=false, so
// the client renders it as non-actionable intervention information with no buttons.
func projectSafeApproval(rec *approvalRecord) SafeApprovalDTO {
	dto := SafeApprovalDTO{
		ID:         boundStr(rec.approval.ID, authMaxApprovalIDLen),
		SessionID:  boundStr(rec.approval.SessionID, maxSessionIDLen),
		Summary:    boundStr(pokitApprovalSummary(rec.provider), safeSummaryMaxLen),
		State:      projectPublicStatus(rec.state),
		Actionable: rec.actionable,
		Options:    []SafeOptionDTO{},
		CreatedAt:  rec.createdAt.UTC().Format(time.RFC3339),
		ExpiresAt:  rec.expiresAt.UTC().Format(time.RFC3339),
	}
	if !rec.actionable {
		return dto
	}
	n := 0
	for i := range rec.approval.Options {
		if n >= safeMaxOptions {
			break
		}
		o := rec.approval.Options[i]
		so := SafeOptionDTO{
			ID:            boundStr(o.ID, safeLabelMaxLen),
			Label:         boundStr(safeOptionLabel(o.Kind), safeLabelMaxLen),
			Kind:          o.Kind,
			RequiresInput: o.Input != nil && o.Input.Required,
		}
		if o.Input != nil && o.Input.Required {
			so.InputPlaceholder = boundStr("Enter input", safePlaceholderMaxLen)
		}
		dto.Options = append(dto.Options, so)
		n++
	}
	return dto
}
