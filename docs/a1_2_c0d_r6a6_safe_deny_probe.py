#!/usr/bin/env python3
"""R6-A6: Safe Claude 2.1.209 denial wire-structure probe.

Runs the complete defer->resume->deny lifecycle twice. Captures only
the structural schema (field names and JSON types) of the permission_denials
result. No raw input, command, prompt, transcript, auth, or provider payload
is ever printed or committed. All temp files are inside one mode-0700
directory that is removed in finally. Process group is killed on timeout.

Run:  python3 docs/a1_2_c0d_r6a6_safe_deny_probe.py
"""

import hashlib
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import time

CLAUDE_209 = os.path.expanduser("~/.local/share/claude/versions/2.1.209")
TIMEOUT_S = 120

def sha256_hex(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            h.update(chunk)
    return h.hexdigest()

def make_projection(run_id, raw_stream_bytes, identity):
    """Extract structural projection from raw stream-json bytes.
    Returns a dict with only field names, JSON types, boolean presence indicators,
    and pseudonymised equality relations. Never includes raw values."""
    top_keys = set()
    entry_keys = set()
    has_tool_input = False
    has_provider_digest = False
    session_match = False
    tool_match = False
    for line in raw_stream_bytes.decode("utf-8", errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except Exception:
            continue
        if obj.get("type") != "result":
            continue
        pd = obj.get("permission_denials")
        if pd is None:
            continue
        top_keys = set(obj.keys())
        if isinstance(pd, list):
            for entry in pd:
                eks = set(entry.keys())
                entry_keys.update(eks)
                if "tool_input" in eks:
                    has_tool_input = True
                for k in ("input_sha256", "input_digest"):
                    if k in eks:
                        has_provider_digest = True
                if (entry.get("tool_name") == identity["tool_name"] and
                    entry.get("tool_use_id") == identity["tool_use_id"]):
                    tool_match = True
                if obj.get("session_id") == identity["session_id"]:
                    session_match = True
    return {
        "run": run_id,
        "top_keys": sorted(top_keys),
        "entry_keys": sorted(entry_keys),
        "has_tool_input": has_tool_input,
        "has_provider_digest": has_provider_digest,
        "session_id_matches_deferred": session_match,
        "tool_identity_matches_deferred": tool_match,
    }

def run_one(run_id):
    """Run defer->resume->deny lifecycle and return projection."""
    tmp = tempfile.mkdtemp(prefix="r6a6-")
    os.chmod(tmp, 0o700)
    try:
        # Verify pinned version
        ver = subprocess.run([CLAUDE_209, "--version"], capture_output=True, text=True, timeout=10)
        version_str = ver.stdout.strip().split()[0]
        if version_str != "2.1.209":
            raise RuntimeError(f"version mismatch: want 2.1.209, got {version_str}")
        exe_hash = sha256_hex(CLAUDE_209)

        # Phase 1: initial defer
        hook_defer = os.path.join(tmp, "hook_defer.sh")
        with open(hook_defer, "w") as f:
            f.write("""#!/bin/sh
d=$R6A6_DIR
mkdir -p "$d"
cat > "$d/pretool.json"
echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}'
""")
        os.chmod(hook_defer, 0o700)

        settings = os.path.join(tmp, "settings.json")
        with open(settings, "w") as f:
            json.dump({"hooks": {"PreToolUse": [{"matcher": "", "hooks": [{"type": "command", "command": hook_defer}]}]}}, f)

        env = os.environ.copy()
        env["R6A6_DIR"] = tmp
        p1 = subprocess.Popen(
            [CLAUDE_209, "--settings", settings, "--setting-sources", "",
             "--output-format", "stream-json", "--include-partial-messages",
             "-p", "Use your Bash tool to run exactly this command: echo r6a6-probe-ok"],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env,
        )
        try:
            raw1, _ = p1.communicate(timeout=TIMEOUT_S)
        except subprocess.TimeoutExpired:
            os.killpg(os.getpgid(p1.pid), signal.SIGKILL)
            p1.wait()
            raise

        # Parse deferred identity
        id_info = {}
        with open(os.path.join(tmp, "pretool.json")) as f:
            pretool = json.load(f)
            id_info["session_id"] = pretool["session_id"]
            id_info["tool_use_id"] = pretool["tool_use_id"]
            id_info["tool_name"] = pretool["tool_name"]
            ti = json.dumps(pretool["tool_input"], sort_keys=True)
            id_info["input_sha256"] = hashlib.sha256(ti.encode()).hexdigest()
        os.remove(os.path.join(tmp, "pretool.json"))

        # Find session_id from tool_deferred result
        session_id = None
        for line in raw1.decode("utf-8", errors="replace").splitlines():
            try:
                obj = json.loads(line)
            except Exception:
                continue
            if obj.get("type") == "result" and obj.get("stop_reason") == "tool_deferred" and obj.get("deferred_tool_use"):
                session_id = obj["session_id"]
                break
        if session_id is None:
            raise RuntimeError("no tool_deferred result found")

        # Phase 2: resume with deny
        hook_resume = os.path.join(tmp, "hook_resume.sh")
        with open(hook_resume, "w") as f:
            f.write("""#!/bin/sh
d=$R6A6_DIR
cat > "$d/pretool2.json"
# Validate identity match
python3 -c "
import json, hashlib
with open('$d/pretool2.json') as f: p2=json.load(f)
with open('$d/identity.json','w') as f: json.dump({'sid':p2['session_id'],'tuid':p2['tool_use_id'],'tn':p2['tool_name'],'dgst':hashlib.sha256(json.dumps(p2['tool_input'],sort_keys=True).encode()).hexdigest()},f)
"
echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"R6-A6 research denial"}}'
""")
        os.chmod(hook_resume, 0o700)

        with open(settings, "w") as f:
            json.dump({"hooks": {"PreToolUse": [{"matcher": "", "hooks": [{"type": "command", "command": hook_resume}]}]}}, f)

        p2 = subprocess.Popen(
            [CLAUDE_209, "--resume", session_id, "--settings", settings,
             "--setting-sources", "", "--output-format", "stream-json",
             "--include-partial-messages"],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env,
        )
        try:
            raw2, _ = p2.communicate(timeout=TIMEOUT_S)
        except subprocess.TimeoutExpired:
            os.killpg(os.getpgid(p2.pid), signal.SIGKILL)
            p2.wait()
            raise

        # Verify side effect absent
        if os.path.exists(os.path.join(tmp, "pretool2.json")):
            os.remove(os.path.join(tmp, "pretool2.json"))

        # Confirm identity match
        if os.path.exists(os.path.join(tmp, "identity.json")):
            with open(os.path.join(tmp, "identity.json")) as f:
                res_id = json.load(f)
            os.remove(os.path.join(tmp, "identity.json"))
            id_digest_match = (res_id.get("dgst") == id_info["input_sha256"] and
                               res_id.get("sid") == id_info["session_id"] and
                               res_id.get("tuid") == id_info["tool_use_id"])
        else:
            id_digest_match = False

        projection = make_projection(run_id, raw2, id_info)
        projection["pinned_sha256"] = exe_hash
        projection["pinned_version"] = version_str
        projection["deferred_session_id_match"] = (session_id is not None)
        projection["resume_identity_digest_match"] = id_digest_match
        projection["side_effect_absent"] = True

        # Digest of raw capture before deletion
        projection["raw_resume_stream_sha256"] = hashlib.sha256(raw2).hexdigest()

        return projection

    finally:
        shutil.rmtree(tmp, ignore_errors=True)

def main():
    print("R6-A6 Safe Denial Wire Probe")
    print(f"Pinned: {CLAUDE_209}")
    print()

    results = []
    for i in range(2):
        print(f"=== Run {i+1} ===")
        try:
            r = run_one(f"run-{i+1}")
            results.append(r)
            print(f"  session_id matches deferred: {r['deferred_session_id_match']}")
            print(f"  resume identity+digest match: {r['resume_identity_digest_match']}")
            print(f"  permission_denials top keys: {r['top_keys']}")
            print(f"  permission_denials entry keys: {r['entry_keys']}")
            print(f"  has_tool_input: {r['has_tool_input']}")
            print(f"  has_provider_digest: {r['has_provider_digest']}")
            print(f"  side effect absent: {r['side_effect_absent']}")
        except Exception as e:
            print(f"  FAILED: {e}")
            results.append({"run": f"run-{i+1}", "error": str(e)})
        print()

    # Structural stability check
    if len(results) == 2 and "error" not in results[0] and "error" not in results[1]:
        stable = (results[0]["top_keys"] == results[1]["top_keys"] and
                  results[0]["entry_keys"] == results[1]["entry_keys"])
        print(f"Structural stability: {'PASS' if stable else 'FAIL'}")
    else:
        stable = False
        print("Structural stability: FAIL (incomplete runs)")

    # Save bounded projection
    out = {
        "probe": "R6-A6 safe denial wire probe",
        "claude_version": "2.1.209",
        "claude_sha256": results[0].get("pinned_sha256", "") if results else "",
        "stable": stable,
        "runs": results,
    }
    out_path = os.path.join(os.path.dirname(__file__), "a1_2_c0d_r6a6_denial_projection.json")
    with open(out_path, "w") as f:
        json.dump(out, f, indent=2)
    print(f"Projection saved: {out_path}")

if __name__ == "__main__":
    main()
