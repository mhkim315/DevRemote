# STEP7 Evidence — Frozen Clean-Snapshot Independent Validation

Implementation commit: `e1cbe7211e57f9f986306153afb8c247a22a5aed`

STEP7 R2 implementation commit: `a753e126c81ad4fc2d52fb39c7ae7ed419290e8c`

R2 removes caller-controlled stale authorization. `CanAuthorize` now requires
the current binding and recomputes staleness at authorization time, so omitted
`Apply` and post-check drift fail closed. Invalid bindings are stale, and every
finding binding must exactly equal its ValidationResult binding.

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
