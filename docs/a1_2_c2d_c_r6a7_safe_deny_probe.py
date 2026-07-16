#!/usr/bin/env python3
"""A1.2 C2D-C R6-A7: safe Claude 2.1.209 denial evidence harness.

Replaces the rejected R6-A6 probe (98fe899, 23dc740). Evidence only — no
production, mobile, or A1-authority code is touched. The contract is
docs/A1_2_C2D_C_R6_A7_EVIDENCE_CONTRACT_NOTE.md.

Design: every effectful dependency (executable runner, pin verifier input,
stream parser, projector, cleanup observation) is injectable so the Packet A
deterministic tests (a1_2_c2d_c_r6a7_probe_tests.py) exercise the complete
lifecycle without ever invoking Claude.

Live use (Packet B, exactly two runs, only after the no-model tests pass):

    python3 docs/a1_2_c2d_c_r6a7_safe_deny_probe.py --live \
        --out-a <projection-a.json> --out-b <projection-b.json>

Stdout carries only fixed status lines, closed enums/booleans, and the
validated bounded projections. Raw prompt, command, cwd, temp paths, tool
input, transcripts, auth data, exception text, PIDs, and host/user identity
never reach stdout or any serialized artifact.
"""

import hashlib
import json
import os
import re
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile
import time

# --- Pinned artifact contract (public constants) ---------------------------

PINNED_LOCATOR = "~/.local/share/claude/versions/2.1.209"
PINNED_VERSION = "2.1.209"
PINNED_ARCH = "arm64"
PINNED_SHA256 = "59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c"

SCHEMA = "a1_2.c2d_c.r6a7.denial_projection.v1"
DENIAL_DISCRIMINATOR = "permission_denials"

VERSION_TIMEOUT_S = 15
PHASE_TIMEOUT_S = 120
CLEANUP_DEADLINE_S = 5.0
CLEANUP_POLL_S = 0.02
MAX_HOOK_STDIN_BYTES = 1048576

# --- Closed failure vocabulary ---------------------------------------------

FAILURE_CODES = frozenset({
    "version_mismatch", "executable_not_pinned", "spawn_failed",
    "timeout", "hook_capture_missing", "no_deferred_result",
    "defer_join_failed", "resume_join_failed", "identity_mismatch",
    "unexpected_tool_input", "no_denial_match", "ambiguous_denial",
    "marker_violation", "schema_invalid", "projection_bounds_exceeded",
    "cleanup_failed", "raw_delete_failed", "provider_failed",
    "unstable_structure", "unexpected_error",
})


class ProbeFailure(Exception):
    """Failure carrying only a closed vocabulary code — never raw values."""

    def __init__(self, code):
        if code not in FAILURE_CODES:
            code = "unexpected_error"
        self.code = code
        super().__init__(code)


# --- Closed projection schema ----------------------------------------------

EXIT_VOCAB = frozenset({"exited_zero", "exited_nonzero", "timeout"})
CLEANUP_VOCAB = frozenset({"clean", "killed_leftover"})
ARCH_VOCAB = frozenset({"arm64", "script"})  # "script" is Packet-A fakes only
JSON_TYPE_VOCAB = frozenset({"null", "boolean", "number", "string", "array", "object"})
MAX_FIELD_COUNT = 32
MAX_FIELD_NAME_LEN = 64
HEX64_RE = re.compile(r"^[0-9a-f]{64}$")

PROJECTION_KEYS = frozenset({
    "schema", "pinned_version", "pinned_sha256", "architecture",
    "executable_locator", "run",
    "result_top_fields", "denial_entry_top_fields", "denial_discriminator",
    "denial_match_unique", "provider_input_present",
    "provider_input_digest_equal",
    "defer_join_session_equal", "defer_join_tool_use_equal",
    "defer_join_tool_name_equal", "defer_join_input_digest_equal",
    "resume_join_session_equal", "resume_join_tool_use_equal",
    "resume_join_tool_name_equal", "resume_join_input_digest_equal",
    "marker_command_bound", "marker_absent_pre", "marker_absent_post",
    "initial_exit", "resume_exit", "initial_cleanup", "resume_cleanup",
    "raw_capture_sha256",
})

RUN_PSEUDONYMS = frozenset({"run-a", "run-b"})
STRIP_FOR_STABILITY = ("run", "raw_capture_sha256")


# --- Canonicalizer (the single digest rule for every tool_input site) ------

def canonical_digest(obj):
    canon = json.dumps(obj, sort_keys=True, separators=(",", ":"),
                       ensure_ascii=False)
    return hashlib.sha256(canon.encode("utf-8")).hexdigest()


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            h.update(chunk)
    return h.hexdigest()


def framed_digest(parts):
    """Digest of length-prefixed byte parts in fixed order (no ambiguity)."""
    h = hashlib.sha256()
    for part in parts:
        h.update(len(part).to_bytes(8, "big"))
        h.update(part)
    return h.hexdigest()


# --- Pin verification (before any provider spawn) ---------------------------

class PinConfig:
    def __init__(self, path, expected_sha256, expected_version, expected_arch,
                 locator):
        self.path = path
        self.expected_sha256 = expected_sha256
        self.expected_version = expected_version
        self.expected_arch = expected_arch
        self.locator = locator


LIVE_PIN = PinConfig(
    path=PINNED_LOCATOR,
    expected_sha256=PINNED_SHA256,
    expected_version=PINNED_VERSION,
    expected_arch=PINNED_ARCH,
    locator=PINNED_LOCATOR,
)


def detect_arch(path):
    with open(path, "rb") as f:
        head = f.read(8)
    if head[:2] == b"#!":
        return "script"
    if head[:4] == b"\xcf\xfa\xed\xfe":  # MH_MAGIC_64 little endian
        cputype = int.from_bytes(head[4:8], "little")
        if cputype == 0x0100000C:
            return "arm64"
        if cputype == 0x01000007:
            return "x86_64"
    return "unknown"


def verify_pin(candidate_path, pin, runner, cwd):
    """Realpath, digest, and architecture checks happen BEFORE any spawn.

    Only the already-proven pinned artifact is ever executed (--version).
    """
    cand_real = os.path.realpath(os.path.expanduser(candidate_path))
    pin_real = os.path.realpath(os.path.expanduser(pin.path))
    if cand_real != pin_real or not os.path.isfile(cand_real):
        raise ProbeFailure("executable_not_pinned")
    if sha256_file(cand_real) != pin.expected_sha256:
        raise ProbeFailure("executable_not_pinned")
    arch = detect_arch(cand_real)
    if arch != pin.expected_arch:
        raise ProbeFailure("executable_not_pinned")
    outcome = runner.run([cand_real, "--version"], cwd=cwd, env=None,
                         timeout_s=VERSION_TIMEOUT_S)
    if outcome.exit_enum != "exited_zero":
        raise ProbeFailure("version_mismatch")
    tokens = outcome.stdout.decode("utf-8", errors="replace").split()
    if not tokens or tokens[0] != pin.expected_version:
        raise ProbeFailure("version_mismatch")
    return {"version": tokens[0], "sha256": pin.expected_sha256, "arch": arch}


# --- Owned-process-group runner ---------------------------------------------

class RunOutcome:
    def __init__(self, exit_enum, stdout, cleanup_enum, pgid):
        self.exit_enum = exit_enum
        self.stdout = stdout
        self.cleanup_enum = cleanup_enum
        self.pgid = pgid


class GroupRunner:
    """Spawns into a fresh session/process group and proves group cleanup.

    Cleanup is bounded polling against a deadline — never a bare sleep used
    as evidence. The kill signal is only ever sent to the owned group, never
    to the harness's own group.
    """

    def __init__(self, poll_interval_s=CLEANUP_POLL_S,
                 cleanup_deadline_s=CLEANUP_DEADLINE_S):
        self.poll_interval_s = poll_interval_s
        self.cleanup_deadline_s = cleanup_deadline_s

    def run(self, argv, cwd, env, timeout_s):
        try:
            proc = subprocess.Popen(
                argv, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                stdin=subprocess.DEVNULL, cwd=cwd, env=env,
                start_new_session=True)
        except OSError:
            raise ProbeFailure("spawn_failed")
        try:
            pgid = os.getpgid(proc.pid)
        except ProcessLookupError:
            pgid = proc.pid  # new session: pgid == pid even after exit
        if pgid == os.getpgrp():
            # start_new_session failed to isolate; never signal our own group.
            proc.kill()
            proc.wait()
            raise ProbeFailure("cleanup_failed")
        timed_out = False
        try:
            out, _ = proc.communicate(timeout=timeout_s)
        except subprocess.TimeoutExpired:
            timed_out = True
            self._killpg(pgid)
            out, _ = proc.communicate()  # reap the direct child
        exit_enum = ("timeout" if timed_out else
                     "exited_zero" if proc.returncode == 0 else
                     "exited_nonzero")
        cleanup_enum = self._drain_group(pgid)
        return RunOutcome(exit_enum, out, cleanup_enum, pgid)

    def _killpg(self, pgid):
        try:
            os.killpg(pgid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        except PermissionError:
            # macOS: a group whose remaining members are exiting zombies can
            # refuse signals with EPERM. Non-fatal: the bounded poll below
            # only reports success once the group is provably gone (ESRCH).
            pass

    def group_alive(self, pgid):
        try:
            os.killpg(pgid, 0)
            return True
        except ProcessLookupError:
            return False
        except PermissionError:
            return True

    def _drain_group(self, pgid):
        if not self.group_alive(pgid):
            return "clean"
        self._killpg(pgid)
        deadline = time.monotonic() + self.cleanup_deadline_s
        while time.monotonic() < deadline:
            if not self.group_alive(pgid):
                return "killed_leftover"
            time.sleep(self.poll_interval_s)
        raise ProbeFailure("cleanup_failed")


class SpawnRecorder:
    """Wraps a runner; records every spawn attempt (for zero-spawn proofs)."""

    def __init__(self, inner):
        self.inner = inner
        self.calls = []

    def run(self, argv, cwd, env, timeout_s):
        self.calls.append(list(argv))
        return self.inner.run(argv, cwd=cwd, env=env, timeout_s=timeout_s)


# --- Private stream parsing and identity joins ------------------------------

def iter_json_lines(raw):
    for line in raw.decode("utf-8", errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except ValueError:
            continue
        if isinstance(obj, dict):
            yield obj


def find_deferred_event(objs):
    hits = [o for o in objs
            if o.get("type") == "result"
            and o.get("stop_reason") == "tool_deferred"
            and isinstance(o.get("deferred_tool_use"), dict)]
    if not hits:
        raise ProbeFailure("no_deferred_result")
    if len(hits) > 1:
        raise ProbeFailure("provider_failed")
    return hits[0]


def compare_identity(expected, got):
    """Four-field identity comparison. Both sides are private dicts with
    session_id / tool_use_id / tool_name / input_digest."""
    return {
        "session": expected.get("session_id") == got.get("session_id"),
        "tool_use": expected.get("tool_use_id") == got.get("tool_use_id"),
        "tool_name": expected.get("tool_name") == got.get("tool_name"),
        "input_digest": expected.get("input_digest") == got.get("input_digest"),
    }


def join_deferred(identity, deferred_event):
    dtu = deferred_event.get("deferred_tool_use") or {}
    got = {
        "session_id": deferred_event.get("session_id"),
        "tool_use_id": dtu.get("id"),
        "tool_name": dtu.get("name"),
        "input_digest": canonical_digest(dtu.get("input")),
    }
    return compare_identity(identity, got)


def match_denial(identity, objs):
    """Exactly one unambiguous provider-native denial for our identity.

    A carrying event only counts when its session_id equals the bound
    session. Within it, exactly one entry may carry our tool_use_id; when
    that entry names a tool it must be our tool, and when it carries raw
    tool input the input is hashed privately with the same canonicalizer
    and only the equality boolean is kept.
    """
    carrying = [o for o in objs
                if o.get("type") == "result"
                and isinstance(o.get(DENIAL_DISCRIMINATOR), list)
                and o.get(DENIAL_DISCRIMINATOR)]
    if not carrying:
        raise ProbeFailure("no_denial_match")
    if len(carrying) > 1:
        raise ProbeFailure("ambiguous_denial")
    event = carrying[0]
    if event.get("session_id") != identity["session_id"]:
        raise ProbeFailure("no_denial_match")
    entries = [e for e in event[DENIAL_DISCRIMINATOR]
               if isinstance(e, dict)
               and e.get("tool_use_id") == identity["tool_use_id"]]
    if not entries:
        raise ProbeFailure("no_denial_match")
    if len(entries) > 1:
        raise ProbeFailure("ambiguous_denial")
    entry = entries[0]
    if "tool_name" in entry and entry.get("tool_name") != identity["tool_name"]:
        raise ProbeFailure("identity_mismatch")
    provider_input_present = "tool_input" in entry
    digest_equal = None
    if provider_input_present:
        digest_equal = (canonical_digest(entry["tool_input"])
                        == identity["input_digest"])
        if not digest_equal:
            raise ProbeFailure("identity_mismatch")
    return {
        "event": event,
        "entry": entry,
        "unique": True,
        "provider_input_present": provider_input_present,
        "digest_equal": digest_equal,
    }


# --- Bounded structural projection ------------------------------------------

def json_type(v):
    if v is None:
        return "null"
    if isinstance(v, bool):
        return "boolean"
    if isinstance(v, (int, float)):
        return "number"
    if isinstance(v, str):
        return "string"
    if isinstance(v, list):
        return "array"
    if isinstance(v, dict):
        return "object"
    raise ProbeFailure("schema_invalid")


def project_fields(obj):
    """Top-level field names and JSON types only — no recursion, hard bounds.

    Exceeding a bound fails closed; nothing is silently truncated.
    """
    if not isinstance(obj, dict):
        raise ProbeFailure("schema_invalid")
    if len(obj) > MAX_FIELD_COUNT:
        raise ProbeFailure("projection_bounds_exceeded")
    out = {}
    for k in sorted(obj.keys()):
        if not isinstance(k, str) or not k:
            raise ProbeFailure("schema_invalid")
        if len(k) > MAX_FIELD_NAME_LEN:
            raise ProbeFailure("projection_bounds_exceeded")
        out[k] = json_type(obj[k])
    return out


def validate_projection(proj):
    """Re-asserts the closed key set, closed vocabularies, and the success
    predicate. A projection violating any rule cannot be serialized."""
    if not isinstance(proj, dict) or set(proj.keys()) != PROJECTION_KEYS:
        raise ProbeFailure("schema_invalid")
    if proj["schema"] != SCHEMA:
        raise ProbeFailure("schema_invalid")
    if proj["pinned_version"] != PINNED_VERSION:
        raise ProbeFailure("schema_invalid")
    if not (isinstance(proj["pinned_sha256"], str)
            and HEX64_RE.match(proj["pinned_sha256"])):
        raise ProbeFailure("schema_invalid")
    if not (isinstance(proj["raw_capture_sha256"], str)
            and HEX64_RE.match(proj["raw_capture_sha256"])):
        raise ProbeFailure("schema_invalid")
    if proj["architecture"] not in ARCH_VOCAB:
        raise ProbeFailure("schema_invalid")
    loc = proj["executable_locator"]
    if not (isinstance(loc, str) and loc.startswith("~/")
            and "/Users/" not in loc and len(loc) <= 128):
        raise ProbeFailure("schema_invalid")
    if proj["run"] not in RUN_PSEUDONYMS:
        raise ProbeFailure("schema_invalid")
    for fk in ("result_top_fields", "denial_entry_top_fields"):
        fields = proj[fk]
        if not isinstance(fields, dict) or len(fields) > MAX_FIELD_COUNT:
            raise ProbeFailure("projection_bounds_exceeded")
        for name, typ in fields.items():
            if (not isinstance(name, str) or not name
                    or len(name) > MAX_FIELD_NAME_LEN):
                raise ProbeFailure("projection_bounds_exceeded")
            if typ not in JSON_TYPE_VOCAB:
                raise ProbeFailure("schema_invalid")
    if proj["denial_discriminator"] != DENIAL_DISCRIMINATOR:
        raise ProbeFailure("schema_invalid")
    for ek, vocab in (("initial_exit", EXIT_VOCAB), ("resume_exit", EXIT_VOCAB),
                      ("initial_cleanup", CLEANUP_VOCAB),
                      ("resume_cleanup", CLEANUP_VOCAB)):
        if proj[ek] not in vocab:
            raise ProbeFailure("schema_invalid")
    # Success predicate: only all-true evidence may ever be serialized.
    must_be_true = (
        "denial_match_unique",
        "defer_join_session_equal", "defer_join_tool_use_equal",
        "defer_join_tool_name_equal", "defer_join_input_digest_equal",
        "resume_join_session_equal", "resume_join_tool_use_equal",
        "resume_join_tool_name_equal", "resume_join_input_digest_equal",
        "marker_command_bound", "marker_absent_pre", "marker_absent_post",
    )
    for bk in must_be_true:
        if proj[bk] is not True:
            raise ProbeFailure("schema_invalid")
    pip = proj["provider_input_present"]
    pde = proj["provider_input_digest_equal"]
    if not isinstance(pip, bool):
        raise ProbeFailure("schema_invalid")
    if pip and pde is not True:
        raise ProbeFailure("schema_invalid")
    if not pip and pde is not None:
        raise ProbeFailure("schema_invalid")
    if proj["initial_exit"] == "timeout" or proj["resume_exit"] == "timeout":
        raise ProbeFailure("schema_invalid")
    return proj


# --- Hook scripts (generated into the owned directory) ----------------------
# Both hooks derive the owned directory from their own location, keep every
# raw value inside it, and fail closed (deny + exit 2) per the accepted C0D
# hook semantics. The canonicalizer is byte-identical to canonical_digest().

HOOK_DEFER_SOURCE = '''#!/usr/bin/env python3
import hashlib, json, os, sys

OWNED = os.path.dirname(os.path.abspath(__file__))
DENY = ('{"hookSpecificOutput":{"hookEventName":"PreToolUse",'
        '"permissionDecision":"deny",'
        '"permissionDecisionReason":"malformed input - tool call blocked"}}')
DEFER = ('{"hookSpecificOutput":{"hookEventName":"PreToolUse",'
         '"permissionDecision":"defer"}}')

raw = sys.stdin.buffer.read(''' + str(MAX_HOOK_STDIN_BYTES) + ''')
try:
    d = json.loads(raw.decode("utf-8"))
except Exception:
    print(DENY)
    sys.exit(2)
sid = d.get("session_id")
tuid = d.get("tool_use_id")
tname = d.get("tool_name")
tin = d.get("tool_input")
if not sid or not tuid or not tname or not isinstance(tin, dict):
    print(DENY)
    sys.exit(2)
canon = json.dumps(tin, sort_keys=True, separators=(",", ":"),
                   ensure_ascii=False)
rec = {
    "session_id": sid,
    "tool_use_id": tuid,
    "tool_name": tname,
    "input_digest": hashlib.sha256(canon.encode("utf-8")).hexdigest(),
    "command": tin.get("command", ""),
}
try:
    fd = os.open(os.path.join(OWNED, "defer_capture.json"),
                 os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
except FileExistsError:
    print(DENY)
    sys.exit(2)
with os.fdopen(fd, "w") as f:
    json.dump(rec, f)
print(DEFER)
'''

HOOK_RESUME_SOURCE = '''#!/usr/bin/env python3
import hashlib, json, os, sys

OWNED = os.path.dirname(os.path.abspath(__file__))

def deny(reason, code):
    print('{"hookSpecificOutput":{"hookEventName":"PreToolUse",'
          '"permissionDecision":"deny",'
          '"permissionDecisionReason":"' + reason + '"}}')
    sys.exit(code)

raw = sys.stdin.buffer.read(''' + str(MAX_HOOK_STDIN_BYTES) + ''')
try:
    d = json.loads(raw.decode("utf-8"))
except Exception:
    deny("malformed input - tool call blocked", 2)
try:
    with open(os.path.join(OWNED, "expected_identity.json")) as f:
        want = json.load(f)
except Exception:
    deny("no deferred identity - tool call blocked", 2)
tin = d.get("tool_input")
if not isinstance(tin, dict):
    deny("malformed input - tool call blocked", 2)
canon = json.dumps(tin, sort_keys=True, separators=(",", ":"),
                   ensure_ascii=False)
got = {
    "session_id": d.get("session_id"),
    "tool_use_id": d.get("tool_use_id"),
    "tool_name": d.get("tool_name"),
    "input_digest": hashlib.sha256(canon.encode("utf-8")).hexdigest(),
}
for k in ("session_id", "tool_use_id", "tool_name", "input_digest"):
    if got[k] != want.get(k):
        deny("identity mismatch - tool call blocked", 2)
try:
    os.mkdir(os.path.join(OWNED, "claim"))
except FileExistsError:
    deny("already consumed - tool call blocked", 2)
with open(os.path.join(OWNED, "resume_capture.json"), "w") as f:
    json.dump(got, f)
deny("a1.2 c2d-c r6-a7 research denial", 0)
'''


def write_hook(owned, name, source):
    path = os.path.join(owned, name)
    with open(path, "w") as f:
        f.write(source)
    os.chmod(path, 0o700)
    return path


def write_settings(owned, name, hook_path):
    settings = {"hooks": {"PreToolUse": [{"matcher": "", "hooks": [
        {"type": "command",
         "command": "python3 " + json.dumps(hook_path)}]}]}}
    path = os.path.join(owned, name)
    with open(path, "w") as f:
        json.dump(settings, f)
    return path


# --- Live lifecycle (one run) ------------------------------------------------

def say(line):
    """Stdout discipline: only fixed strings and closed enums/booleans."""
    print(line, flush=True)


def read_private_json(path, failure_code):
    if not os.path.isfile(path):
        raise ProbeFailure(failure_code)
    try:
        with open(path) as f:
            obj = json.load(f)
    except ValueError:
        raise ProbeFailure(failure_code)
    if not isinstance(obj, dict):
        raise ProbeFailure(failure_code)
    return obj


def run_live_once(run_name, pin, runner, executable=None):
    """One complete defer -> resume -> deny lifecycle in an owned 0700 dir.

    Returns a validated bounded projection; raises ProbeFailure otherwise.
    Every private raw artifact is deleted in finally and the deletion is
    re-verified.
    """
    if run_name not in RUN_PSEUDONYMS:
        raise ProbeFailure("schema_invalid")
    candidate = executable if executable is not None else pin.path
    owned = tempfile.mkdtemp(prefix="r6a7-")
    os.chmod(owned, 0o700)
    try:
        facts = verify_pin(candidate, pin, runner, cwd=owned)
        exe = os.path.realpath(os.path.expanduser(pin.path))
        say(run_name + ": pin verified")

        marker_name = "marker-" + secrets.token_hex(4)
        marker_path = os.path.join(owned, marker_name)
        if os.path.exists(marker_path):
            raise ProbeFailure("marker_violation")
        marker_absent_pre = True
        expected_cmd = "touch " + marker_name

        hook1 = write_hook(owned, "hook_defer.py", HOOK_DEFER_SOURCE)
        settings1 = write_settings(owned, "settings_defer.json", hook1)
        prompt = ("Use your Bash tool to run exactly this command: "
                  + expected_cmd)
        argv1 = [exe, "--verbose", "--settings", settings1,
                 "--setting-sources", "", "--output-format", "stream-json",
                 "--include-partial-messages", "-p", prompt]
        outcome1 = runner.run(argv1, cwd=owned, env=None,
                              timeout_s=PHASE_TIMEOUT_S)
        say(run_name + ": initial exit=" + outcome1.exit_enum
            + " cleanup=" + outcome1.cleanup_enum)
        if outcome1.exit_enum == "timeout":
            raise ProbeFailure("timeout")

        capture = read_private_json(os.path.join(owned, "defer_capture.json"),
                                    "hook_capture_missing")
        identity = {k: capture.get(k) for k in
                    ("session_id", "tool_use_id", "tool_name", "input_digest")}
        if any(not isinstance(v, str) or not v for v in identity.values()):
            raise ProbeFailure("hook_capture_missing")
        if identity["tool_name"] != "Bash":
            raise ProbeFailure("unexpected_tool_input")
        if capture.get("command") != expected_cmd:
            raise ProbeFailure("unexpected_tool_input")
        marker_command_bound = True

        deferred = find_deferred_event(list(iter_json_lines(outcome1.stdout)))
        defer_join = join_deferred(identity, deferred)
        if not all(defer_join.values()):
            raise ProbeFailure("defer_join_failed")
        say(run_name + ": defer join complete")

        expected_path = os.path.join(owned, "expected_identity.json")
        with open(expected_path, "w") as f:
            json.dump(identity, f)
        hook2 = write_hook(owned, "hook_resume.py", HOOK_RESUME_SOURCE)
        settings2 = write_settings(owned, "settings_resume.json", hook2)
        argv2 = [exe, "--verbose", "--resume", identity["session_id"],
                 "--settings", settings2, "--setting-sources", "",
                 "--output-format", "stream-json",
                 "--include-partial-messages"]
        outcome2 = runner.run(argv2, cwd=owned, env=None,
                              timeout_s=PHASE_TIMEOUT_S)
        say(run_name + ": resume exit=" + outcome2.exit_enum
            + " cleanup=" + outcome2.cleanup_enum)
        if outcome2.exit_enum == "timeout":
            raise ProbeFailure("timeout")

        resumed = read_private_json(
            os.path.join(owned, "resume_capture.json"), "resume_join_failed")
        resume_join = compare_identity(identity, resumed)
        if not all(resume_join.values()):
            raise ProbeFailure("resume_join_failed")
        say(run_name + ": resume join complete")

        denial = match_denial(identity, list(iter_json_lines(outcome2.stdout)))
        say(run_name + ": denial matched unique=True provider_input_present="
            + str(denial["provider_input_present"]))

        if os.path.exists(marker_path):
            raise ProbeFailure("marker_violation")
        marker_absent_post = True

        raw_parts = [outcome1.stdout, outcome2.stdout]
        for name in ("defer_capture.json", "expected_identity.json",
                     "resume_capture.json"):
            p = os.path.join(owned, name)
            with open(p, "rb") as f:
                raw_parts.append(f.read())
        raw_capture_sha256 = framed_digest(raw_parts)

        proj = {
            "schema": SCHEMA,
            "pinned_version": facts["version"],
            "pinned_sha256": facts["sha256"],
            "architecture": facts["arch"],
            "executable_locator": pin.locator,
            "run": run_name,
            "result_top_fields": project_fields(denial["event"]),
            "denial_entry_top_fields": project_fields(denial["entry"]),
            "denial_discriminator": DENIAL_DISCRIMINATOR,
            "denial_match_unique": denial["unique"],
            "provider_input_present": denial["provider_input_present"],
            "provider_input_digest_equal": denial["digest_equal"],
            "defer_join_session_equal": defer_join["session"],
            "defer_join_tool_use_equal": defer_join["tool_use"],
            "defer_join_tool_name_equal": defer_join["tool_name"],
            "defer_join_input_digest_equal": defer_join["input_digest"],
            "resume_join_session_equal": resume_join["session"],
            "resume_join_tool_use_equal": resume_join["tool_use"],
            "resume_join_tool_name_equal": resume_join["tool_name"],
            "resume_join_input_digest_equal": resume_join["input_digest"],
            "marker_command_bound": marker_command_bound,
            "marker_absent_pre": marker_absent_pre,
            "marker_absent_post": marker_absent_post,
            "initial_exit": outcome1.exit_enum,
            "resume_exit": outcome2.exit_enum,
            "initial_cleanup": outcome1.cleanup_enum,
            "resume_cleanup": outcome2.cleanup_enum,
            "raw_capture_sha256": raw_capture_sha256,
        }
        return validate_projection(proj)
    finally:
        shutil.rmtree(owned, ignore_errors=True)
        if os.path.exists(owned):
            # Do not mask an in-flight failure; the run already fails.
            if sys.exc_info()[0] is None:
                raise ProbeFailure("raw_delete_failed")
            say("FAIL raw_delete_failed")


# --- Packet B entry -----------------------------------------------------------

def stability_strip(proj):
    return {k: v for k, v in proj.items() if k not in STRIP_FOR_STABILITY}


def run_packet_b(pin, runner, out_a, out_b, orchestrate=run_live_once):
    """Exactly two live runs. A projection file exists only on full success."""
    try:
        proj_a = orchestrate("run-a", pin, runner)
        proj_b = orchestrate("run-b", pin, runner)
        if stability_strip(proj_a) != stability_strip(proj_b):
            raise ProbeFailure("unstable_structure")
        text_a = json.dumps(validate_projection(proj_a), indent=2,
                            sort_keys=True)
        text_b = json.dumps(validate_projection(proj_b), indent=2,
                            sort_keys=True)
        with open(out_a, "w") as f:
            f.write(text_a + "\n")
        with open(out_b, "w") as f:
            f.write(text_b + "\n")
        say("run-a projection:")
        say(text_a)
        say("run-b projection:")
        say(text_b)
        say("PACKET-B: OK")
        return 0
    except ProbeFailure as e:
        say("FAIL " + e.code)
        return 1
    except BaseException:
        say("FAIL unexpected_error")
        return 1


def main(argv):
    if len(argv) != 6 or argv[1] != "--live" or argv[2] != "--out-a" \
            or argv[4] != "--out-b":
        say("usage: --live --out-a <path> --out-b <path>")
        say("refusing to run live without explicit --live")
        return 2
    return run_packet_b(LIVE_PIN, GroupRunner(), argv[3], argv[5])


if __name__ == "__main__":
    sys.exit(main(sys.argv))
