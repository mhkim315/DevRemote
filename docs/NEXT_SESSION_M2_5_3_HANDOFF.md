# Next Session Handoff — M2.5-3 Device Challenge Authentication

Status: implementation may begin only after M2.5-2 receives verifier ACCEPT

Read first:

- `docs/M2_5_DEVICE_TRUST_SECURITY_PLAN.md`
- `docs/M2_5_3_DEVICE_CHALLENGE_AUTH_PLAN.md`
- `docs/NEXT_SESSION_M2_5_1_HANDOFF.md`
- `companion-daemon/internal/devicetrust/identity.go`
- `companion-daemon/internal/devicetrust/registry.go`

## Mission

Implement daemon-owned P-256 device challenge authentication and an in-memory
short-session manager. This phase establishes a `Principal` boundary for M2.5-4;
it does not migrate all REST/WebSocket handlers yet.

## Must implement

- versioned start/verify endpoints and canonical transcript helpers;
- host signature, then device signature verification;
- single-use bounded-expiry challenge store;
- opaque 256-bit token creation, digest-only storage, expiry and revoke;
- `Principal` creation independent of HTTP handlers;
- secret-safe errors/logging and concurrency-safe race behavior.

## Must not implement

- REST or WebSocket middleware migration;
- mobile UI/Keystore work;
- `dev-token` fallback for remote traffic;
- E2EE, refresh tokens, account login, or persistent token storage;
- changes to Recorder, controlled PTY, lifecycle, or Transcript.

## Required proof

Run the exact product-boundary and race tests listed in the plan. In the final
report include a transcript field list, challenge/session storage lifetime,
replay/revoke behavior, and confirmation that raw tokens/nonces/signatures are
absent from logs and DTOs after issuance.

## Verifier prompt

```text
<commit> 기준 M2.5-3을 검토해줘. 확인: (1) QR로 pin된 host key의
host proof와 paired device의 P-256 proof가 canonical domain-separated
transcript에 묶이는지, (2) challenge consume/verify/token issuance가 동시성에서
최대 한 번인지, (3) token은 256-bit opaque·digest-only·host/device/boot-bound이며
만료·revoke·restart에 무효화되는지, (4) Principal이 handler-independent인지,
(5) token/nonce/signature가 DTO·로그·오류에 새지 않는지. REST/WS middleware와
Android UI는 M2.5-4 이후 범위다.
```
