# STEP7 Evidence — Frozen Clean-Snapshot Independent Validation

Implementation commit: `e1cbe7211e57f9f986306153afb8c247a22a5aed`

`internal/validation` binds results/findings to clean committed snapshot
identity (repository, base/target SHA, tree/index/untracked/diff digests,
snapshot, lease epoch), validator provider/model/runtime/session/generation,
config epoch, evidence/artifact digests, and the only implemented `repo-only`
isolation profile. Dirty manifests are rejected by the existing workspace
contract; no executor reasoning, auto-dispatch, mobile, or terminal wiring was
added.

`StalenessCheck` makes any identity difference stale. Stale findings remain
objects for history but `CanAuthorize` returns `ErrStale`; a finding cannot
authorize acceptance, merge, or lifecycle behavior. Tests cover changed SHA,
lease epoch, validator generation, config epoch, evidence/artifact identity,
binding, and stale authorization prevention. The app flag is default-off.
