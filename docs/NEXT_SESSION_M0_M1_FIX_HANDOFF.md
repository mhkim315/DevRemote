# Next Session Handoff — M0/M1 Correction After e4e2704d0

Status: REJECT correction handoff
Branch: `feature/phase10-multi-adapter`
Rejected commit: `e4e2704d0`

Read first:

- `docs/M0_M1_E4E2704_REVIEW.md`
- `docs/MOBILE_SESSION_LIFECYCLE_AND_TRANSCRIPT_PLAN.md`
- `docs/SESSION_OWNERSHIP_AND_LOCAL_HOST_CONTRACT.md` (M1.5 preview only)

## Mission

Fix the three M0/M1 product blockers. Do not start M1.5 implementation, M2,
mobile UI, or Transcript work in the same correction commit.

## Required corrections

### 1. Privileged local launch boundary

- do not use `InsecureLocalOnly` or loopback address as caller-local proof;
- Cloudflare-to-localhost must not gain arbitrary command execution;
- preset-only HTTP create remains allowed under normal authenticated policy;
- custom argv and legacy command-string HTTP shapes must not bypass the policy;
- preserve `pokit run <arbitrary command>` through a privileged local channel,
  preferably the existing `0600` Unix socket;
- add public-boundary bypass tests.

### 2. Recorder readiness

- make session lookup/OpenStream/EnsureRecorder readiness observable;
- return `running` only when Recorder is alive;
- cleanup the just-created runtime if startup readiness fails;
- do not expose an unrecorded live process;
- remove the phantom starter subscriber or unsubscribe it explicitly;
- add OpenStream-failure and cleanup tests.

### 3. Strict legacy decoding

- reject malformed, missing, null, object, or numeric legacy command values;
- never convert invalid input into a default shell session;
- retain valid CLI compatibility only through the privileged local boundary.

## Preserve

- daemon-owned Shell/Codex/Claude profiles;
- direct executable + argv path;
- daemon canonical ID generation;
- exact CWD propagation and validation;
- lifecycle DTO/types;
- Recorder single-reader invariant;
- tmux/cmux adapter behavior;
- raw terminal input non-storage.

## Required tests

- remote/tunneled HTTP cannot use custom profile;
- remote/tunneled HTTP cannot fall through to legacy command execution;
- preset HTTP profile creation works under the intended auth policy;
- privileged local CLI arbitrary command still works;
- invalid legacy command JSON creates no session;
- OpenStream failure never returns running and terminates the created runtime;
- successful create has one alive Recorder and no retained phantom viewer;
- targeted `-race` suite passes.

## Stop condition

Commit and push only the M0/M1 correction. Report the new hash for verifier
review. M1.5 begins only after explicit M0/M1 ACCEPT.
