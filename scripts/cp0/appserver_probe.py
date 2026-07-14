#!/usr/bin/env python3
"""A1.1 CP0 evidence harness (capacity-zero spike; NOT production, not wired anywhere).

Fixed, reviewable behavior only. Argv is a closed allowlist. All output goes to one
dedicated temp dir. The only command driven into the provider is a fixed harmless
`date > <tmp>/probe_<decision>.txt`. No credential or prompt is passed in argv (codex
uses its ambient login). Redacts UUID/host/user-path/email from every artifact.

Usage (closed allowlist):
  appserver_probe.py schema
  appserver_probe.py init
  appserver_probe.py approval accept
  appserver_probe.py approval decline
  appserver_probe.py launchchain
  appserver_probe.py attest
  appserver_probe.py clean
"""
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import threading
import time

TMP = f"/tmp/pokit-cp0-{os.getuid()}"
SCHEMA_DIR = os.path.join(TMP, "schema")

_UUID = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")


def redact(s: str) -> str:
    s = re.sub(r"/Users/[^/\"\s]+", "/Users/<U>", s)
    s = re.sub(r"[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+", "<EMAIL>", s)
    s = _UUID.sub("<UUID>", s)
    s = re.sub(r'"serverName":"[^"]*"', '"serverName":"<HOST>"', s)
    s = re.sub(r'"installationId":"[^"]*"', '"installationId":"<UUID>"', s)
    return s


def sha256_file(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def ensure_tmp():
    os.makedirs(TMP, exist_ok=True)


class AppServer:
    """Minimal ordered JSON-RPC stdio client over `codex app-server --stdio`."""

    def __init__(self):
        self.p = subprocess.Popen(
            ["codex", "app-server", "--stdio"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, bufsize=1,
        )
        self.seq = 0
        self.trace = []
        self.msgs = []
        self.lock = threading.Lock()
        threading.Thread(target=self._reader, daemon=True).start()

    def _rec(self, direction, obj):
        with self.lock:
            self.seq += 1
            typ = obj.get("method") or ("result" if "result" in obj else "error" if "error" in obj else "?")
            payload = json.loads(redact(json.dumps(obj)))
            self.trace.append({"seq": self.seq, "dir": direction, "type": typ, "payload": payload})

    def _reader(self):
        for line in self.p.stdout:
            line = line.rstrip("\n")
            if not line.strip():
                continue
            try:
                obj = json.loads(line)
            except Exception:
                continue
            self.msgs.append(obj)
            self._rec("provider->daemon", obj)

    def send(self, obj):
        self._rec("daemon->provider", obj)
        with self.lock:
            self.p.stdin.write(json.dumps(obj) + "\n")
            self.p.stdin.flush()

    def result_of(self, req_id):
        for o in list(self.msgs):
            if o.get("id") == req_id and "result" in o:
                return o["result"]
        return None

    def stop(self):
        try:
            self.p.terminate()
            self.p.wait(timeout=3)
        except Exception:
            self.p.kill()
        return self.p.pid


def cmd_schema():
    ensure_tmp()
    if os.path.isdir(SCHEMA_DIR):
        shutil.rmtree(SCHEMA_DIR)
    os.makedirs(SCHEMA_DIR)
    subprocess.run(["codex", "app-server", "generate-json-schema", "--out", SCHEMA_DIR], check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    entries = []
    for root, _, files in os.walk(SCHEMA_DIR):
        for fn in files:
            fp = os.path.join(root, fn)
            rel = os.path.relpath(fp, SCHEMA_DIR)
            entries.append((rel, sha256_file(fp)))
    entries.sort()
    man = os.path.join(TMP, "schema.manifest")
    with open(man, "w") as f:
        for rel, dig in entries:
            f.write(f"{dig}  {rel}\n")
    man_digest = sha256_file(man)
    with open(os.path.join(TMP, "schema.manifest.sha256"), "w") as f:
        f.write(man_digest + "\n")
    print(f"schema files={len(entries)} manifest_digest={man_digest}")


def cmd_init():
    ensure_tmp()
    a = AppServer()
    a.send({"jsonrpc": "2.0", "id": 1, "method": "initialize",
            "params": {"clientInfo": {"name": "pokit-cp0", "version": "0.0.0"}}})
    time.sleep(1.5)
    a.send({"jsonrpc": "2.0", "method": "initialized"})
    time.sleep(1.0)
    a.stop()
    out = os.path.join(TMP, "init.jsonl")
    with open(out, "w") as f:
        for t in a.trace:
            f.write(json.dumps(t) + "\n")
    print(f"init trace lines={len(a.trace)} -> {out}")


def cmd_approval(decision):
    ensure_tmp()
    probe = os.path.join(TMP, f"probe_{decision}.txt")
    if os.path.exists(probe):
        os.remove(probe)
    a = AppServer()
    responded = {"done": False}

    def on_msgs():
        for o in list(a.msgs):
            m = o.get("method", "")
            if "id" in o and "requestApproval" in m and not responded["done"]:
                a.send({"jsonrpc": "2.0", "id": o["id"], "result": {"decision": decision}})
                responded["done"] = True

    a.send({"jsonrpc": "2.0", "id": 1, "method": "initialize",
            "params": {"clientInfo": {"name": "pokit-cp0", "version": "0.0.0"}}})
    time.sleep(1.0)
    a.send({"jsonrpc": "2.0", "method": "initialized"})
    time.sleep(0.5)
    a.send({"jsonrpc": "2.0", "id": 2, "method": "thread/start",
            "params": {"approvalPolicy": "untrusted", "cwd": TMP, "config": {"sandbox_mode": "read-only"}}})
    tid = None
    t0 = time.time()
    while time.time() - t0 < 10 and not tid:
        r = a.result_of(2)
        if r:
            tid = r.get("threadId") or (r.get("thread") or {}).get("id")
        time.sleep(0.3)
    if tid:
        cmd = f'/bin/sh -c "date > {probe}"'
        a.send({"jsonrpc": "2.0", "id": 3, "method": "turn/start",
                "params": {"threadId": tid,
                           "input": [{"type": "text",
                                      "text": f"Use your shell tool now to run exactly this one command (it writes a file so it needs approval): {cmd}. Do not explain."}],
                           "approvalPolicy": "untrusted"}})
    t0 = time.time()
    resolved = False
    while time.time() - t0 < 100:
        on_msgs()
        if any(o.get("method") == "serverRequest/resolved" for o in list(a.msgs)):
            resolved = True
        if responded["done"] and resolved:
            break
        time.sleep(0.4)
    a.stop()
    out = os.path.join(TMP, f"wire_{decision}.jsonl")
    with open(out, "w") as f:
        for t in a.trace:
            f.write(json.dumps(t) + "\n")
    created = os.path.exists(probe)
    print(f"decision={decision} responded={responded['done']} resolved={resolved} "
          f"probe_created={created} trace_lines={len(a.trace)} -> {out}")


def _digest_and_stat(path):
    st = os.stat(path)
    return {"path_kind": "regular" if os.path.isfile(path) and not os.path.islink(path) else "other",
            "size": st.st_size, "sha256": sha256_file(path)}


def cmd_launchchain():
    ensure_tmp()
    info = {"os": os.uname().sysname, "arch": os.uname().machine}
    node = shutil.which("node")
    if node:
        real = os.path.realpath(node)
        ver = subprocess.run([node, "--version"], capture_output=True, text=True).stdout.strip()
        info["node"] = {**_digest_and_stat(real), "realpath": redact(real), "version": ver}
    codex = shutil.which("codex")
    if codex:
        shim = os.path.realpath(codex)
        info["shim"] = {**_digest_and_stat(shim), "realpath": redact(shim), "is_symlink_on_path": os.path.islink(codex)}
    a = AppServer()
    time.sleep(1.0)
    pid = a.p.pid
    try:
        comm = subprocess.run(["ps", "-p", str(pid), "-o", "comm="], capture_output=True, text=True).stdout.strip()
        info["spawned_image_path"] = redact(comm)
        if comm and os.path.exists(comm):
            info["spawned_image"] = _digest_and_stat(comm)
    except Exception as e:
        info["spawned_image_error"] = str(e)
    a.stop()
    out = os.path.join(TMP, "launch_chain.json")
    with open(out, "w") as f:
        json.dump(info, f, indent=2)
    print(f"launch chain -> {out}")


def cmd_attest():
    """Demonstrate the macOS verified==spawned primitive with a deterministic adversarial
    replacement, using a tiny compiled test binary (NOT the 260MB codex native). Proves
    whether exec-from-open-fd runs the verified bytes after the on-disk path is replaced.
    """
    ensure_tmp()
    d = os.path.join(TMP, "attest")
    if os.path.isdir(d):
        shutil.rmtree(d)
    os.makedirs(d)
    srcA = os.path.join(d, "a.c"); srcB = os.path.join(d, "b.c")
    binp = os.path.join(d, "prog")
    open(srcA, "w").write('#include <stdio.h>\nint main(){printf("VERSION_A\\n");return 0;}\n')
    open(srcB, "w").write('#include <stdio.h>\nint main(){printf("VERSION_B\\n");return 0;}\n')
    cc = shutil.which("cc") or shutil.which("clang")
    result = {"cc": bool(cc)}
    if not cc:
        result["status"] = "BLOCKED: no C compiler to build the attestation test binary"
        open(os.path.join(TMP, "attest.json"), "w").write(json.dumps(result, indent=2))
        print(json.dumps(result)); return
    binA = os.path.join(d, "progA"); binB = os.path.join(d, "progB")
    subprocess.run([cc, srcA, "-o", binA], check=True)
    subprocess.run([cc, srcB, "-o", binB], check=True)
    shutil.copy(binA, binp)
    verified_digest = sha256_file(binp)
    # fexecve helper: exec exactly the bytes referenced by an open fd, even after the
    # on-disk path is replaced. This is the canonical POSIX primitive for
    # verified==spawned (macOS supports fexecve; Python does not wrap it).
    fexec_src = os.path.join(d, "fexec.c")
    fexec_bin = os.path.join(d, "fexec")
    open(fexec_src, "w").write(
        '#include <unistd.h>\n#include <stdlib.h>\n#include <stdio.h>\n'
        'extern char **environ;\n'
        'int main(int argc,char**argv){int fd=atoi(argv[1]);char*a[]={"prog",0};'
        'fexecve(fd,a,environ);perror("fexecve");return 3;}\n')
    fexecve_available = subprocess.run([cc, fexec_src, "-o", fexec_bin],
                                       capture_output=True, text=True).returncode == 0
    result["fexecve_available_on_platform"] = fexecve_available
    fd = os.open(binp, os.O_RDONLY)
    os.set_inheritable(fd, True)
    os.remove(binp)
    shutil.copy(binB, binp)  # path now holds different bytes (VERSION_B)
    # (a) exec via /dev/fd (often restricted on macOS)
    devfd = f"/dev/fd/{fd}"
    try:
        r = subprocess.run([devfd], capture_output=True, text=True, pass_fds=(fd,))
        devfd_out = r.stdout.strip() or f"<rc={r.returncode} err={r.stderr.strip()[:60]}>"
    except Exception as e:
        devfd_out = f"<exec-fd-failed: {e}>"
    # (b) exec via fexecve (canonical POSIX; may be absent on macOS)
    if fexecve_available:
        try:
            r = subprocess.run([fexec_bin, str(fd)], capture_output=True, text=True, pass_fds=(fd,))
            fexecve_out = r.stdout.strip() or f"<rc={r.returncode} err={r.stderr.strip()[:60]}>"
        except Exception as e:
            fexecve_out = f"<fexecve-failed: {e}>"
    else:
        fexecve_out = "<fexecve-unavailable-on-platform>"
    via_path = subprocess.run([binp], capture_output=True, text=True).stdout.strip()
    os.close(fd)
    result.update({
        "verified_digest": verified_digest,
        "exec_via_devfd_output": devfd_out,
        "exec_via_fexecve_output": fexecve_out,
        "exec_via_path_output": via_path,
        "verified_equals_spawned_via_fexecve": fexecve_out == "VERSION_A",
        "verified_equals_spawned_via_devfd": devfd_out == "VERSION_A",
        "path_exec_sees_replacement": via_path == "VERSION_B",
    })
    result["status"] = ("PROVEN: fexecve binds verified==spawned across adversarial path replacement"
                        if result["verified_equals_spawned_via_fexecve"] and result["path_exec_sees_replacement"]
                        else "BLOCKED: could not bind spawned image to verified bytes on this macOS")
    open(os.path.join(TMP, "attest.json"), "w").write(json.dumps(result, indent=2))
    print(json.dumps(result))


def cmd_clean():
    if os.path.isdir(TMP):
        shutil.rmtree(TMP)
        print(f"removed {TMP}")
    else:
        print("nothing to clean")


def main():
    args = sys.argv[1:]
    if args == ["schema"]:
        cmd_schema()
    elif args == ["init"]:
        cmd_init()
    elif args == ["approval", "accept"]:
        cmd_approval("accept")
    elif args == ["approval", "decline"]:
        cmd_approval("decline")
    elif args == ["launchchain"]:
        cmd_launchchain()
    elif args == ["attest"]:
        cmd_attest()
    elif args == ["clean"]:
        cmd_clean()
    else:
        sys.stderr.write("refused: argv not in the fixed CP0 allowlist\n")
        sys.exit(2)


if __name__ == "__main__":
    main()
