# A1.2 C3D-B R4 — Final Negative Evidence Contract Note

Status: **PRE-IMPLEMENTATION — TEST-ONLY PACKET**

Parent: `docs/NEXT_EXECUTOR_A1_2_C3D_B_R4_HANDOFF.md`
Rejected R3 HEAD: `df5186a8db20e2c9c253bb19a37350a44ca73d07`
Accepted C3D-B plan: `a3f9cc961cbe86eb628b205d0ecc1d363deea3f0`

## Authority owner

- Paired-device middleware: `devicetrust.RequirePrincipal(sessionMgr, h.HandleApprovalAction, devicetrust.PermTerminalInput)` on the registered
  `POST /api/sessions/{id}/approvals/{approvalId}` route (`cmd/devremote/app.go`).
  A bearer resolves to a Principal ONLY through the App-owned
  `DeviceSessionManager` of the exact daemon incarnation that minted it.
- Canonical approval authority: the App-owned `AuthoritativeApprovalStore`.
  Records exist ONLY via the certified ingestion paths
  (`managed_claude.go` hook bridge, `approval_ingest.go` accepted-adapter
  DetectApproval). The runtime status layer never writes to it.

## Immutable binding

A claim commits only under the exact tuple: canonical session ID, approval ID,
runtime generation (LaunchGen/StreamGen), server-derived requester context
(DeviceID, HostID, BearerSessionID, BootID, stored permissions), and the
catalog action digest. No client field can substitute for any element.

## Success evidence

- **R4-A (foreign-host / old-boot)**: a real POST to the target App's
  registered approval route bearing (i) a bearer minted by a genuinely
  different App/host fixture, and (ii) a bearer minted by a prior
  boot/session-manager incarnation of the SAME host identity and device
  registry, is rejected 401/403 at the middleware — before claim — leaving
  the pending record byte-identical and producing zero resume/provider
  delivery (launcher launch count, coordinator identity/entry counts
  unchanged).
- **R4-B (status/approval separation)**: an actual `waiting_approval`
  runtime status produced through the current production status path
  (`TelemetryService.processSession` over a real Claude native log whose
  final record is the `permission-mode: ask` observation) projects
  `agentStatus: "waiting_approval"` while the canonical
  `AuthoritativeApprovalStore` holds zero records, `ListSafe` is empty, the
  serialized session DTO carries no `approvals` key, a fully-authorized
  claim on a fabricated ApprovalID cannot resolve (`not_found`), and the
  activated runtime delivery endpoint holds zero queued items/bytes.

## Negative controls

- For each R4-A negative, a current-host/current-boot bearer for the same
  route reaches the handler and returns the expected 409
  (`not_actionable`) against a non-actionable record — proving the
  negative's rejection is the middleware's authority decision, not a
  broken route — without entering the 120-second delivery path.
- R4-B claims with a fabricated ApprovalID under a FULL permission set —
  proving resolution fails on record absence, not on permission shortfall.

## Non-goals

- No production backend or mobile code change.
- No C3D-C work.
- No live Claude provider invocation.
