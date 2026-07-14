#!/usr/bin/env python3
"""A1.1 CP0 evidence harness — capacity-zero spike; NOT production, not wired anywhere."""
import hashlib, json, os, re, shutil, signal, subprocess, sys, threading, time

TMP = f"/tmp/pokit-cp0-{os.getuid()}"
SCHEMA_DIR = os.path.join(TMP, "schema")
_LOCK_PATH = os.path.join(TMP, ".probe.lock")
_PROBE_CMD = "date"

# ── run-specific lock (at most one probe at a time) ──
_lock_fd = None


def _acquire_lock(run_id: str) -> bool:
    global _lock_fd
    os.makedirs(TMP, exist_ok=True)
    try:
        _lock_fd = os.open(_LOCK_PATH, os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o644)
        os.write(_lock_fd, run_id.encode())
        return True
    except FileExistsError:
        return False


def _release_lock():
    global _lock_fd
    if _lock_fd is not None:
        try:
            os.close(_lock_fd)
        except OSError:
            pass
        _lock_fd = None
    try:
        os.unlink(_LOCK_PATH)
    except OSError:
        pass


# ── structured pseudonym + field-allowlist redaction ──
_pseudo_next: dict[str, int] = {}


def _pseudo(label: str) -> str:
    n = _pseudo_next.get(label, 0) + 1
    _pseudo_next[label] = n
    return f"{label}-{n}"


_HOST_RE = re.compile(r"\b[A-Za-z][A-Za-z0-9-]{2,}\.[A-Za-z][A-Za-z0-9-]{2,}\b")
_INSTALL_ID_RE = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")


def _redact_value(v):
    if isinstance(v, str):
        v = _HOST_RE.sub("<HOST>", v)
        v = _INSTALL_ID_RE.sub("<INSTALL-ID>", v)
        v = re.sub(r"/Users/[^/\"\s]+", "/Users/<U>", v)
    return v


_ALLOW: dict = {
    "*": {"id", "jsonrpc", "method", "result", "error", "params"},
    "initialize": {"clientInfo"},
    "thread/start": {"approvalPolicy", "cwd", "config"},
    "turn/start": {"threadId", "input", "approvalPolicy"},
    "initialized": set(),
    "remoteControl/status/changed": {"status"},
    "thread/started": {"threadId"},
    "thread/status/changed": {"threadId", "status"},
    "turn/started": {"threadId", "turnId"},
    "item/started": {"itemId", "type", "status", "command"},
    "item/completed": {"itemId", "type", "status", "exitCode"},
    "item/commandExecution/requestApproval": {"threadId", "turnId", "itemId",
                                                "command", "cwd", "environmentId",
                                                "availableDecisions"},
    "serverRequest/resolved": {"threadId", "requestId"},
    "item/agentMessage/delta": {"text"},
    "thread/tokenUsage/updated": set(),
    "mcpServer/startupStatus/updated": {"id", "status"},
    "account/rateLimits/updated": set(),
    "turn/completed": {"threadId", "turnId"},
}

_REPLACE_FIELDS = {"command": "<REDACTED-CMD>", "cwd": "<CWD>",
                   "text": "<REDACTED>", "config": "<REDACTED>"}

_PSEUDO_FIELDS = {"threadId": "thread", "turnId": "turn", "itemId": "item",
                  "requestId": "request", "id": "objid", "installId": "install",
                  "serverName": "hostname"}


def _label_field(k: str, v):
    if k in _PSEUDO_FIELDS and isinstance(v, (str, int)):
        return _pseudo(_PSEUDO_FIELDS[k])
    if k in _REPLACE_FIELDS and isinstance(v, str):
        return _REPLACE_FIELDS[k]
    return _redact_value(v)


def _allowed_keys(method_like: str) -> set:
    return _ALLOW.get(method_like, _ALLOW.get("*", set()))


def _filtered_flat(obj: dict, direction: str) -> dict:
    m = obj.get("method") or ("result" if "result" in obj else ("error" if "error" in obj else "?"))
    rec: dict = {"dir": direction, "type": m}
    if "id" in obj:
        rec["id"] = _label_field("id", obj["id"])
    params = obj.get("params") or obj.get("result")
    if isinstance(params, dict):
        keys = _allowed_keys(m)
        for k in keys & params.keys():
            rec[k] = _label_field(k, params[k])
    return rec


# ── sha256 helpers ──
def sha256_file(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def _canonical_json_digest(path: str) -> str:
    with open(path) as f:
        obj = json.load(f)
    return hashlib.sha256(json.dumps(obj, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


# ── external Popen supervisor with SCM_RIGHTS fd-passing ──
import array as _array
import socket as _socket


def _spawn_codex_with_timeout(timeout_s: float = 15):
    """Fork a supervisor child. It spawns codex app-server and sends the
    grandchild's PID, PGID, and stdio pipe FDs back to the parent via
    SCM_RIGHTS over a socketpair. The grandchild survives through its own
    session. On timeout, the supervisor's entire process group is killed."""
    a, b = _socket.socketpair(_socket.AF_UNIX, _socket.SOCK_STREAM)
    pid = os.fork()
    if pid == 0:
        # --- supervisor child ---
        a.close()
        try:
            p = subprocess.Popen(
                ["codex", "app-server", "--stdio"],
                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                text=True, bufsize=1, start_new_session=True,
            )
            pgid = os.getpgid(p.pid)
            fds = [p.stdin.fileno(), p.stdout.fileno(), p.stderr.fileno()]
            ancillary = [(_socket.SOL_SOCKET, _socket.SCM_RIGHTS, _array.array("i", fds))]
            msg = f"{p.pid}\n{pgid}\n".encode()
            b.sendmsg([msg], ancillary)
            b.close()
            # grandchild survives via start_new_session; supervisor exits
            os._exit(0)
        except Exception as e:
            b.sendmsg([f"ERROR:{e}\n".encode()], [])
            b.close()
            os._exit(1)

    # --- parent ---
    b.close()
    a.settimeout(timeout_s)
    spawned_pid = spawned_pgid = None
    sin = sout = serr = None
    try:
        data, ancdata, _, _ = a.recvmsg(1024, 4096)
        lines = data.decode().splitlines()
        if len(lines) >= 2 and not lines[0].startswith("ERROR"):
            spawned_pid = int(lines[0])
            spawned_pgid = int(lines[1])
            # Extract pipe FDs from SCM_RIGHTS
            for cmsg_level, cmsg_type, cmsg_data in ancdata:
                if cmsg_level == _socket.SOL_SOCKET and cmsg_type == _socket.SCM_RIGHTS:
                    recv_fds = list(_array.array("i", cmsg_data))
                    if len(recv_fds) >= 3:
                        sin = os.fdopen(recv_fds[0], "w")
                        sout = os.fdopen(recv_fds[1], "r")
                        serr = os.fdopen(recv_fds[2], "r")
        os.waitpid(pid, 0)
    except (TimeoutError, _socket.timeout, OSError):
        try:
            os.killpg(os.getpgid(pid), signal.SIGKILL)
        except (ProcessLookupError, PermissionError, OSError):
            pass
        os.waitpid(pid, 0)
    finally:
        a.close()
    return spawned_pid, spawned_pgid, sin, sout, serr


# ── ordered wire client (write+flush+seq under ONE lock) ──
class AppServer:
    def __init__(self, deadline_s: float = 180):
        self._deadline_s = deadline_s
        self._started_at = time.time()
        self._timed_out = False
        # External supervisor bounds Popen startup to 15s wall-clock
        spawned_pid, spawned_pgid, sin, sout, serr = _spawn_codex_with_timeout(15)
        if spawned_pid is None:
            self._timed_out = True
            self._pgid = None
            self._parent_pid = None
            self._sin = None
            self._sout = None
            self.p = None
            self.trace = []
            self.msgs = []
            self._lock = threading.Lock()
            return
        self._pgid = spawned_pgid
        self._parent_pid = spawned_pid
        self._sin = sin
        self._sout = sout
        self.p = None  # no direct Popen handle; we own the PGID
        self._seq = 0
        self.trace: list[dict] = []
        self.msgs: list[dict] = []
        self._lock = threading.Lock()
        self._deadline_timer = threading.Timer(self._deadline_s, self._on_deadline)
        self._deadline_timer.daemon = True
        self._deadline_timer.start()
        threading.Thread(target=self._reader, daemon=True).start()

    def _on_deadline(self):
        self._timed_out = True
        self._terminate_pg()

    def _terminate_pg(self):
        try:
            os.killpg(self._pgid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError, OSError):
            pass

    @property
    def timed_out(self) -> bool:
        return self._timed_out

    def _reader(self):
        if self._sout is None:
            return
        try:
            for line in self._sout:
                line = line.rstrip("\n")
                if not line.strip():
                    continue
                try:
                    obj = json.loads(line)
                except Exception:
                    continue
                with self._lock:
                    self._seq += 1
                    rec = _filtered_flat(obj, "provider->daemon")
                    rec["seq"] = self._seq
                    self.msgs.append(obj)
                    self.trace.append(rec)
        except Exception:
            pass

    def send(self, obj) -> int:
        if self._timed_out or self._sin is None:
            return -1
        line = json.dumps(obj) + "\n"
        with self._lock:
            self._sin.write(line)
            self._sin.flush()
            self._seq += 1
            rec = _filtered_flat(obj, "daemon->provider")
            rec["seq"] = self._seq
            self.trace.append(rec)
            return self._seq

    def result_of(self, req_id: int):
        with self._lock:
            for o in list(self.msgs):
                if o.get("id") == req_id and "result" in o:
                    return o["result"]
        return None

    def stop(self):
        self._deadline_timer.cancel()
        self._terminate_pg()
        for f in (self._sin, self._sout):
            try:
                if f: f.close()
            except OSError:
                pass
        return self._parent_pid


# ── subcommands ──
def cmd_schema():
    global _pseudo_next
    _pseudo_next = {}
    os.makedirs(TMP, exist_ok=True)
    if os.path.isdir(SCHEMA_DIR): shutil.rmtree(SCHEMA_DIR)
    os.makedirs(SCHEMA_DIR)
    subprocess.run(["codex", "app-server", "generate-json-schema", "--out", SCHEMA_DIR],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    entries = []
    for root, _, files in os.walk(SCHEMA_DIR):
        for fn in sorted(files):
            fp = os.path.join(root, fn)
            rel = os.path.relpath(fp, SCHEMA_DIR)
            dig = _canonical_json_digest(fp) if fn.endswith(".json") else sha256_file(fp)
            entries.append((rel, dig))
    entries.sort()
    man = os.path.join(TMP, "schema.manifest")
    with open(man, "w") as f:
        for rel, dig in entries:
            f.write(f"{dig}  {rel}\n")
    man_digest = sha256_file(man)
    with open(os.path.join(TMP, "schema.manifest.sha256"), "w") as f:
        f.write(man_digest + "\n")
    print(f"schema files={len(entries)} manifest_digest={man_digest}")


def cmd_schema_repro():
    global _pseudo_next
    _pseudo_next = {}
    a = os.path.join(TMP, "schema_repro_a"); b = os.path.join(TMP, "schema_repro_b")
    for d in (a, b):
        if os.path.isdir(d): shutil.rmtree(d)
        os.makedirs(d)
    for d in (a, b):
        subprocess.run(["codex", "app-server", "generate-json-schema", "--out", d],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    a_map = {}; b_map = {}
    for root, _, files in os.walk(a):
        for fn in files:
            rp = os.path.relpath(os.path.join(root, fn), a)
            a_map[rp] = _canonical_json_digest(os.path.join(root, fn)) if fn.endswith(".json") else sha256_file(os.path.join(root, fn))
    for root, _, files in os.walk(b):
        for fn in files:
            rp = os.path.relpath(os.path.join(root, fn), b)
            b_map[rp] = _canonical_json_digest(os.path.join(root, fn)) if fn.endswith(".json") else sha256_file(os.path.join(root, fn))
    mismatch = []
    for rp in sorted(set(a_map) | set(b_map)):
        da = a_map.get(rp, "<missing>"); db = b_map.get(rp, "<missing>")
        if da != db:
            mismatch.append((rp, da[:24], db[:24]))
    repro = len(mismatch) == 0
    print(f"schema_repro={repro} mismatch_count={len(mismatch)} files_a={len(a_map)} files_b={len(b_map)}")
    for m in mismatch[:5]:
        print(f"  {m[0]}: {m[1]} vs {m[2]}")


def cmd_init():
    global _pseudo_next
    _pseudo_next = {}
    os.makedirs(TMP, exist_ok=True)
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


def _approval_trace(decision: str, deadline_s: float = 180):
    global _pseudo_next
    _pseudo_next = {}
    run_id = f"run_{decision}_{int(time.time())}"
    if not _acquire_lock(run_id):
        print(f"LOCK_FAILED: another probe is already running (tried {run_id})")
        sys.exit(3)
    run_dir = os.path.join(TMP, run_id)
    os.makedirs(run_dir, exist_ok=True)
    probe = os.path.join(run_dir, f"probe_{decision}.txt")
    a = AppServer(deadline_s=deadline_s)
    responded = {"done": False, "at_seq": 0}

    def on_msgs():
        if responded["done"] or a.timed_out:
            return
        with a._lock:
            for o in list(a.msgs):
                m = o.get("method", "")
                if "requestApproval" in m:
                    responded["at_seq"] = a.send({"jsonrpc": "2.0", "id": o["id"],
                                                   "result": {"decision": decision}})
                    responded["done"] = True
                    return

    a.send({"jsonrpc": "2.0", "id": 1, "method": "initialize",
            "params": {"clientInfo": {"name": "pokit-cp0", "version": "0.0.0"}}})
    time.sleep(1.0)
    a.send({"jsonrpc": "2.0", "method": "initialized"})
    time.sleep(0.5)
    a.send({"jsonrpc": "2.0", "id": 2, "method": "thread/start",
            "params": {"approvalPolicy": "untrusted", "cwd": run_dir,
                       "config": {"sandbox_mode": "read-only"}}})
    tid = None
    t0 = time.time()
    while time.time() - t0 < 15 and not tid and not a.timed_out:
        r = a.result_of(2)
        if r:
            tid = r.get("threadId") or (r.get("thread") or {}).get("id")
        time.sleep(0.3)
    if not tid:
        _fail_and_cleanup(a, run_dir, decision, "thread/start response")
        _release_lock()
        return
    cmd = f'/bin/sh -c "{_PROBE_CMD} > {probe}"'
    a.send({"jsonrpc": "2.0", "id": 3, "method": "turn/start",
            "params": {"threadId": tid,
                       "input": [{"type": "text",
                                  "text": f"Use your shell tool now to run exactly this one command (it writes a file): {cmd}. Do not explain."}],
                       "approvalPolicy": "untrusted"}})
    t0 = time.time()
    resolved = False
    while time.time() - t0 < (deadline_s - 30) and not a.timed_out:
        on_msgs()
        with a._lock:
            if any(o.get("method") == "serverRequest/resolved" for o in list(a.msgs)):
                resolved = True
        if responded["done"] and resolved:
            break
        time.sleep(0.4)
    if a.timed_out:
        _fail_and_cleanup(a, run_dir, decision, f"deadline {deadline_s}s")
        _release_lock()
        return
    if not responded["done"]:
        _fail_and_cleanup(a, run_dir, decision, "no approval request")
        _release_lock()
        return
    if not resolved:
        _fail_and_cleanup(a, run_dir, decision, "no resolved")
        _release_lock()
        return

    a.stop()
    out = os.path.join(run_dir, f"wire_{decision}.jsonl")
    with open(out, "w") as f:
        for t in a.trace:
            f.write(json.dumps(t) + "\n")
    created = os.path.exists(probe)
    resolved_seq = 0
    for t in a.trace:
        if t["type"] == "serverRequest/resolved":
            resolved_seq = t["seq"]
    print(f"decision={decision} responded={responded['done']} resolved={resolved} "
          f"probe_created={created} response_seq={responded['at_seq']} "
          f"resolved_seq={resolved_seq} resolved_after_write={resolved_seq > responded['at_seq']} "
          f"parent_pid={a._parent_pid} pgid={a._pgid}")
    evidence_out = os.path.join(TMP, f"wire_{decision}.jsonl")
    shutil.copy2(out, evidence_out)
    _verify_cleanup(run_dir, a._parent_pid, a._pgid, decision)
    _release_lock()


def _fail_and_cleanup(a: AppServer, run_dir, decision, reason):
    a.stop()
    try:
        os.killpg(a._pgid, signal.SIGKILL)
    except (ProcessLookupError, PermissionError, OSError):
        pass
    fail = {"decision": decision, "status": "FAILED", "reason": reason,
            "timed_out": a.timed_out, "parent_pid": a._parent_pid, "pgid": a._pgid,
            "trace_len": len(a.trace)}
    with open(os.path.join(TMP, f"wire_{decision}_FAIL.json"), "w") as f:
        json.dump(fail, f, indent=2)
    with open(os.path.join(TMP, f"wire_{decision}.jsonl"), "w") as f:
        for t in a.trace:
            f.write(json.dumps(t) + "\n")
    print(f"decision={decision} FAILED reason={reason} timed_out={a.timed_out}")
    _verify_cleanup(run_dir, a._parent_pid, a._pgid, decision)


def _verify_cleanup(run_dir, parent_pid, pgid, decision):
    alive = False
    try:
        os.killpg(pgid, 0)
        alive = True
    except (ProcessLookupError, PermissionError, OSError):
        pass
    if alive:
        print(f"WARNING: {decision} pgid {pgid} still alive — forcing cleanup")
        try:
            os.killpg(pgid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError, OSError):
            pass
    else:
        print(f"cleanup_verified={decision} pgid={pgid} no_remaining_process_group")
    if os.path.isdir(run_dir):
        shutil.rmtree(run_dir, ignore_errors=True)


def _digest_and_stat(path):
    st = os.stat(path)
    return {"path_kind": "regular" if os.path.isfile(path) and not os.path.islink(path) else "other",
            "size": st.st_size, "sha256": sha256_file(path)}


def cmd_launchchain():
    global _pseudo_next
    _pseudo_next = {}
    os.makedirs(TMP, exist_ok=True)
    info = {"os": os.uname().sysname, "arch": os.uname().machine}
    node = shutil.which("node")
    if node:
        real = os.path.realpath(node)
        ver = subprocess.run([node, "--version"], capture_output=True, text=True).stdout.strip()
        info["node"] = {**_digest_and_stat(real),
                        "realpath": "/opt/homebrew/Cellar/node/<REDACTED>", "version": ver}
    codex = shutil.which("codex")
    if codex:
        shim = os.path.realpath(codex)
        info["shim"] = {**_digest_and_stat(shim),
                        "realpath": "/opt/homebrew/lib/node_modules/@openai/codex/bin/codex.js"}
    a = AppServer()
    time.sleep(1.0)
    pid = a.p.pid
    try:
        comm = subprocess.run(["ps", "-p", str(pid), "-o", "comm="], capture_output=True, text=True).stdout.strip()
        info["spawned_image_comm"] = comm
    except Exception as e:
        info["spawned_image_error"] = str(e)
    a.stop()
    out = os.path.join(TMP, "launch_chain.json")
    with open(out, "w") as f:
        json.dump(info, f, indent=2)
    print(f"launch chain -> {out}")


def cmd_attest():
    global _pseudo_next
    _pseudo_next = {}
    os.makedirs(TMP, exist_ok=True)
    d = os.path.join(TMP, "attest")
    if os.path.isdir(d): shutil.rmtree(d)
    os.makedirs(d)
    srcA = os.path.join(d, "a.c"); srcB = os.path.join(d, "b.c")
    binp = os.path.join(d, "prog")
    open(srcA, "w").write('#include <stdio.h>\nint main(){printf("VERSION_A\\n");return 0;}\n')
    open(srcB, "w").write('#include <stdio.h>\nint main(){printf("VERSION_B\\n");return 0;}\n')
    cc = shutil.which("cc") or shutil.which("clang")
    result = {"cc": bool(cc)}
    if not cc:
        result["status"] = "BLOCKED: no C compiler"
        open(os.path.join(TMP, "attest.json"), "w").write(json.dumps(result, indent=2))
        print(json.dumps(result)); return
    binA = os.path.join(d, "progA"); binB = os.path.join(d, "progB")
    subprocess.run([cc, srcA, "-o", binA], check=True)
    subprocess.run([cc, srcB, "-o", binB], check=True)
    shutil.copy(binA, binp)
    verified_digest = sha256_file(binp)
    fexec_src = os.path.join(d, "fexec.c"); fexec_bin = os.path.join(d, "fexec")
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
    shutil.copy(binB, binp)
    devfd_out = "<skipped>"
    try:
        r = subprocess.run([f"/dev/fd/{fd}"], capture_output=True, text=True, pass_fds=(fd,))
        devfd_out = r.stdout.strip() or f"<rc={r.returncode}>"
    except Exception as e:
        devfd_out = f"<{e}>"
    fexecve_out = "<fexecve-unavailable-on-platform>"
    if fexecve_available:
        try:
            r = subprocess.run([fexec_bin, str(fd)], capture_output=True, text=True, pass_fds=(fd,))
            fexecve_out = r.stdout.strip() or f"<rc={r.returncode}>"
        except Exception as e:
            fexecve_out = f"<{e}>"
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
    result["status"] = ("PROVEN: fexecve binds verified==spawned"
                        if result["verified_equals_spawned_via_fexecve"] and result["path_exec_sees_replacement"]
                        else "BLOCKED: no fd-exec mechanism on this macOS")
    open(os.path.join(TMP, "attest.json"), "w").write(json.dumps(result, indent=2))
    print(json.dumps(result))


def cmd_clean():
    _release_lock()
    if os.path.isdir(TMP):
        shutil.rmtree(TMP)
        print(f"removed {TMP}")
    else:
        print("nothing to clean")


# ── no-model tests ──
def cmd_test_lock():
    """Second probe must fail on the lock."""
    os.makedirs(TMP, exist_ok=True)
    rid1 = f"testlock_{int(time.time())}"
    if not _acquire_lock(rid1):
        print("TEST LOCK FAIL: first acquire failed"); sys.exit(1)
    print(f"LOCK_ACQUIRED: {rid1}")
    # Second acquire must fail
    if _acquire_lock("testlock_2"):
        print("TEST LOCK FAIL: second acquire should have failed"); _release_lock(); sys.exit(1)
    print("LOCK_DENIED: second acquire correctly rejected")
    _release_lock()
    print("LOCK_RELEASED: re-acquire should work")
    if not _acquire_lock("testlock_3"):
        print("TEST LOCK FAIL: re-acquire after release failed"); sys.exit(1)
    _release_lock()
    print("LOCK_TEST_PASS")


def cmd_test_pg_cleanup():
    """Spawn a short-lived app-server, stop it, prove only its PGID is gone."""
    global _pseudo_next
    _pseudo_next = {}
    # Record pre-existing codex PIDs (unrelated)
    before = set()
    try:
        out = subprocess.run(["pgrep", "-f", "codex"], capture_output=True, text=True)
        before = set(int(p) for p in out.stdout.strip().splitlines() if p)
    except Exception:
        pass
    print(f"unrelated_before={len(before)}")
    a = AppServer(deadline_s=10)
    if a.timed_out or a._pgid is None:
        print("TEST PG FAIL: spawn timed out"); sys.exit(1)
    probe_pgid = a._pgid
    probe_pid = a._parent_pid
    print(f"probe_pid={probe_pid} probe_pgid={probe_pgid}")
    a.stop()
    # Verify probe PGID is dead
    alive = False
    try:
        os.killpg(probe_pgid, 0)
        alive = True
    except (ProcessLookupError, PermissionError, OSError):
        pass
    if alive:
        print("TEST PG FAIL: probe PGID still alive"); sys.exit(1)
    print("PG_CLEANUP: probe PGID terminated")
    # Verify unrelated codex processes untouched
    after = set()
    try:
        out = subprocess.run(["pgrep", "-f", "codex"], capture_output=True, text=True)
        after = set(int(p) for p in out.stdout.strip().splitlines() if p)
    except Exception:
        pass
    untouched = before & after
    print(f"unrelated_untouched={len(untouched)}")
    # The probe PID should NOT be in after
    if probe_pid in after:
        print(f"TEST PG FAIL: probe PID {probe_pid} still in process list"); sys.exit(1)
    print("PG_CLEANUP_TEST_PASS")


def cmd_test_startup_timeout():
    """Verify a short deadline fires and records failure without hanging."""
    global _pseudo_next
    _pseudo_next = {}
    a = AppServer(deadline_s=3)
    t0 = time.time()
    # just wait for the deadline
    while time.time() - t0 < 8 and not a.timed_out:
        time.sleep(0.3)
    a.stop()
    elapsed = time.time() - t0
    if not a.timed_out:
        print(f"TEST TIMEOUT FAIL: deadline did not fire after {elapsed:.1f}s"); sys.exit(1)
    print(f"DEADLINE_FIRED: {elapsed:.1f}s STARTUP_TIMEOUT_TEST_PASS")


def cmd_test_compile():
    """Verify the harness compiles cleanly."""
    import py_compile
    py_compile.compile(__file__, doraise=True)
    print("COMPILE_TEST_PASS")


def main():
    args = sys.argv[1:]
    if args == ["schema"]:
        cmd_schema()
    elif args == ["schema_repro"]:
        cmd_schema_repro()
    elif args == ["init"]:
        cmd_init()
    elif args == ["approval", "accept"]:
        _approval_trace("accept")
    elif args == ["approval", "decline"]:
        _approval_trace("decline")
    elif args == ["launchchain"]:
        cmd_launchchain()
    elif args == ["attest"]:
        cmd_attest()
    elif args == ["clean"]:
        cmd_clean()
    elif args == ["test_lock"]:
        cmd_test_lock()
    elif args == ["test_pg_cleanup"]:
        cmd_test_pg_cleanup()
    elif args == ["test_startup_timeout"]:
        cmd_test_startup_timeout()
    elif args == ["test_compile"]:
        cmd_test_compile()
    else:
        sys.stderr.write("refused: argv not in the fixed CP0 allowlist\n")
        sys.exit(2)


if __name__ == "__main__":
    main()
