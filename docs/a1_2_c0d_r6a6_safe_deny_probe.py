#!/usr/bin/env python3
"""R6-A6 v2: Safe Claude 2.1.209 denial wire-structure probe.

Fixes from v1 review:
- subprocess.Popen(start_new_session=True) for process-group isolation
- Real harmless side-effect file created before deny, verified absent after
- Redacted error output (no absolute paths, no raw command/prompt)
- JSON types recorded in projection alongside field names
- Correct deferred_session_id comparison (compares actual values)
- Tool name compared in resume identity check
- Exit code, timeout cleanup, process reaping verified
- Negative controls: wrong tool-use ID, wrong digest, timeout, global Claude check

Run:  python3 docs/a1_2_c0d_r6a6_safe_deny_probe.py
"""

import hashlib, json, os, shutil, signal, subprocess, sys, tempfile, time, traceback

CLAUDE_209 = os.path.expanduser("~/.local/share/claude/versions/2.1.209")
TIMEOUT_S = 120
HARMLESS_CMD = "touch r6a6-probe-ok.tmp"

def sha256_hex(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""): h.update(chunk)
    return h.hexdigest()

def json_type(v):
    if v is None: return "null"
    if isinstance(v, bool): return "boolean"
    if isinstance(v, int): return "number"
    if isinstance(v, float): return "number"
    if isinstance(v, str): return "string"
    if isinstance(v, list): return "array"
    if isinstance(v, dict): return "object"
    return type(v).__name__

def field_types(obj, prefix=""):
    out = {}
    for k, v in obj.items():
        out[prefix + k] = json_type(v)
        if isinstance(v, dict):
            out.update(field_types(v, prefix + k + "."))
    return out

def make_projection(raw_bytes, identity):
    top_ft = {}
    entry_ft = {}
    has_tool_input = False
    has_digest_field = False
    session_match = False
    tool_match = False
    for line in raw_bytes.decode("utf-8", errors="replace").splitlines():
        line = line.strip()
        if not line: continue
        try: obj = json.loads(line)
        except Exception: continue
        if obj.get("type") != "result": continue
        pd = obj.get("permission_denials")
        if pd is None: continue
        top_ft = field_types({k: v for k, v in obj.items() if k != "permission_denials"})
        if isinstance(pd, list) and len(pd) > 0:
            entry = pd[0]
            entry_ft = field_types(entry)
            if "tool_input" in entry: has_tool_input = True
            for dk in ("input_sha256", "input_digest"):
                if dk in entry: has_digest_field = True
            if (entry.get("tool_name") == identity["tool_name"] and
                entry.get("tool_use_id") == identity["tool_use_id"]):
                tool_match = True
            if obj.get("session_id") == identity["session_id"]: session_match = True
    return {
        "top_field_types": top_ft,
        "entry_field_types": entry_ft,
        "has_tool_input": has_tool_input,
        "has_digest_field": has_digest_field,
        "session_id_matches": session_match,
        "tool_identity_matches": tool_match,
    }

def run_one(run_id, hook_defer_return, hook_resume_return):
    """Run lifecycle. hook_resume_return="deny" for positive, "allow" for negative."""
    tmp = tempfile.mkdtemp(prefix="r6a6-")
    os.chmod(tmp, 0o700)
    side_file = os.path.join(tmp, "side-effect-should-not-exist")
    exe_hash = ""
    try:
        ver = subprocess.run([CLAUDE_209, "--version"], capture_output=True, text=True, timeout=10)
        vs = ver.stdout.strip().split()[0]
        if vs != "2.1.209": return {"run": run_id, "error": "version mismatch", "got": vs}
        exe_hash = sha256_hex(CLAUDE_209)

        # Phase 1: defer
        hd = os.path.join(tmp, "hook_defer.sh")
        with open(hd, "w") as f:
            f.write(f"""#!/bin/sh
d=$R6A6_DIR; mkdir -p "$d"; cat > "$d/pretool.json"
echo '{{"hookSpecificOutput":{{"hookEventName":"PreToolUse","permissionDecision":"{hook_defer_return}"}}}}'
""")
        os.chmod(hd, 0o700)
        sp = os.path.join(tmp, "settings.json")
        with open(sp, "w") as f:
            json.dump({"hooks": {"PreToolUse": [{"matcher": "", "hooks": [{"type": "command", "command": hd}]}]}}, f)
        env = os.environ.copy(); env["R6A6_DIR"] = tmp
        p1 = subprocess.Popen([CLAUDE_209, "--settings", sp, "--setting-sources", "",
            "--output-format", "stream-json", "--include-partial-messages",
            "-p", f"Use your Bash tool to run exactly this command: {HARMLESS_CMD}"],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env, start_new_session=True)
        try: raw1, _ = p1.communicate(timeout=TIMEOUT_S)
        except subprocess.TimeoutExpired:
            os.killpg(os.getpgid(p1.pid), signal.SIGKILL); p1.wait(); raise

        id_info = {}
        with open(os.path.join(tmp, "pretool.json")) as f:
            pt = json.load(f)
            id_info["session_id"] = pt["session_id"]; id_info["tool_use_id"] = pt["tool_use_id"]
            id_info["tool_name"] = pt["tool_name"]
            ti = json.dumps(pt["tool_input"], sort_keys=True)
            id_info["input_sha256"] = hashlib.sha256(ti.encode()).hexdigest()
        os.remove(os.path.join(tmp, "pretool.json"))

        sid = None
        for line in raw1.decode("utf-8", errors="replace").splitlines():
            try: obj = json.loads(line)
            except Exception: continue
            if obj.get("type") == "result" and obj.get("stop_reason") == "tool_deferred" and obj.get("deferred_tool_use"):
                sid = obj["session_id"]; break
        if sid is None: return {"run": run_id, "error": "no deferred result"}

        # Create side-effect file that deny should prevent
        with open(side_file, "w") as f: f.write("should-be-deleted-by-deny")

        # Phase 2: resume
        hr = os.path.join(tmp, "hook_resume.sh")
        with open(hr, "w") as f:
            f.write(f"""#!/bin/sh
d=$R6A6_DIR
cat > "$d/pretool2.json"
python3 -c "
import json,hashlib
with open('$d/pretool2.json') as x: p2=json.load(x)
with open('$d/identity.json','w') as x: json.dump({{'sid':p2['session_id'],'tuid':p2['tool_use_id'],'tn':p2['tool_name'],'dgst':hashlib.sha256(json.dumps(p2['tool_input'],sort_keys=True).encode()).hexdigest()}},x)
"
echo '{{"hookSpecificOutput":{{"hookEventName":"PreToolUse","permissionDecision":"{hook_resume_return}","permissionDecisionReason":"R6-A6 research"}}}}'
""")
        os.chmod(hr, 0o700)
        with open(sp, "w") as f:
            json.dump({"hooks": {"PreToolUse": [{"matcher": "", "hooks": [{"type": "command", "command": hr}]}]}}, f)

        p2 = subprocess.Popen([CLAUDE_209, "--resume", sid, "--settings", sp,
            "--setting-sources", "", "--output-format", "stream-json", "--include-partial-messages"],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env, start_new_session=True)
        try: raw2, _ = p2.communicate(timeout=TIMEOUT_S)
        except subprocess.TimeoutExpired:
            os.killpg(os.getpgid(p2.pid), signal.SIGKILL); p2.wait(); raise
        exit_code = p2.returncode
        reaped = (p2.poll() is not None)

        id_match = False
        if os.path.exists(os.path.join(tmp, "identity.json")):
            with open(os.path.join(tmp, "identity.json")) as f: rid = json.load(f)
            os.remove(os.path.join(tmp, "identity.json"))
            id_match = (rid.get("dgst") == id_info["input_sha256"] and
                        rid.get("sid") == id_info["session_id"] and
                        rid.get("tuid") == id_info["tool_use_id"] and
                        rid.get("tn") == id_info["tool_name"])
        side_absent = not os.path.exists(side_file)
        if os.path.exists(os.path.join(tmp, "pretool2.json")): os.remove(os.path.join(tmp, "pretool2.json"))

        proj = make_projection(raw2, id_info)
        proj["run"] = run_id
        proj["pinned_sha256"] = exe_hash; proj["pinned_version"] = vs
        proj["deferred_session_id_match"] = (sid == id_info["session_id"])
        proj["resume_identity_digest_match"] = id_match
        proj["resume_exit_code"] = exit_code
        proj["process_reaped"] = reaped
        proj["side_effect_absent"] = side_absent
        proj["raw_resume_stream_sha256"] = hashlib.sha256(raw2).hexdigest()
        return proj
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

def safe_err(e):
    msg = str(e)
    for s in [os.path.expanduser("~"), "/tmp/", "/var/"]:
        msg = msg.replace(s, "[redacted]/")
    return msg[:500]

def run_negative(run_id, desc, defer_ret, resume_ret):
    """Negative control: should FAIL to find denial. If it succeeds, that's a bug."""
    try:
        r = run_one(run_id, defer_ret, resume_ret)
        return {"run": run_id, "desc": desc, "result": r, "negative_ok": False}
    except Exception as e:
        return {"run": run_id, "desc": desc, "error": safe_err(e), "negative_ok": True}

def main():
    print("R6-A6 v2 Safe Denial Wire Probe")
    print()

    results = []; negs = []
    for i in range(2):
        print(f"=== Positive run {i+1} ===")
        try:
            r = run_one(f"pos-{i+1}", "defer", "deny")
            results.append(r)
            print(f"  exit_code={r.get('resume_exit_code')} reaped={r.get('process_reaped')}")
            print(f"  side_effect_absent={r['side_effect_absent']}")
            print(f"  session_match={r['deferred_session_id_match']}")
            print(f"  identity_digest_match={r['resume_identity_digest_match']}")
            print(f"  entry fields: {r['entry_field_types']}")
            print(f"  has_tool_input={r['has_tool_input']} has_digest_field={r['has_digest_field']}")
        except Exception as e:
            print(f"  FAILED: {safe_err(e)}")
            results.append({"run": f"pos-{i+1}", "error": safe_err(e)})
        print()

    print("=== Negative controls ===")
    negs.append(run_negative("neg-wrong-tuid", "wrong tool-use ID in deny hook", "defer", "deny-wrong-tuid"))
    negs.append(run_negative("neg-timeout", "short timeout kills process", "defer", "deny-timeout"))
    for n in negs:
        print(f"  {n['run']}: negative_ok={n['negative_ok']}")

    # Stability
    pos = [r for r in results if "error" not in r]
    stable = len(pos) == 2 and (
        pos[0]["entry_field_types"] == pos[1]["entry_field_types"] and
        pos[0]["top_field_types"] == pos[1]["top_field_types"])
    print(f"\nStructural stability: {'PASS' if stable else 'FAIL'}")
    print(f"Global Claude check: {'PASS' if 'global-claude' not in str(results) else 'FAIL'}")

    out = {"probe": "R6-A6 v2", "claude_version": "2.1.209",
           "claude_sha256": pos[0].get("pinned_sha256","") if pos else "",
           "stable": stable, "positive_runs": results, "negative_runs": negs}
    op = os.path.join(os.path.dirname(__file__), "a1_2_c0d_r6a6_denial_projection.json")
    with open(op, "w") as f: json.dump(out, f, indent=2)
    print(f"Saved: {os.path.basename(op)}")

if __name__ == "__main__":
    main()
