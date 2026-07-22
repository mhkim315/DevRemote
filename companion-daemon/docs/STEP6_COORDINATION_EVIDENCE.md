# STEP6 Evidence — Inter-Session Coordination Envelope and Broker

Implementation commit: `dfefef7b233274b40f1513e8bc5cb26c78c7d088`

STEP6 R2 implementation commit: `abec8fbd4be6787a5e819775bdbf600cbb09d522`

R2 adds closed `question`, `finding`, and `revision_request` message types;
requires the explicit `requiredTargetCapability` authorization binding; bounds
handoff/evidence/reply/causation references; and bounds every handoff list to
64 non-empty 512-byte items. Oversized values are rejected before broker entry.

`internal/coordination` is a pure, in-memory broker-owned delivery ledger. It
defines closed message types and delivery states, exact source/target runtime
identity, repository/snapshot provenance, bounded redacted content/evidence
references, expiry, reply/causation linkage, and bounded handoff metadata.

The broker never selects a provider, dispatches a validator, synthesizes a
result, or retries/replays delivery. `delivery_accepted` transitions do not
represent model understanding; expiry and delivery-unknown remain explicit.
Timeline has no delivery authority or import. Redacted summaries reject bearer
token markers, and the envelope stores no transcript, reasoning, pairing secret,
approval payload, environment dump, or unlimited tool output.

Tests cover closed transitions/no replay, runtime acknowledgement, expiry, and
secret-bearing summary rejection under `-race`.
