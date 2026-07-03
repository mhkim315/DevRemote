# POKIT Security Architecture Audit Request

## Overview
This document requests a formal security audit from the Verification Agent regarding the recent Phase 5 implementation of Supabase Authentication for the POKIT (formerly DevRemote) project.

## Scope of Audit
The current implementation spans two main components:
1. **Mobile App (Client)**: `mobile/src/screens/AuthScreen.tsx`, `mobile/App.tsx`, `mobile/src/lib/supabase.ts`
2. **Go Daemon (Host)**: `companion-daemon/internal/term/pty.go`

## Implementation Summary
- The mobile app uses `@supabase/supabase-js` to authenticate users via Email/Password.
- Upon successful login, the app retrieves a JWT access token from Supabase.
- The app passes this token as a URL query parameter (`?token=...`) when connecting to the Cloudflare tunnel WebSocket endpoint.
- The Go daemon (`pty.go`) extracts the token and validates it using a hardcoded HS256 `JWTSecret`.

## Known Vulnerabilities Identified for Review

We have identified two critical vulnerabilities in the current prototype design that must be addressed before any production release. We request the auditor to review these and confirm our mitigation strategies:

### 1. Missing End-to-End Owner Verification (Multi-tenancy Flaw)
- **Current State**: The Go daemon only verifies that the JWT is a valid Supabase token. It does not check *who* the token belongs to.
- **Vulnerability**: Any user who registers on the POKIT mobile app will receive a valid token. If they know a target Mac's Cloudflare tunnel URL and session name, they can bypass the daemon's JWT check because their token is technically signed by the correct Supabase instance.
- **Proposed Mitigation**: Implement strict UUID verification. The Go daemon must be started with the owner's UUID (`--owner-uuid=<uuid>`). The `verifyToken` function must parse the `sub` claim from the JWT and assert that `token.sub == owner_uuid`.

### 2. Distributed Binary Secret Leak (Hardcoded HS256 Key)
- **Current State**: The Go daemon uses symmetric encryption (HS256) to verify the token, meaning the highly sensitive `JWTSecret` is hardcoded into `pty.go`.
- **Vulnerability**: Since the Go daemon will be compiled and distributed to end-users (developers installing it on their Macs), a malicious actor can reverse-engineer the binary, extract the `JWTSecret`, and forge valid tokens for any user in the system.
- **Proposed Mitigation**: Migrate to asymmetric cryptography (RS256). Supabase should sign the JWTs with a Private Key, and the Go daemon should dynamically fetch the Public Key (JWKS) from the Supabase API to verify the signature. This ensures the daemon only contains non-sensitive verification capabilities.

## Request to Auditor
1. Please review the code in `companion-daemon/internal/term/pty.go` to confirm the presence of the hardcoded secret and the missing `sub` claim validation.
2. Provide a concrete Go code snippet recommendation for implementing JWKS (JSON Web Key Set) fetching and RS256 token verification using `golang-jwt/jwt/v5`.
3. Validate if passing the JWT via URL query parameter (`?token=...`) over WSS is acceptable, or if it should be moved to a custom WebSocket subprotocol header.
