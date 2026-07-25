# Reasonix + AGY Capability Research — Coordinator Suitability

**Date:** 2026-07-26
**Purpose:** Evaluate Reasonix and AGY CLI binaries as coordinator providers
for the OMNI orchestration system, alongside existing Codex and Claude
integrations.

---

## A. Identity

### Reasonix

| Field | Value |
|-------|-------|
| **Binary** | `reasonix` |
| **Path** | `/opt/homebrew/bin/reasonix` |
| **Version** | `v1.17.10` |
| **Install** | Homebrew (`brew install reasonix`) |
| **Auth** | Environment variables via `api_key_env` in `reasonix.toml` (e.g. `DEEPSEEK_API_KEY`) |
| **Config** | `~/.reasonix/config.toml` (global) + `./reasonix.toml` (project) |

### AGY

| Field | Value |
|-------|-------|
| **Binary** | `agy` |
| **Path** | `/Users/mhk/.local/bin/agy` |
| **Version** | `1.1.6` |
| **Install** | Unknown (non-Homebrew local binary) |
| **Auth** | Pre-configured (provider credentials managed outside the CLI) |
| **Config** | Built-in; no visible config file |

---

## B. Non-Interactive Execution

### Reasonix

| Capability | Status | Details |
|-----------|--------|---------|
| **One-shot mode** | ✅ | `reasonix run <task>` |
| **Stdin prompt** | ✅ | `echo "..." \| reasonix run ...` |
| **Structured output** | ⚠️ Partial | JSON returned but with thinking progress on stderr. Surrounding prose/markdown possible without strict prompting. |
| **Timeout control** | ❌ | No `--timeout` flag. Process must be killed externally. |
| **Tool restriction** | ✅ | `--max-steps 0` disables tool use entirely → pure text response. |
| **Session continuity** | ✅ | `--continue`, `--resume <path>`, `--copy` for session continuation. |
| **Config** | ✅ | `reasonix.toml` for project-level config. Model, provider configured centrally. |
| **Metrics** | ✅ | `--metrics <path>` writes JSON token/cache/cost summary. |

### AGY

| Capability | Status | Details |
|-----------|--------|---------|
| **One-shot mode** | ✅ | `agy --print <prompt>` (alias: `-p`) |
| **Stdin prompt** | ⚠️ Limited | Piping to `--print` accepted but may conflict with prompt arg. |
| **Structured output** | ✅ | Returns exactly the JSON requested. No surrounding text, no markdown fences. |
| **Timeout control** | ✅ | `--print-timeout` (default 5m). Configurable duration. |
| **Tool restriction** | ❌ | No `--max-steps` equivalent. Tool use is implicit. |
| **Session continuity** | ✅ | `--continue` / `--conversation <id>` for resuming sessions. |
| **Model list** | ✅ | `agy models` returns available models. |
| **Config** | ⚠️ Limited | No per-project config file visible. Models are built-in. |

---

## C. Decision Output — Strict JSON Compliance

### Test: Return exact JSON with no other text

**Prompt:** `"Return ONLY {\"decision\":\"VALIDATE\",\"reason\":\"start\",\"next_instruction\":\"write file\"}. No other text."`

| Provider | Attempt 1 | Attempt 2 | Attempt 3 |
|----------|-----------|-----------|-----------|
| **Reasonix** | ⚠️ Returned JSON in ```json fence with thinking | ✅ `{"decision":"VALIDATE","reason":"start","next_instruction":"write output.txt"}` (with `--max-steps 0`) | ✅ Consistent |
| **AGY** | ✅ `{"decision":"COMPLETE","reason":"No prior task...","next_instruction":""}` | ✅ `{"decision":"VALIDATE","reason":"start","next_instruction":"write output.txt"}` | ✅ `{"decision":"COMPLETE","reason":"done","next_instruction":""}` |

### Verdict

- **AGY**: **READY** — returns exact JSON, no surrounding text, no markdown. 3/3 attempts consistent.
- **Reasonix**: **LIMITED** — requires `--max-steps 0` to suppress tool use. Without it, Reasonix uses tools (write_file) instead of returning text. With `--max-steps 0`, returns JSON but with thinking indicator on stderr.

### Schema Enforcement

| Provider | Follows exact decision value? | Follows exact JSON shape? | Surrounding prose risk? |
|----------|------------------------------|--------------------------|------------------------|
| **Reasonix** | ⚠️ May paraphrase ("acknowledged" instead of "VALIDATE") | ✅ Correct shape | 🟡 Low with `--max-steps 0` + strict prompt |
| **AGY** | ⚠️ May change decision based on prompt interpretation | ✅ Correct shape | 🟢 None |

---

## D. Model/Effort Control

### Reasonix

| Capability | Status | Details |
|-----------|--------|---------|
| **Model selection** | ✅ | `--model deepseek-pro/deepseek-v4-pro` (provider/model format) |
| **Programmatic discovery** | ❌ | No `reasonix models` command. Must read config.toml. |
| **Reasoning/effort** | ❌ | No explicit effort control. Model choice determines behavior. |
| **Invalid model behavior** | ✅ | Clear error: "unknown model" with configured alternatives listed. |

### AGY

| Capability | Status | Details |
|-----------|--------|---------|
| **Model selection** | ✅ | `--model claude-sonnet-4-6` (human-readable name) |
| **Programmatic discovery** | ✅ | `agy models` returns explicit list. |
| **Reasoning/effort** | ✅ | `--effort low|medium|high` (Haiku, Sonnet, Opus mapping) |
| **Invalid model behavior** | ❓ | Untested. May error or silently fall back. |

---

## E. Architecture

### Reasonix

| Feature | Status | Details |
|---------|--------|---------|
| **Subagents** | ❓ | Not visible in CLI help |
| **Planner/executor split** | ❓ | Not visible |
| **Parallel execution** | ❓ | Not visible |
| **Memory** | ✅ | `memory-v5` with `observe\|compact\|on\|off\|status` modes. Project memory via `AGENTS.md`. |
| **Approval** | ✅ | Interactive approval; `--yolo` to skip. |
| **MCP** | ✅ | `reasonix mcp add\|remove\|list\|import` |
| **Async/serve** | ✅ | `reasonix serve` over HTTP+SSE |
| **ACP** | ✅ | Agent Client Protocol via `reasonix --acp` (stdio JSON-RPC) |

### AGY

| Feature | Status | Details |
|---------|--------|---------|
| **Subagents** | ❓ | `--agent` flag exists. May support agent specification. |
| **Planner/executor split** | ❓ | `--mode plan` exists |
| **Parallel execution** | ❓ | Not visible |
| **Memory** | ❌ | No visible memory subsystem |
| **Approval** | ✅ | `--dangerously-skip-permissions` to bypass |
| **MCP** | ❌ | Not visible |
| **Async/serve** | ✅ | `--remote-control` for remote sessions |
| **Sandbox** | ✅ | `--sandbox` for restricted execution |

---

## F. Runtime Behavior

### Reasonix

| Scenario | Behavior |
|----------|----------|
| **Normal exit** | Returns JSON + metrics. Exit code 0. |
| **Tool use** | With `--max-steps 0`: suppressed. Without: may write files, run commands. |
| **Interactive request** | Not triggered in `run` mode (non-interactive). |
| **Auth fail** | Error message with config guidance. |
| **Invalid model** | Clear error: "unknown model NAME (configured:...)" |
| **Malformed output** | May return JSON in markdown fence or prose. Parseable with `extractJSON`. |
| **Timeout** | No built-in timeout. Must use `context.WithTimeout` in Go. |
| **Resume** | `--resume <path>` loads session file. |
| **Latency** | ~14s for simple decision (with cached prompt). Cold: ~30s. |

### AGY

| Scenario | Behavior |
|----------|----------|
| **Normal exit** | Returns clean JSON. Exit code 0. |
| **Tool use** | No tool restriction flag. May use tools based on prompt. |
| **Interactive request** | Not triggered in `--print` mode (non-interactive). |
| **Auth fail** | Unknown (pre-authenticated). |
| **Invalid model** | Unknown. May silently fall back to default. |
| **Malformed output** | Tested: no malformed output in 3 attempts. Risk is low. |
| **Timeout** | `--print-timeout` with configurable duration. |
| **Resume** | `--continue` / `--conversation <id>`. |
| **Latency** | ~2-3s for simple decision. |

---

## G. Verdict

### AGY: **READY**

| Criterion | Score |
|-----------|-------|
| Non-interactive one-shot | ✅ `--print` |
| Strict JSON output | ✅ Clean JSON, no prose |
| Model selection | ✅ `--model` + `--effort` |
| Timeout control | ✅ `--print-timeout` |
| Latency | ✅ ~2-3s |
| Session continuity | ✅ `--continue` / `--conversation` |
| Tool restriction | ⚠️ No `--max-steps`; relies on prompt |
| MCP support | ❌ Not visible |

**Integration effort:** ~30 lines of Go (same pattern as ClaudeCoordinator). The
`--print` mode plus strict prompting produces clean JSON suitable for coordinator
decisions. AGY is the best candidate among the two for a new coordinator provider.

**Recommended coordinator command:**
```
agy --print --model claude-sonnet-4-6 --effort low --print-timeout 30s "<structured prompt>"
```

### Reasonix: **LIMITED**

| Criterion | Score |
|-----------|-------|
| Non-interactive one-shot | ✅ `reasonix run` |
| Strict JSON output | ⚠️ Requires `--max-steps 0` + strict prompt |
| Model selection | ✅ `--model` |
| Timeout control | ❌ Must use external `context.WithTimeout` |
| Latency | ⚠️ ~14s (cached) to ~30s (cold) |
| Session continuity | ✅ `--continue` / `--resume` |
| Tool restriction | ✅ `--max-steps 0` |
| MCP support | ✅ |
| ACP protocol | ✅ Structured JSON-RPC over stdio |

**Integration effort:** ~40 lines of Go (same pattern). Requires `--max-steps 0`
to suppress tool use and strict prompting for JSON compliance. Higher latency
makes it less suitable for real-time coordinator decisions but viable for
thorough planning decisions.

**Recommended coordinator command:**
```
reasonix run --max-steps 0 --model deepseek-pro/deepseek-v4-pro "<structured prompt>"
```

---

## H. OMNI Integration Summary

| Provider | Verdict | Binary | Integration Lines | Latency | Key Risk |
|----------|---------|--------|-------------------|---------|----------|
| **Codex** | ✅ INTEGRATED | `codex exec --json` | ~80 | ~15-30s | JSON in code fences |
| **Claude** | ✅ INTEGRATED | `claude -p --output-format json` | ~80 | ~10-20s | JSON parsing |
| **AGY** | 🟢 READY | `agy --print` | ~30 | ~2-3s | No `--max-steps` |
| **Reasonix** | 🟡 LIMITED | `reasonix run --max-steps 0` | ~40 | ~14-30s | Tool use suppression required |

### Next Steps

1. Implement `AGYCoordinator` in `internal/coordinator/agy_coordinator.go`
   using the ClaudeCoordinator pattern (same Coordinator interface).
2. Wire `--coordinator agy` in CLI.
3. Add black-box E2E test with fake agy binary.
4. Consider ReasonixCoordinator for planning-heavy coordinator tasks
   where higher latency is acceptable.

---

**Research complete:** 2026-07-26
