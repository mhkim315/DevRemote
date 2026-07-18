# PA2b — Generic Session Identity Primitive Extraction Evidence Report

Status: **REVIEW REQUEST**

Implementation SHA: `d86959d2a460fa0da23ff8416fc4da63f1e013a3`
Evidence/report SHA: (this commit)
Gate execution SHA: `d86959d2a460fa0da23ff8416fc4da63f1e013a3`

## 1. Ancestry

| Ancestor | SHA | Role |
| --- | --- | --- |
| PA2a final ACCEPT (rollback SHA) | `34b5275bc6be51c6f8de386e311f76e9fc1725a0` | accepted baseline; local==remote verified clean before work began |
| PA2b implementation | `d86959d2a460fa0da23ff8416fc4da63f1e013a3` | this packet |

## 2. Contract mapping (docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md §PA2b)

**Moved to `companion-daemon/internal/sessionid/` (provider-neutral only)**:
`SessionRef`, `ParseSessionID` (split at first `:`), `Canonical`/`String`,
structural `Validate` (non-empty adapter + local id, no control characters),
`ValidateAdapterName` (`[a-z][a-z0-9_-]*`), plus the `ErrInvalidSessionID`
sentinel they wrap. Logic copied byte-identically from the accepted
`internal/mux/id_parser.go`.

**Retained in current owners (verified untouched or unmoved)**:
- `MigrateLegacyID` + `legacyCmuxRe` — `internal/mux/registry.go` (location
  proof in `TestPA2b_MigrateLegacyID_UnchangedAndStaysInMux`)
- Adapter discovery, registry lookup/mutation — `internal/mux/registry.go`
- `validSessionID` / `validAdapterID` — `internal/term/approval_delivery.go`
  (call sessionid primitives internally, as the contract permits)
- Provider-specific identity validation, authority-version validation,
  lifecycle generation authority — unmodified owners

**Legacy compatibility**: `internal/mux/id_parser.go` now contains ONLY a
type alias (`SessionRef = sessionid.SessionRef`) and two one-line delegating
functions, each marked `// Deprecated:` as temporary PA4/PB deletion targets.
Zero parsing logic remains. `mux.ErrInvalidSessionID` aliases the sessionid
sentinel value, so `errors.Is` matches identically through either name.

**Consumers migrated to direct `internal/sessionid` imports** (non-frozen
term production files): `create.go`, `pty.go`, `telemetry.go`,
`telemetry_service.go`, `lifecycle_service.go`, `approval_delivery.go`, and
`managed_catalog.go` under its explicit §5 PA2b import exception
(`managed_catalog.go`, `managed_claude_activation.go`, `approval_delivery.go`
had imported mux solely for identity parsing; the first and last dropped the
mux import entirely).

**Frozen-boundary compliance (§4/§5)**: `managed_claude_activation.go`
(Claude RuntimeOf, §5 frozen, no PA2b exception) was initially migrated,
then REVERTED to its byte-identical accepted state after re-checking §5 —
frozen files keep using the deprecated wrappers (behavior-identical thin
delegates) until PA4/PB. No other §5 file, no `internal/agent/`, no
`mobile/`, and no tmux/cmux/localpty adapter file was modified. No approval
semantic change (§4): bounds regression-tested byte-for-byte.

## 3. Focused tests (contract tests 1–5)

| # | Contract requirement | Test(s) |
| --- | --- | --- |
| 1 | sessionid: canonical positives, malformed, empty adapter/local, control chars, non-canonical serialization, grammar, round trip | `internal/sessionid/sessionid_test.go`: `TestParseSessionID_CanonicalPositive`, `TestValidate_MalformedAndEmpty`, `TestValidate_ControlCharacters`, `TestCanonical_NonCanonicalForms`, `TestValidateAdapterName_Grammar`, `TestParseString_RoundTrip` |
| 2 | mux callers receive identical results through aliases/wrappers | `internal/mux/id_parser_compat_test.go`: `TestPA2b_WrapperCompat_IdenticalResults` (parse/serialize/validate/grammar parity incl. error messages), `TestPA2b_WrapperCompat_SentinelIdentity` (errors.Is both directions), `TestPA2b_WrapperCompat_TypeAlias`; plus the untouched pre-existing mux suites (`adapter_golden_test.go`, `adapter_contract_test.go`, `phase1_test.go`) now exercising the wrappers |
| 3 | MigrateLegacyID unchanged, remains in internal/mux | `TestPA2b_MigrateLegacyID_UnchangedAndStaysInMux` (behavior table incl. `cmux:3` → `cmux:surface:3`, idempotence, non-cmux passthrough; source-location proof both directions) |
| 4 | Approval regression: accepted identities pass, malformed/non-canonical fail, bindings unchanged | `internal/term/approval_identity_pa2b_test.go`: `TestPA2b_ApprovalValidSessionID_BoundsPreserved` (UTF-8, 512-byte boundary both sides, canonical round trip, control chars, grammar), `TestPA2b_ApprovalValidAdapterID_BoundsPreserved` (64-byte boundary both sides); provider-origin / authority-version / adapter / launch-generation binding covered by the unchanged existing suites (`approval_store_gen_test.go`, `approval_delivery_r9a/r9b_test.go`, `managed_approval_test.go`) — all pass in the full gate |
| 5 | Architecture: no mux import solely for parsing; no second parser | `internal/sessionid/arch_gate_test.go`: `TestPA2b_ArchGate_ForbiddenImport`, `TestPA2b_ArchGate_NoDuplicateParser`, `TestPA2b_ArchGate_TermConsumersUseSessionID` (frozen §5 files exempted by exact filename, documented in-test) |

## 4. Exact commands and results (at gate SHA)

```
$ go test ./internal/sessionid ./internal/mux ./internal/term -run "TestPA2b" -count=1
ok  devremote/companion-daemon/internal/sessionid
ok  devremote/companion-daemon/internal/mux
ok  devremote/companion-daemon/internal/term
    (9 PA2b tests: 3 arch gates, 3 wrapper-compat, 1 migration, 2 approval — all PASS)

$ go test -race ./internal/sessionid ./internal/mux \
    -run "TestPA2b|TestParse|TestValidate|TestCanonical" -count=20
ok  devremote/companion-daemon/internal/sessionid  1.434s   (20/20, race-clean)
ok  devremote/companion-daemon/internal/mux        1.556s   (20/20, race-clean)

$ go test -race ./... -count=1
exit 0, 12 packages ok (sessionid is the 12th), 0 failures
```

## 5. Gate results

| Gate | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| Focused PA2b suites `-count=1` and `-count=20 -race` | PASS |
| `go test -race ./... -count=1` (full backend) | PASS (exit 0, 12 ok) |
| `gofmt -l` (all changed + new files) | CLEAN (0 files) |
| `git diff --check` | PASS |
| Secret scan (changed + new files) | CLEAN |
| PA2a zero-production-reference regression (contract rg pattern) | 0 matches |
| PA2b static duplicate-parser gate (shell: colon-`SplitN` outside sessionid, non-test) | 0 matches |
| PA2b forbidden-import gate (import blocks of `internal/sessionid/*.go`; authoritative go/parser form in `TestPA2b_ArchGate_ForbiddenImport`) | CLEAN |
| Invariant: vendor branch scan (`mobile/src`) | CLEAN |
| Invariant: ID inference scan (`mobile/src`) | CLEAN |
| Mobile `npx tsc --noEmit` | **not-run** — zero `mobile/` source changes in this packet (verified `git diff --name-only HEAD -- mobile/` empty) and `mobile/node_modules` not installed; per CLAUDE.md the backend gate ran and mobile is reported not-run with reason |

## 6. Final state

| Condition | Value |
| --- | --- |
| Implementation commit | `d86959d2a460fa0da23ff8416fc4da63f1e013a3` (14 files: 5 new, 9 modified) |
| Evidence/report commit | this commit (docs only) |
| Worktree at push | clean |
| Local == Remote after push | verified in worker_done |
| Rollback SHA (per contract) | `34b5275bc6be51c6f8de386e311f76e9fc1725a0` (PA2a final ACCEPT) |
