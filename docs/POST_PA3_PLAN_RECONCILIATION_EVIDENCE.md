# Post-PA3 Plan Reconciliation Evidence

**Plan commit:** `35044f68b4f81b533ce3511fd7429329fe66f18f`

**Plan parent / documentation input HEAD:** `b8884b5934d3a89b132f887de6339b976e01c236`

**Frozen PA3 production baseline:** `34d55e950012e97ccdcb03fd9abba88088ffd9a7`

**Branch:** `feature/phase10-multi-adapter`

This is a documentation-only, non-self-referential evidence record. The SHA of
the commit containing this file is intentionally not embedded in the file.

## 1. Pre-edit repository identity

Commands were run from repository root after `git fetch origin`:

```sh
git rev-parse HEAD
git rev-parse '@{upstream}'
git status --porcelain=v1 --untracked-files=all
```

Output before editing:

```text
b8884b5934d3a89b132f887de6339b976e01c236
b8884b5934d3a89b132f887de6339b976e01c236
```

The status command produced no output. Local HEAD equaled tracked upstream and
the tracked/untracked worktree was clean.

## 2. Input diff scope

```sh
git diff --name-only 34d55e950012e97ccdcb03fd9abba88088ffd9a7..b8884b5934d3a89b132f887de6339b976e01c236
```

Output:

```text
companion-daemon/docs/PB_PA4_CONTRACT.md
companion-daemon/docs/PB_PA4_CONTRACT_EVIDENCE.md
```

The PA3 baseline-to-documentation-input range contained documentation/evidence
only.

## 3. Exact ancestry

```sh
git merge-base --is-ancestor b93c521b7de45f3f577805dbb7de505e16f3172d 34d55e950012e97ccdcb03fd9abba88088ffd9a7
git merge-base --is-ancestor 34d55e950012e97ccdcb03fd9abba88088ffd9a7 b8884b5934d3a89b132f887de6339b976e01c236
git merge-base --is-ancestor b8884b5934d3a89b132f887de6339b976e01c236 35044f68b4f81b533ce3511fd7429329fe66f18f
```

All three commands exited `0`, proving PA2 → PA3 → documentation input → plan
commit ancestry.

## 4. Plan diff scope

```sh
git diff --name-only 34d55e950012e97ccdcb03fd9abba88088ffd9a7..35044f68b4f81b533ce3511fd7429329fe66f18f
```

Output:

```text
companion-daemon/docs/PB_PA4_CONTRACT.md
companion-daemon/docs/PB_PA4_CONTRACT_EVIDENCE.md
docs/PA4_MANAGED_ISOLATION_CONTRACT.md
docs/PB_LEGACY_REMOVAL_CONTRACT.md
docs/POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md
docs/POST_PA3_AUTHORITATIVE_ROADMAP.md
docs/ROADMAP_AFTER_E10B.md
```

Zero-production/mobile/build-script proof:

```sh
git diff --quiet 34d55e950012e97ccdcb03fd9abba88088ffd9a7..35044f68b4f81b533ce3511fd7429329fe66f18f -- companion-daemon/internal companion-daemon/cmd mobile scripts
```

Exit status: `0`.

## 5. Build and vet gates

```sh
cd companion-daemon
GOCACHE=/tmp/devremote-post-pa3-plan-go-cache go build ./...
GOCACHE=/tmp/devremote-post-pa3-plan-go-cache go vet ./...
```

Both commands exited `0` with no output.

## 6. Formatting and clean-plan gate

```sh
git diff --check b8884b5934d3a89b132f887de6339b976e01c236..35044f68b4f81b533ce3511fd7429329fe66f18f
git status --porcelain=v1 --untracked-files=all
```

Both commands produced no output after the plan commit and before this evidence
file was created.

## 7. Reconciled authority proof

The plan commit:

- records PA2 ACCEPT and PA3 ACCEPT at exact frozen production SHA;
- separates PA4 structural isolation from PB physical deletion;
- blocks PB until an exact independent PA4 ACCEPT SHA exists;
- makes that future PA4 SHA the PB prerequisite and rollback;
- preserves managed PTY, exact-generation terminal transport, provider-native
  evidence, and PA3 lifecycle/generation mechanisms;
- classifies legacy discovery and attach paths for removal without treating
  Gemini as an accepted adapter;
- makes `AcceptedRecordSource` conditional on future surviving provider-native
  need rather than a legacy-preservation requirement;
- marks stale combined and higher-level plans as superseded or historical.

## 8. Release verification

After the evidence commit is pushed, the releaser must run:

```sh
git fetch origin
git rev-parse HEAD
git rev-parse '@{upstream}'
git status --porcelain=v1 --untracked-files=all
```

Acceptance requires exact HEAD/upstream equality and no status output. That
post-push result is reported alongside the enclosing evidence commit SHA rather
than self-referenced in this file.
