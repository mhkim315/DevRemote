# R1 Runtime Signal Evidence Manifest

Status: redacted research evidence
Research date: 2026-07-10

No raw user prompts, source, repository paths, secrets, tokens, host names,
device identifiers, or provider session identifiers are committed.

| Fixture | Product/version | Evidence | SHA-256 | Correlation result |
|---|---|---|---|---|
| `r1-fixtures/claude-hooks-2.1.206.jsonl` | Claude Code 2.1.206 | `UserPromptSubmit` hook lifecycle in stream JSON | `9ff0e7486021934d9f800f379c091f63ab0393e871ae89bb31849bcad09af764` | provider session observed; controlled_pty mapping unavailable |
| `r1-fixtures/codex-app-server-0.144.1.json` | Codex CLI 0.144.1 | version-matched schema methods and isolated initialize response | `294c9b51a0e051fbbbab6d054839d8762b17878ff127a2f3eacf43b3db02ae31` | app-server transport observed; TUI mapping unavailable |
| `r1-fixtures/generic-pty-structural.json` | synthetic macOS PTY | alternate-screen, erase-line, CR byte sequence | `2a32dae2999e3965a857b5ed0e2ce2baac2c7b5fd48a41f7300e921f3ca7c87c` | structural only; no semantic state claim |

## Capture environments

### Claude Code

- Publisher/product: Anthropic Claude Code.
- Version: `2.1.206`.
- Surface: isolated `/tmp` print-mode invocation with temporary hook settings.
- Setup: no user settings file was edited. The no-credential model request
  failed only after local hook lifecycle evidence was emitted.
- Source: <https://code.claude.com/docs/en/hooks>.
- Redaction: UUIDs, hook IDs, cwd, model/account fields, and host paths were
  replaced with placeholders.

### Codex

- Publisher/product: OpenAI Codex CLI.
- Version: `0.144.1`.
- Surface: `codex app-server generate-json-schema` and stdio initialize with
  isolated temporary `CODEX_HOME`.
- Source: <https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md>.
- Redaction: installation ID, hostname, path, and machine user-agent fields
  were replaced with placeholders.

### Generic PTY

- Surface: isolated local PTY test with synthetic, non-sensitive ANSI bytes.
- Source classification: observed terminal protocol structure, not agent state.
- Redaction: fixture is synthetic and contains no user data.

## Not observed

OpenCode, Orca, Omnara, Cline, Aider, Goose, Continue, and Warp were not
installed or authenticated in the research environment. Their matrix entries
are documented-source findings only; they are not runtime proof. R1 did not
install products solely to manufacture evidence.
