# PB-DG-R4 Device-Gate Closeout Plan

**Status:** AUTHORITATIVE PRECONDITION — IMPLEMENTATION PENDING

**Planning baseline:** `45d2433dce8b59a025bf3adf7563a7e5fc37d747`

**PB ACCEPT SHA:** **UNSET**

This micro-packet is the sole execution priority before final PB acceptance or
any post-PB Canonical Timeline work. It closes the Pairing V1 redirect boundary,
binds the TERM-C1-R3 repair to reproducible artifacts, and replaces the stale
pre-R3 device candidate. Earlier device observations remain diagnostic history;
they cannot be spliced into the replacement candidate's acceptance evidence.

## 1. Frozen order and stop gate

```text
PB-DG-R4.1 complete Pairing V1 redirect rejection
-> PB-DG-R4.2 add focused automated regression tests
-> PB-DG-R4.3 regenerate full automated PB evidence
-> freeze exact PB_DEVICE_CANDIDATE_SHA
-> build daemon and release APK from that same candidate
-> PB-DG-R4.4 SM-S926N bounded smoke
-> evidence-only closeout commit
-> independent read-only PB verification
-> exact PB ACCEPT SHA, only on ACCEPT
-> CT-P1 operational-evidence amendment review
```

The executor must stop after pushing the implementation and evidence commits.
It must not assign `PB_ACCEPT_SHA`, begin CT-P2, add Timeline production wiring,
or start later roadmap work. Only an independent verifier may release this gate.

## 2. PB-DG-R4.1 — Pairing V1 transport closure

Required production behavior:

- retain release cleartext support because Pairing V1's one-time QR transport
  is an exact private-LAN HTTP endpoint;
- require `http`, an RFC1918 IPv4 literal, explicit port, no credentials,
  query, or fragment, and pathname exactly `/pair`;
- reject redirects for `/pair`, `/pair/confirm`, and `/pair/result`;
- derive confirm/result URLs from the already validated origin only;
- never send a bearer token, operational request, terminal traffic, approval,
  input, transcript, or session API request over the LAN HTTP pairing origin;
- require the persisted operational origin to remain HTTPS/WSS;
- preserve bootstrap-token single use, expiry, host-key fingerprint/proof,
  device-key proof, and operator approval.

Release-wide Android cleartext permission is an acknowledged Pairing V1
platform limitation, not a claim that arbitrary cleartext traffic is safe.
Pairing V2/TLS removal of that limitation remains a separate post-device packet.

## 3. PB-DG-R4.2 — Mandatory tests

Automated tests must prove:

- exact `/pair` is accepted;
- `/pair/`, another path, encoded path ambiguity, credentials, query, fragment,
  public IP, hostname, and missing port are rejected before network I/O;
- phase 1, confirm, and result-poll redirects are rejected and cannot produce an
  approved result;
- all derived pairing requests retain the exact validated origin;
- operational origin validation remains HTTPS-only;
- no bearer-bearing request can target the cleartext pairing origin;
- queryless paired WebView bootstrap selects the injected exact session,
  forwards a valid daemon hello once, and still rejects mismatched
  session/generation/connection identity;
- the explicit-local-development query fallback and viewer read-only behavior
  remain intact.

Static string-presence checks may supplement but do not replace behavioral
queryless WebView and redirect tests.

## 4. PB-DG-R4.3 — Candidate and automated evidence

After the last production/test change, freeze an exact implementation commit as
`PB_DEVICE_CANDIDATE_SHA`. Build both artifacts from a clean checkout of that
exact SHA and record:

- full candidate SHA, ancestry, branch, and clean worktree;
- daemon embedded VCS revision/modified state and SHA-256;
- release APK SHA-256 and bundled-JS proof;
- build, vet, full race, TypeScript, Jest, formatting, and `git diff --check`;
- release manifest Pairing V1 cleartext exception;
- negative proof that post-pairing operational origins remain HTTPS/WSS and
  production local-test/no-login bypasses remain disabled.

The evidence HEAD is distinct from the production candidate. Evidence commits
must not be used as the daemon/APK build identity.

## 5. PB-DG-R4.4 — Bounded SM-S926N smoke

Use only the matched daemon/APK artifacts from the new candidate and record:

1. LAN HTTP QR pairing reaches explicit operator approval;
2. Android Keystore identity and DeviceAuth verification succeed;
3. the app switches to the configured HTTPS/WSS operational origin;
4. the exact managed shell session is `running` and input-capable;
5. the TERM-C1 hello reaches React Native with `deviceCanInput=true`;
6. one command receives acknowledged delivery and expected PTY output;
7. Ctrl+C or one control macro is delivered;
8. no cleartext post-pairing bearer/session/terminal request is observed.

Any artifact rebuild, candidate change, identity mismatch, pairing redirect,
HTTP operational fallback, missing hello, or input-delivery regression invalidates
the smoke and restarts the candidate/evidence sequence.

## 6. Independent verification handoff

The executor must provide:

- exact implementation and evidence SHAs;
- exact `PB_DEVICE_CANDIDATE_SHA`;
- daemon/APK SHA-256 and provenance;
- automated gate transcript;
- SM-S926N smoke matrix;
- local/upstream equality and clean-worktree proof;
- `PB_ACCEPT_SHA: UNSET`.

The verifier checks code, tests, evidence, artifact identity, and device results.
`PB_ACCEPT_SHA` may be assigned only in the independently recorded ACCEPT. A
REJECT returns only this packet for remediation; it does not authorize CT-P2 or
any other later work.
