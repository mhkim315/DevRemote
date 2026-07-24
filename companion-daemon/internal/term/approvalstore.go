package term

import "devremote/companion-daemon/internal/devicetrust"

// A1-C — the legacy in-memory approval store is retired. Production approval
// authority is now the generation-bound AuthoritativeApprovalStore
// (approval_store_gen.go), fed exclusively from accepted-adapter DetectApproval
// (approval_ingest.go). NewApprovalStore is kept as a thin constructor alias so
// existing composition/test call sites build against the authoritative store
// without change; it no longer returns a parser-fed, generation-blind store.
func NewApprovalStore(authorizer devicetrust.MutationAuthorizer) (*AuthoritativeApprovalStore, error) {
	return NewAuthoritativeApprovalStore(authorizer)
}
