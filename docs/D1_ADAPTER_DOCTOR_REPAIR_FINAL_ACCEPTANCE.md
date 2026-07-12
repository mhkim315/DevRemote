# D1 Adapter Doctor/Repair — Final Acceptance

Status: **ACCEPT**

Accepted implementation:

```text
8f7c22def81abf0b932f6dbbacc07325ae2bb12e
```

Accepted ancestry:

```text
R2  6f940b03bbb451dd0ddcfc6bf7ce5f86bbec3b50
T2  ef4a162c7f9a5644fd52d89501e97f4e62301dfa
T1  162266f830caaf07bf701d9a1294557432855769
T0  3ce2604bd333dcb63142b5b1710185a823162efa
```

## Accepted boundary

D1 now provides a fail-closed Adapter Doctor/Repair workflow:

```text
compatibility/drift assessment
→ bounded and redacted evidence
→ one provider/version-specific patch
→ hostile-patch validation
→ isolated reproducible candidate workspace
→ Pokit-owned fixed suite
→ immutable evidence-bound review bundle
→ explicit digest-bound user decision
```

The accepted implementation preserves the frozen T0 contract and accepted T1/T2
adapters. Candidate patches cannot modify Pokit-owned conformance tests, common
contracts, another adapter, Terminal, Recorder, authentication, lifecycle, mobile,
or activation code. Fixture admission is provider/version/digest/source-kind bound,
and suite evidence is bound to the authoritative fixed-command manifest rather
than caller-supplied labels.

Activation remains deliberately unsupported and fail closed. A production repair
runner also remains unavailable unless a separately reviewed secure runner is
provided. D1 does not claim automatic repair activation or unrestricted coding-agent
execution.

## Independent evidence

Independent verification of the accepted tip confirmed:

- `TestFullWorkflow_CompleteVertical` passes through candidate build,
  conformance, review bundle, approval/rejection lifecycle, and fail-closed
  activation;
- `TestE2E_ManifestsInBundleCorrect` passes;
- partial, duplicate, extra, skipped, failed, zero-test, nonzero-exit, and
  aggregate-inconsistent suite evidence is rejected;
- unknown/traversal provider names and empty/malformed/traversal versions are
  rejected by the same authoritative validation used by the sandbox;
- patch, fixture, manifest, evidence, workspace, command-suite, and approval
  digests are checked at their trust boundaries;
- secret/path leakage scanning and admitted-fixture provenance checks fail closed;
- accepted T0/T1/T2 code and wire contracts remain unchanged.

The focused D1 verification left no `d1-workspace-*` residue and no runaway test
process. Full repository gates had also passed on the accepted tree.

## Explicitly deferred

- secure production invocation of a local coding agent;
- activation/rollback implementation beyond the accepted fail-closed boundary;
- T3 Transcript integration, S1 Status, A1 Approval UX, and O1 Orchestrator;
- Windows runtime implementation and Windows-specific abstractions.

D1 is complete. The next authorized implementation stage is T3, governed only by
`docs/NEXT_SESSION_T3_TRANSCRIPT_INTEGRATION_HANDOFF.md`.
