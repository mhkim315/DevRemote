#!/usr/bin/env python3
"""A1.1 CP0 evidence harness — capacity-zero spike; NOT production, not wired anywhere."""
import hashlib, json, os, re, shutil, signal, subprocess, sys, threading, time

TMP = f"/tmp/pokit-cp0-{os.getuid()}"
SCHEMA_DIR = os.path.join(TMP, "schema")
_LOCK_PATH = os.path.join(TMP, ".probe.lock")
_PROBE_CMD = "date"

# ── run-specific lock (at most one probe at a time) ──
_lock_fd = None
_lock_ident = None   # (st_dev, st_ino) of the lock file WE created
_lock_token = None   # bounded owner token written into the lock file


def _acquire_lock(run_id: str) -> bool:
    global _lock_fd, _lock_ident, _lock_token
    os.makedirs(TMP, exist_ok=True)
    try:
        fd = os.open(_LOCK_PATH, os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o644)
    except FileExistsError:
        return False
    try:
        token = f"{run_id} pid={os.getpid()}"[:256]
        os.write(fd, (token + "\n").encode())
        st = os.fstat(fd)
    except OSError:
        # H0-B: never leave a half-created lock behind on an exception.
        try:
            os.close(fd)
        finally:
            try:
                os.unlink(_LOCK_PATH)
            except OSError:
                pass
        raise
    _lock_fd = fd
    # H0-R1: record the identity of the inode we own so release can prove
    # the pathname still refers to OUR lock before unlinking it.
    _lock_ident = (st.st_dev, st.st_ino)
    _lock_token = token
    return True


def _release_lock():
    global _lock_fd, _lock_ident, _lock_token
    if _lock_fd is None:
        # H0-B ownership guard: we do not hold the lock, so we must never
        # unlink another run's live lock file.
        return
    fd, ident = _lock_fd, _lock_ident
    _lock_fd = None
    _lock_ident = None
    _lock_token = None
    # H0-R1: unlink the pathname ONLY when the fd identity (the inode we
    # created at acquire) still matches the current pathname identity. If
    # our path was deleted and replaced by a foreign lock, close our fd and
    # leave the foreign lock untouched.
    unlink_ok = False
    try:
        fd_st = os.fstat(fd)
        cur_st = os.stat(_LOCK_PATH)
        unlink_ok = (ident is not None
                     and (fd_st.st_dev, fd_st.st_ino) == ident
                     and (cur_st.st_dev, cur_st.st_ino) == ident)
    except OSError:
        unlink_ok = False  # path gone or fd unusable — nothing we may unlink
    try:
        os.close(fd)
    except OSError:
        pass
    if unlink_ok:
        try:
            os.unlink(_LOCK_PATH)
        except OSError:
            pass


def _with_run_lock(run_id: str, fn):
    """H0-B: acquire the run lock once and release it from ONE outer finally
    on every normal, error, timeout and exception path."""
    if not _acquire_lock(run_id):
        print(f"LOCK_FAILED: another probe is already running (tried {run_id})")
        sys.exit(3)
    try:
        return fn()
    finally:
        _release_lock()


# ── structured pseudonym + field-allowlist redaction ──
_pseudo_next: dict[str, int] = {}
_pseudo_map: dict = {}


def _reset_pseudonyms():
    global _pseudo_next, _pseudo_map
    _pseudo_next = {}
    _pseudo_map = {}


def _pseudo(label: str, value) -> str:
    """Stable per-value pseudonym: (field-domain, original-value) maps to the
    SAME token for the whole run, so request/thread/turn/item identity
    relations (e.g. resolved.requestId == request.id) remain independently
    verifiable without exposing the real values."""
    key = (label, value)
    if key not in _pseudo_map:
        n = _pseudo_next.get(label, 0) + 1
        _pseudo_next[label] = n
        _pseudo_map[key] = f"{label}-{n}"
    return _pseudo_map[key]


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
    "turn/interrupt": {"threadId", "turnId"},
    "turn/aborted": {"threadId", "turnId"},
    "turn/failed": {"threadId", "turnId"},
}

_REPLACE_FIELDS = {"command": "<REDACTED-CMD>", "cwd": "<CWD>",
                   "text": "<REDACTED>", "config": "<REDACTED>",
                   "input": "<REDACTED-INPUT>"}

# requestId shares the "objid" domain with JSON-RPC id so that
# serverRequest/resolved.requestId is comparable with the original
# request's outer id under the stable pseudonym map.
_PSEUDO_FIELDS = {"threadId": "thread", "turnId": "turn", "itemId": "item",
                  "requestId": "objid", "id": "objid", "installId": "install",
                  "serverName": "hostname"}


def _label_field(k: str, v):
    if k in _PSEUDO_FIELDS and isinstance(v, (str, int)):
        return _pseudo(_PSEUDO_FIELDS[k], v)
    if k in _REPLACE_FIELDS:
        return _REPLACE_FIELDS[k]
    if isinstance(v, (dict, list)):
        # defense in depth: nested structures never pass through verbatim
        return "<REDACTED>"
    return _redact_value(v)


def _allowed_keys(method_like: str) -> set:
    return _ALLOW.get(method_like, _ALLOW.get("*", set()))


# ── method-specific structural projectors (bounded, never copy nested) ──
def _project_available_decisions(v):
    """Preserve only the bounded decision tags; drop amendment/exec-policy
    payloads entirely."""
    if not isinstance(v, list):
        return "<REDACTED>"
    tags = []
    for e in v:
        if isinstance(e, str):
            tags.append(e)
        elif isinstance(e, dict) and len(e) == 1:
            tags.append(f"<{next(iter(e))}:PAYLOAD-DROPPED>")
        else:
            tags.append("<REDACTED-DECISION>")
    return tags


def _project_turn_start(params: dict) -> dict:
    out = {"input": "<REDACTED-INPUT>"}
    if params.get("threadId") is not None:
        out["threadId"] = _pseudo("thread", params["threadId"])
    if isinstance(params.get("approvalPolicy"), str):
        out["approvalPolicy"] = params["approvalPolicy"]
    return out


def _project_turn_started(params: dict) -> dict:
    out = {}
    if params.get("threadId") is not None:
        out["threadId"] = _pseudo("thread", params["threadId"])
    turn = params.get("turn")
    if isinstance(turn, dict) and turn.get("id") is not None:
        out["turnId"] = _pseudo("turn", turn["id"])
    return out


def _project_item(params: dict) -> dict:
    """item/started + item/completed: the item object nests id/type/status/
    command; project only bounded scalar fields, redact command."""
    out = {}
    if params.get("threadId") is not None:
        out["threadId"] = _pseudo("thread", params["threadId"])
    if params.get("turnId") is not None:
        out["turnId"] = _pseudo("turn", params["turnId"])
    item = params.get("item") if isinstance(params.get("item"), dict) else params
    if item.get("id") is not None:
        out["itemId"] = _pseudo("item", item["id"])
    # "itemType"/"itemStatus": never reuse the record's "type" key (the
    # method name) — a projected params field must not clobber it.
    for k, outk in (("type", "itemType"), ("status", "itemStatus"), ("exitCode", "exitCode")):
        v = item.get(k)
        if v is not None:
            out[outk] = v if isinstance(v, (str, int, float, bool)) else "<REDACTED>"
    if "command" in item:
        out["command"] = "<REDACTED-CMD>"
    return out


def _project_request_approval(params: dict) -> dict:
    out = {}
    for k, dom in (("threadId", "thread"), ("turnId", "turn"), ("itemId", "item")):
        if params.get(k) is not None:
            out[k] = _pseudo(dom, params[k])
    if "command" in params:
        out["command"] = "<REDACTED-CMD>"
    if "cwd" in params:
        out["cwd"] = "<CWD>"
    if isinstance(params.get("environmentId"), (str, type(None))):
        out["environmentId"] = params.get("environmentId")
    if "availableDecisions" in params:
        out["availableDecisions"] = _project_available_decisions(params["availableDecisions"])
    return out


_PROJECTORS = {
    "turn/start": _project_turn_start,
    "turn/started": _project_turn_started,
    "item/started": _project_item,
    "item/completed": _project_item,
    "item/commandExecution/requestApproval": _project_request_approval,
}


def _filtered_flat(obj: dict, direction: str) -> dict:
    m = obj.get("method") or ("result" if "result" in obj else ("error" if "error" in obj else "?"))
    rec: dict = {"dir": direction, "type": m}
    if "id" in obj:
        rec["id"] = _label_field("id", obj["id"])
    if isinstance(obj.get("error"), dict):
        # bounded refusal evidence: numeric code only, never the message text
        rec["error_code"] = obj["error"].get("code")
    params = obj.get("params") or obj.get("result")
    if isinstance(params, dict):
        proj = _PROJECTORS.get(m)
        if proj is not None:
            rec.update(proj(params))
        else:
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

# ── pinned provider executable (exact-version 0.144.1) ──
# The global PATH codex drifted (observed 0.144.4). CP0 evidence is pinned to
# exactly 0.144.1, installed in a dedicated prefix OUTSIDE the repository.
# The harness never uses PATH lookup for the provider: it executes this
# explicit absolute path and fail-closes (exit 4) when the executable is
# missing, reports another version, or any recorded artifact digest differs.
# Wording note: this is an exact-version + artifact-digest record made BEFORE
# execution — it is NOT a complete process-image attestation (CP0 gate #1
# stays BLOCKED per the risk-proportional criteria).
_PINNED_CODEX_PREFIX = os.path.expanduser("~/.pokit-cp0-toolchain")
_PINNED_CODEX_BIN = _PINNED_CODEX_PREFIX + "/node_modules/.bin/codex"
_PINNED_CODEX_SHIM = _PINNED_CODEX_PREFIX + "/node_modules/@openai/codex/bin/codex.js"
_PINNED_CODEX_NATIVE = (_PINNED_CODEX_PREFIX +
                        "/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex")
_PINNED_CODEX_VERSION = "codex-cli 0.144.1"
_PINNED_CODEX_SHIM_SHA256 = "134063e133f0b4244fa3b251acf973d4fe4b4aeeacbdc135211bf480f59f1477"
_PINNED_CODEX_NATIVE_SHA256 = "29915529b97697def1a957b0505e770aa6a45744435d62fc263e98d7619e167a"
_pinned_codex_checked = False


def _pinned_codex_bin() -> str:
    """Verify the pinned executable (version string + shim/native artifact
    digests) once per process BEFORE any provider spawn. Fail-closed."""
    global _pinned_codex_checked
    if _pinned_codex_checked:
        return _PINNED_CODEX_BIN
    try:
        out = subprocess.run([_PINNED_CODEX_BIN, "--version"],
                             capture_output=True, text=True, timeout=30)
        ver = out.stdout.strip()
    except Exception as e:
        print(f"PINNED_CODEX_VERIFY_FAILED: {e}")
        sys.exit(4)
    if out.returncode != 0 or ver != _PINNED_CODEX_VERSION:
        print(f"PINNED_CODEX_VERIFY_FAILED: version {ver!r}, require {_PINNED_CODEX_VERSION!r}")
        sys.exit(4)
    # The executed entrypoint must actually BE the verified shim: .bin/codex
    # is a symlink and must resolve to the digest-checked codex.js.
    if os.path.realpath(_PINNED_CODEX_BIN) != os.path.realpath(_PINNED_CODEX_SHIM):
        print("PINNED_CODEX_VERIFY_FAILED: .bin/codex does not resolve to the verified shim")
        sys.exit(4)
    for label, path, want in (("shim", _PINNED_CODEX_SHIM, _PINNED_CODEX_SHIM_SHA256),
                              ("native", _PINNED_CODEX_NATIVE, _PINNED_CODEX_NATIVE_SHA256)):
        try:
            got = sha256_file(path)
        except OSError as e:
            print(f"PINNED_CODEX_VERIFY_FAILED: {label} unreadable: {e}")
            sys.exit(4)
        if got != want:
            print(f"PINNED_CODEX_VERIFY_FAILED: {label} digest {got[:16]}… != pinned {want[:16]}…")
            sys.exit(4)
    _pinned_codex_checked = True
    return _PINNED_CODEX_BIN


def _pinned_codex_cmd() -> list:
    return [_pinned_codex_bin(), "app-server", "--stdio"]


def _pinned_codex_identity() -> dict:
    """Bounded identity record: pinned executable version + artifact digests
    + package-lock provenance, and the (never executed for evidence) global
    PATH version for contrast."""
    ident = {"pinned_prefix_redacted": _redact_value(_PINNED_CODEX_PREFIX),
             "pinned_bin_realpath_redacted": "/Users/<U>/.pokit-cp0-toolchain/node_modules/.bin/codex",
             "pinned_version": _PINNED_CODEX_VERSION,
             "shim_sha256": _PINNED_CODEX_SHIM_SHA256,
             "native_sha256": _PINNED_CODEX_NATIVE_SHA256}
    try:
        with open(os.path.join(_PINNED_CODEX_PREFIX, "package-lock.json")) as f:
            lk = json.load(f)
        for name, pkg in lk.get("packages", {}).items():
            if name == "node_modules/@openai/codex":
                ident["lock_resolved"] = pkg.get("resolved")
                ident["lock_integrity"] = pkg.get("integrity")
            elif name == "node_modules/@openai/codex-darwin-arm64":
                ident["lock_native_resolved"] = pkg.get("resolved")
                ident["lock_native_integrity"] = pkg.get("integrity")
    except Exception as e:
        ident["lock_error"] = str(e)
    try:
        g = subprocess.run(["codex", "--version"], capture_output=True, text=True, timeout=30)
        ident["global_path_version_not_used"] = g.stdout.strip()
    except Exception as e:
        ident["global_path_version_error"] = str(e)
    return ident


_BENIGN_CHILD_CMD = [sys.executable, "-c", "import time; time.sleep(9999)"]
_READY_FRAME_LEN = 64


def _recv_exact(sock, n: int, deadline: float) -> bytes:
    """Read exactly n bytes with a hard monotonic deadline."""
    buf = b""
    while len(buf) < n:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("deadline while reading supervisor frame")
        sock.settimeout(remaining)
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise OSError("supervisor socket closed before full frame")
        buf += chunk
    return buf


def _reap_supervisor(sv_pid: int, timeout_s: float = 5.0):
    """Reap our direct supervisor child within a bound; escalate to SIGKILL
    so the caller can never block on an unkilled hung supervisor."""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        try:
            pid, _ = os.waitpid(sv_pid, os.WNOHANG)
        except InterruptedError:
            continue
        except ChildProcessError:
            return
        if pid == sv_pid:
            return
        time.sleep(0.05)
    try:
        os.kill(sv_pid, signal.SIGKILL)
    except (ProcessLookupError, OSError):
        pass
    while True:
        try:
            os.waitpid(sv_pid, 0)
            return
        except InterruptedError:
            continue
        except (ChildProcessError, OSError):
            return


def _spawn_with_timeout(cmd, timeout_s: float = 15,
                        _fault_prehandoff_hang: bool = False, _on_ready=None):
    """Fork a supervisor child that owns its session/PG from fork until the
    grandchild exits.

    Protocol:
    1. Child does os.setsid() — creates its own session+process group.
    2. Child sends a fixed 64-byte SUPERVISOR_READY frame carrying its PGID
       (fixed size: the READY bytes can never coalesce with the SCM_RIGHTS
       message that follows on the stream socket).
    3. The parent owns the acknowledged PGID from the moment the frame
       parses (PG-ownership linearization point). Each phase has its own
       monotonic deadline; total spawn wall-clock ≤ 2×timeout_s + reap bound.
    4. Child spawns cmd WITHOUT start_new_session, so it stays in the
       supervisor's process group, then sends PID + pipe FDs via SCM_RIGHTS
       and exits.
    5. On any phase timeout or protocol failure the parent kills the
       acknowledged PGID — or only the direct supervisor PID while no PGID
       has been acknowledged — and always reaps the supervisor.

    Test seams (H0-C, no-model): _fault_prehandoff_hang blocks the
    supervisor after READY + child spawn but BEFORE the PID/FD handoff;
    _on_ready(pgid) is invoked in the parent at the ownership linearization
    point (deterministic barrier for intermediate-state assertions)."""
    a, b = _socket.socketpair(_socket.AF_UNIX, _socket.SOCK_STREAM)
    sv_pid = os.fork()
    if sv_pid == 0:
        # ── supervisor child ──
        a.close()
        os.setsid()  # own session + process group
        sv_pgid = os.getpgid(0)
        try:
            frame = f"SUPERVISOR_READY {sv_pgid}".ljust(_READY_FRAME_LEN).encode()
            b.sendmsg([frame], [])
        except OSError:
            os._exit(1)
        try:
            p = subprocess.Popen(
                cmd,
                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                text=True, bufsize=1,
            )
            if _fault_prehandoff_hang:
                while True:  # deterministic pre-handoff hang seam (H0-C)
                    time.sleep(3600)
            fds = [p.stdin.fileno(), p.stdout.fileno(), p.stderr.fileno()]
            ancillary = [(_socket.SOL_SOCKET, _socket.SCM_RIGHTS, _array.array("i", fds))]
            b.sendmsg([f"{p.pid}\n".encode()], ancillary)
            b.close()
            # Grandchild stays in our PG; supervisor exits.
            os._exit(0)
        except Exception as e:
            try:
                b.sendmsg([f"ERROR:{e}\n".encode()], [])
                b.close()
            except OSError:
                pass
            os._exit(1)

    # ── parent ──
    b.close()
    spawned_pid = spawned_pgid = None
    sin = sout = serr = None
    recv_fds: list[int] = []
    try:
        # Phase 1: fixed-size READY frame (PG-ownership linearization point).
        frame = _recv_exact(a, _READY_FRAME_LEN, time.monotonic() + timeout_s)
        parts = frame.decode().split()
        if len(parts) != 2 or parts[0] != "SUPERVISOR_READY":
            raise OSError("bad READY frame")
        spawned_pgid = int(parts[1])
        if _on_ready is not None:
            _on_ready(spawned_pgid)
        # Phase 2: PID + SCM_RIGHTS FDs, bounded separately.
        a.settimeout(timeout_s)
        data, ancdata, _, _ = a.recvmsg(1024, 4096)
        lines = data.decode().splitlines()
        if not lines or not lines[0].strip().isdigit():
            raise OSError("no PID handoff")
        pid_candidate = int(lines[0])
        for cmsg_level, cmsg_type, cmsg_data in ancdata:
            if cmsg_level == _socket.SOL_SOCKET and cmsg_type == _socket.SCM_RIGHTS:
                recv_fds.extend(_array.array("i", cmsg_data))
        if len(recv_fds) < 3:
            raise OSError("SCM_RIGHTS handoff truncated")
        # Ownership assertion: the child must belong to the acknowledged PGID.
        if os.getpgid(pid_candidate) != spawned_pgid:
            raise OSError("child escaped the acknowledged PGID")
        sin = os.fdopen(recv_fds.pop(0), "w")
        sout = os.fdopen(recv_fds.pop(0), "r")
        serr = os.fdopen(recv_fds.pop(0), "r")
        for fd in recv_fds:  # never keep undocumented extra descriptors
            try:
                os.close(fd)
            except OSError:
                pass
        recv_fds = []
        spawned_pid = pid_candidate
    except (TimeoutError, _socket.timeout, OSError, ValueError):
        # Kill only what we own: the acknowledged PG, or the bare supervisor
        # PID while no PGID has been acknowledged.
        spawned_pid = None
        if spawned_pgid is not None:
            try:
                os.killpg(spawned_pgid, signal.SIGKILL)
            except (ProcessLookupError, PermissionError, OSError):
                pass
        else:
            try:
                os.kill(sv_pid, signal.SIGKILL)
            except (ProcessLookupError, OSError):
                pass
        for fd in recv_fds:
            try:
                os.close(fd)
            except OSError:
                pass
        for f in (sin, sout, serr):
            try:
                if f:
                    f.close()
            except (OSError, ValueError):
                pass
        sin = sout = serr = None
    finally:
        _reap_supervisor(sv_pid)
        a.close()
    # Drain stderr in a background thread (never discard silently)
    if serr is not None:
        def _drain_stderr(f):
            try:
                while f.readline():
                    pass
            except Exception:
                pass
        threading.Thread(target=_drain_stderr, args=(serr,), daemon=True).start()
    return spawned_pid, spawned_pgid, sin, sout, serr


# ── ordered wire client (write+flush+seq under ONE lock) ──
# Known-bad control seam (test-only): reproduces the rejected 0f67833
# recursive acquisition — send() while holding the state lock.
_FAULT_HOLD_LOCK_ACROSS_SEND = False


class AppServer:
    def __init__(self, deadline_s: float = 180, cmd=None, _test_no_spawn: bool = False):
        # H0-B: every cleanup field exists before any operation that can
        # fail, so stop() is idempotent and safe after partial construction.
        self._deadline_s = deadline_s
        self._started_at = time.time()
        self._timed_out = False
        self._stopped = False
        self._pgid = None
        self._parent_pid = None
        self._sin = None
        self._sout = None
        self._serr = None
        self._seq = 0
        self.trace: list[dict] = []
        self.msgs: list[dict] = []
        self._responded_ids: set = set()
        self._lock = threading.Lock()
        self._deadline_timer = None
        if _test_no_spawn:
            return  # test seam (H0-C): state machinery only, no child/timer
        # External supervisor bounds Popen startup wall-clock; the provider
        # is always the pinned exact-version executable, never PATH lookup.
        spawned_pid, spawned_pgid, sin, sout, serr = _spawn_with_timeout(
            cmd or _pinned_codex_cmd(), 15)
        self._pgid = spawned_pgid  # may be set even on failure; stop() re-kills safely
        if spawned_pid is None:
            self._timed_out = True
            return
        self._parent_pid = spawned_pid
        self._sin = sin
        self._sout = sout
        self._serr = serr
        self._deadline_timer = threading.Timer(self._deadline_s, self._on_deadline)
        self._deadline_timer.daemon = True
        self._deadline_timer.start()
        threading.Thread(target=self._reader, daemon=True).start()

    def _on_deadline(self):
        self._timed_out = True
        self._terminate_pg()

    def _terminate_pg(self):
        if self._pgid is None:
            return
        try:
            os.killpg(self._pgid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError, OSError):
            pass

    @property
    def timed_out(self) -> bool:
        return self._timed_out

    def _ingest(self, obj: dict):
        """Single linearization point for inbound message + seq publication."""
        with self._lock:
            self._seq += 1
            rec = _filtered_flat(obj, "provider->daemon")
            rec["seq"] = self._seq
            self.msgs.append(obj)
            self.trace.append(rec)

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
                self._ingest(obj)
        except Exception:
            pass

    def send(self, obj) -> int:
        """Wire-publication linearization point: write + flush + seq under
        ONE lock. Fail-closed: returns -1 (publishing no seq) after timeout,
        stop or a wire failure. Must never be called while holding
        self._lock."""
        line = json.dumps(obj) + "\n"
        with self._lock:
            if self._timed_out or self._sin is None:
                return -1
            try:
                self._sin.write(line)
                self._sin.flush()
            except (OSError, ValueError):
                return -1
            self._seq += 1
            rec = _filtered_flat(obj, "daemon->provider")
            rec["seq"] = self._seq
            self.trace.append(rec)
            return self._seq

    def _claim_locked(self):
        """Caller must hold self._lock. Response-claim linearization point:
        insert the request id into _responded_ids (at most one claimant)."""
        for o in self.msgs:
            m = o.get("method", "")
            if "requestApproval" in m and o.get("id") is not None \
                    and o["id"] not in self._responded_ids:
                self._responded_ids.add(o["id"])
                return o["id"]
        return None

    def pending_approval(self):
        """Copy+claim the first unanswered approval request under the state
        lock. Never calls send() (H0-A)."""
        with self._lock:
            return self._claim_locked()

    def respond_to_approval(self, decision: str):
        """H0-A: claim under the state lock, then call send() only AFTER the
        lock is released. Returns the outbound seq, -1 on wire failure, or
        None when there is no unanswered request. Deliberately NOT an RLock:
        send() must only ever run outside the state lock."""
        if _FAULT_HOLD_LOCK_ACROSS_SEND:
            # Known-bad control (test-only): the rejected 0f67833 recursive
            # path — send() under self._lock deadlocks the response path.
            with self._lock:
                req_id = self._claim_locked()
                if req_id is None:
                    return None
                return self.send({"jsonrpc": "2.0", "id": req_id,
                                  "result": {"decision": decision}})
        req_id = self.pending_approval()
        if req_id is None:
            return None
        return self.send({"jsonrpc": "2.0", "id": req_id,
                          "result": {"decision": decision}})

    def saw_method(self, name: str) -> bool:
        with self._lock:
            return any(o.get("method") == name for o in self.msgs)

    def count_method(self, name: str) -> int:
        with self._lock:
            return sum(1 for o in self.msgs if o.get("method") == name)

    def approval_request_id(self):
        """Read-only view of the first approval request id — does NOT claim."""
        with self._lock:
            for o in self.msgs:
                if "requestApproval" in o.get("method", "") and o.get("id") is not None:
                    return o["id"]
        return None

    def approval_request_param(self, key: str):
        """Read-only param of the first approval request — does NOT claim."""
        with self._lock:
            for o in self.msgs:
                if "requestApproval" in o.get("method", ""):
                    return (o.get("params") or {}).get(key)
        return None

    def first_param(self, method: str, key: str):
        with self._lock:
            for o in self.msgs:
                if o.get("method") == method:
                    p = o.get("params") or {}
                    return p.get(key)
        return None

    def result_of(self, req_id: int):
        with self._lock:
            for o in list(self.msgs):
                if o.get("id") == req_id and "result" in o:
                    return o["result"]
        return None

    def stop(self):
        """Idempotent; safe after partial construction and after timeout.
        Cancels the timer and closes descriptors exactly once; kills only
        the acknowledged owned PG."""
        with self._lock:
            if self._stopped:
                return self._parent_pid
            self._stopped = True
            timer = self._deadline_timer
            self._deadline_timer = None
        if timer is not None:
            timer.cancel()
        self._terminate_pg()
        with self._lock:
            files = (self._sin, self._sout, self._serr)
            self._sin = self._sout = self._serr = None
        for f in files:
            try:
                if f:
                    f.close()
            except (OSError, ValueError):
                pass
        return self._parent_pid


# ── owned-PG observation helpers (observation failure ≠ silent pass) ──
def _pgrep_codex():
    """Pre-existing codex PID set, or None when the observation itself
    failed. pgrep rc 1 (no match) is a valid empty observation."""
    try:
        out = subprocess.run(["pgrep", "-f", "codex"],
                             capture_output=True, text=True, timeout=10)
    except Exception:
        return None
    if out.returncode not in (0, 1):
        return None
    try:
        return set(int(p) for p in out.stdout.split())
    except ValueError:
        return None


def _scan_pg(pgid: int):
    """ps scan of the owned PGID → (live, zombies) or None when the
    observation failed — callers must treat None as a failure."""
    try:
        out = subprocess.run(["ps", "-axo", "pid,ppid,pgid,state,comm"],
                             capture_output=True, text=True, timeout=10)
    except Exception:
        return None
    if out.returncode != 0:
        return None
    live, zombies = [], []
    for line in out.stdout.splitlines():
        parts = line.split(None, 4)
        if len(parts) < 5:
            continue
        try:
            pid, ppid, gid = int(parts[0]), int(parts[1]), int(parts[2])
        except ValueError:
            continue
        if gid != pgid:
            continue
        if parts[3].startswith("Z"):
            zombies.append((pid, ppid))
        else:
            live.append((pid, parts[3], parts[4]))
    return live, zombies


def _assert_pg_cleaned(pgid: int, timeout_s: float = 8.0):
    """Bounded poll AFTER the deterministic SIGKILL. Success only when the
    owned PG has no live and no zombie entry. Zombies that are OUR direct
    children are reaped here (harness responsibility) and never counted as
    success while unreaped; leftover zombies are UNREAPED_ZOMBIES, live
    processes are LIVE_ESCAPE — both are failures, distinguished."""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        scan = _scan_pg(pgid)
        if scan is None:
            return False, "OBSERVATION_FAILED: ps scan unavailable"
        live, zombies = scan
        for zpid, zppid in zombies:
            if zppid == os.getpid():
                try:
                    os.waitpid(zpid, os.WNOHANG)
                except (ChildProcessError, InterruptedError, OSError):
                    pass
        if not live and not zombies:
            return True, "no process in owned PG"
        time.sleep(0.1)
    scan = _scan_pg(pgid)
    if scan is None:
        return False, "OBSERVATION_FAILED: ps scan unavailable"
    live, zombies = scan
    if live:
        return False, f"LIVE_ESCAPE: {live}"
    if zombies:
        return False, f"UNREAPED_ZOMBIES: {zombies}"
    return True, "no process in owned PG"


# ── subcommands ──
def cmd_schema():
    _reset_pseudonyms()
    os.makedirs(TMP, exist_ok=True)
    if os.path.isdir(SCHEMA_DIR): shutil.rmtree(SCHEMA_DIR)
    os.makedirs(SCHEMA_DIR)
    subprocess.run([_pinned_codex_bin(), "app-server", "generate-json-schema", "--out", SCHEMA_DIR],
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


# Consumed-subset pinning (risk-proportional CP0): ONLY the protocol/request/
# response files actually used by the positive path + lifecycle traces.
_SUBSET_RE = (r"(Initialize|ThreadStart|TurnStart|ItemStarted|ItemCompleted|"
              r"CommandExecutionRequestApproval|ServerRequestResolved|"
              r"TurnCompleted|TurnInterrupt)")


def cmd_schema_subset():
    """Generate the bundle with the pinned executable and pin the consumed
    subset: per-file raw sha256 (byte identity, comparable with the
    committed 0.144.1 manifest) + canonical-JSON digest (structural)."""
    os.makedirs(TMP, exist_ok=True)
    d = os.path.join(TMP, "schema_subset_gen")
    if os.path.isdir(d):
        shutil.rmtree(d)
    os.makedirs(d)
    subprocess.run([_pinned_codex_bin(), "app-server", "generate-json-schema", "--out", d],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    rows = []
    for root, _, files in os.walk(d):
        for fn in sorted(files):
            rel = os.path.relpath(os.path.join(root, fn), d)
            if re.search(_SUBSET_RE, rel):
                fp = os.path.join(root, fn)
                rows.append((rel, sha256_file(fp),
                             _canonical_json_digest(fp) if fn.endswith(".json") else "-"))
    rows.sort()
    man = os.path.join(TMP, "schema_subset.manifest")
    with open(man, "w") as f:
        f.write("# consumed-subset manifest — pinned codex-cli 0.144.1\n")
        f.write("# columns: raw_sha256  canonical_json_sha256  relative_path\n")
        for rel, raw, canon in rows:
            f.write(f"{raw}  {canon}  {rel}\n")
    print(f"schema_subset files={len(rows)} manifest={man} manifest_sha256={sha256_file(man)}")


def cmd_schema_repro():
    _reset_pseudonyms()
    a = os.path.join(TMP, "schema_repro_a"); b = os.path.join(TMP, "schema_repro_b")
    for d in (a, b):
        if os.path.isdir(d): shutil.rmtree(d)
        os.makedirs(d)
    for d in (a, b):
        subprocess.run([_pinned_codex_bin(), "app-server", "generate-json-schema", "--out", d],
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
    _reset_pseudonyms()
    os.makedirs(TMP, exist_ok=True)
    a = AppServer()
    try:
        a.send({"jsonrpc": "2.0", "id": 1, "method": "initialize",
                "params": {"clientInfo": {"name": "pokit-cp0", "version": "0.0.0"}}})
        time.sleep(1.5)
        a.send({"jsonrpc": "2.0", "method": "initialized"})
        time.sleep(1.0)
    finally:
        a.stop()
    out = os.path.join(TMP, "init.jsonl")
    with open(out, "w") as f:
        for t in a.trace:
            f.write(json.dumps(t) + "\n")
    print(f"init trace lines={len(a.trace)} -> {out}")


def _approval_trace(decision: str, deadline_s: float = 180):
    _reset_pseudonyms()
    run_id = f"run_{decision}_{int(time.time())}"
    _with_run_lock(run_id, lambda: _approval_trace_locked(decision, deadline_s, run_id))


def _trace_header(a: "AppServer", run_id: str) -> dict:
    """Bounded per-run identity record: pinned executable identity, the
    supervisor-acknowledged spawned PID/PGID, process-start identity and the
    connection generation (this run; seq epoch starts at 1)."""
    hdr = {"type": "trace_header", "run_id": run_id,
           "connection_generation": run_id, "seq_epoch_start": 1,
           "spawned_pid": a._parent_pid, "spawned_pgid": a._pgid,
           "pinned": _pinned_codex_identity()}
    try:
        out = subprocess.run(["ps", "-p", str(a._parent_pid), "-o", "lstart="],
                             capture_output=True, text=True, timeout=10)
        hdr["process_start_identity"] = out.stdout.strip()
    except Exception as e:
        hdr["process_start_identity_error"] = str(e)
    return hdr


def _write_wire(a: "AppServer", run_dir: str, name: str, header: dict):
    out = os.path.join(run_dir, f"wire_{name}.jsonl")
    with open(out, "w") as f:
        f.write(json.dumps(header) + "\n")
        for t in a.trace:
            f.write(json.dumps(t) + "\n")
    shutil.copy2(out, os.path.join(TMP, f"wire_{name}.jsonl"))


def _start_approval_turn(a: "AppServer", run_dir: str, probe: str):
    """Shared positive-path setup: initialize/initialized, thread/start
    (untrusted + read-only sandbox), then a turn whose command must trigger a
    command-execution approval request. Returns threadId or None."""
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
        return None
    cmd = f'/bin/sh -c "{_PROBE_CMD} > {probe}"'
    a.send({"jsonrpc": "2.0", "id": 3, "method": "turn/start",
            "params": {"threadId": tid,
                       "input": [{"type": "text",
                                  "text": f"Use your shell tool now to run exactly this one command (it writes a file): {cmd}. Do not explain."}],
                       "approvalPolicy": "untrusted"}})
    return tid


def _approval_trace_locked(decision: str, deadline_s: float, run_id: str):
    run_dir = os.path.join(TMP, run_id)
    os.makedirs(run_dir, exist_ok=True)
    probe = os.path.join(run_dir, f"probe_{decision}.txt")
    a = None
    try:
        a = AppServer(deadline_s=deadline_s)
        if a.timed_out:
            _fail_and_cleanup(a, run_dir, decision, "app-server spawn timeout")
            return
        header = _trace_header(a, run_id)
        tid = _start_approval_turn(a, run_dir, probe)
        if not tid:
            _fail_and_cleanup(a, run_dir, decision, "thread/start response")
            return
        t0 = time.time()
        responded_seq = None
        resolved = False
        while time.time() - t0 < (deadline_s - 30) and not a.timed_out:
            if responded_seq is None:
                # H0-A: claim under the state lock inside respond_to_approval;
                # send() runs only after that lock is released.
                s = a.respond_to_approval(decision)
                if s is not None:
                    responded_seq = s
                if responded_seq == -1:
                    break
            if a.saw_method("serverRequest/resolved"):
                resolved = True
            if responded_seq is not None and resolved:
                break
            time.sleep(0.4)
        if a.timed_out:
            _fail_and_cleanup(a, run_dir, decision, f"deadline {deadline_s}s")
            return
        if responded_seq is None:
            _fail_and_cleanup(a, run_dir, decision, "no approval request")
            return
        if responded_seq == -1:
            _fail_and_cleanup(a, run_dir, decision, "wire failure on response write")
            return
        if not resolved:
            _fail_and_cleanup(a, run_dir, decision, "no resolved")
            return

        a.stop()
        _write_wire(a, run_dir, decision, header)
        created = os.path.exists(probe)
        resolved_seq = 0
        for t in a.trace:
            if t["type"] == "serverRequest/resolved":
                resolved_seq = t["seq"]
        print(f"decision={decision} responded=True resolved={resolved} "
              f"probe_created={created} response_seq={responded_seq} "
              f"resolved_seq={resolved_seq} resolved_after_write={resolved_seq > responded_seq} "
              f"parent_pid={a._parent_pid} pgid={a._pgid}")
        _verify_cleanup(run_dir, a._parent_pid, a._pgid, decision)
    finally:
        if a is not None:
            a.stop()  # idempotent; covers every exception path (H0-B)


# ── #6 lifecycle traces: duplicate / timeout / cancel (evidence, capacity zero) ──
def _lifecycle_trace(mode: str, deadline_s: float = 180):
    _reset_pseudonyms()
    run_id = f"run_{mode}_{int(time.time())}"
    _with_run_lock(run_id, lambda: _lifecycle_trace_locked(mode, deadline_s, run_id))


def _lifecycle_trace_locked(mode: str, deadline_s: float, run_id: str):
    run_dir = os.path.join(TMP, run_id)
    os.makedirs(run_dir, exist_ok=True)
    probe = os.path.join(run_dir, f"probe_{mode}.txt")
    a = None
    try:
        a = AppServer(deadline_s=deadline_s)
        if a.timed_out:
            _fail_and_cleanup(a, run_dir, mode, "app-server spawn timeout")
            return
        header = _trace_header(a, run_id)
        tid = _start_approval_turn(a, run_dir, probe)
        if not tid:
            _fail_and_cleanup(a, run_dir, mode, "thread/start response")
            return
        summary = {"mode": mode}
        if mode == "duplicate":
            _run_duplicate_phase(a, summary, deadline_s)
        elif mode == "timeout":
            _run_timeout_phase(a, summary)
        elif mode == "cancel":
            _run_cancel_phase(a, tid, summary)
        summary["probe_created"] = os.path.exists(probe)
        summary["provider_timed_out"] = a.timed_out
        a.stop()
        _write_wire(a, run_dir, mode, header)
        with open(os.path.join(TMP, f"lifecycle_{mode}_summary.json"), "w") as f:
            json.dump(summary, f, indent=2)
        print(f"mode={mode} " + " ".join(f"{k}={v}" for k, v in summary.items() if k != "mode"))
        _verify_cleanup(run_dir, a._parent_pid, a._pgid, mode)
    finally:
        if a is not None:
            a.stop()


def _run_duplicate_phase(a: "AppServer", summary: dict, deadline_s: float):
    """Respond once via the production claim path; after the matching
    resolved, write a SECOND response with the same JSON-RPC id via a
    wire-level seam that deliberately bypasses the local claim (the claim's
    own duplicate refusal is proven by test_response_no_deadlock). Records
    PROVIDER duplicate handling."""
    responded_seq = None
    t0 = time.time()
    while time.time() - t0 < (deadline_s - 60) and not a.timed_out:
        if responded_seq is None:
            s = a.respond_to_approval("accept")
            if s is not None:
                responded_seq = s
            if responded_seq == -1:
                break
        if responded_seq is not None and a.saw_method("serverRequest/resolved"):
            break
        time.sleep(0.4)
    summary["first_response_seq"] = responded_seq
    resolved_before = a.count_method("serverRequest/resolved")
    summary["resolved_after_first"] = resolved_before
    if responded_seq in (None, -1) or resolved_before < 1:
        summary["status"] = "FAILED: no first response/resolved"
        return
    req_id = a.approval_request_id()
    # wire-level duplicate seam (evidence-only claim bypass)
    summary["dup_response_seq"] = a.send(
        {"jsonrpc": "2.0", "id": req_id, "result": {"decision": "accept"}})
    time.sleep(8)  # bounded observation window for the provider reaction
    with a._lock:
        errs = [o for o in a.msgs if o.get("id") == req_id and "error" in o]
        resolved_after = sum(1 for o in a.msgs
                             if o.get("method") == "serverRequest/resolved")
    summary["provider_error_on_dup"] = bool(errs)
    summary["provider_error_codes"] = sorted({(o.get("error") or {}).get("code") for o in errs})
    summary["second_resolved"] = resolved_after > resolved_before
    summary["status"] = "OK"


def _run_timeout_phase(a: "AppServer", summary: dict, window_s: float = 60):
    """Never respond. The request must stay pending (no resolved without a
    response) for the whole window; then stop() terminates the child and a
    late response attempt must fail closed. No decision is synthesized."""
    t0 = time.time()
    req_seen_at = None
    while time.time() - t0 < window_s and not a.timed_out:
        if req_seen_at is None and a.approval_request_id() is not None:
            req_seen_at = round(time.time() - t0, 1)
        time.sleep(0.5)
    summary["window_s"] = window_s
    summary["approval_request_seen_at_s"] = req_seen_at
    summary["resolved_during_window"] = a.saw_method("serverRequest/resolved")
    if req_seen_at is None:
        summary["status"] = "FAILED: no approval request within window"
        return
    a.stop()  # timeout policy: send nothing, terminate the owned child
    summary["post_stop_send_rc"] = a.send(
        {"jsonrpc": "2.0", "id": a.approval_request_id(),
         "result": {"decision": "accept"}})
    summary["fail_closed_after_stop"] = summary["post_stop_send_rc"] == -1
    summary["status"] = ("OK" if not summary["resolved_during_window"]
                         and summary["fail_closed_after_stop"] else "AMBIGUOUS")


def _run_cancel_phase(a: "AppServer", tid: str, summary: dict, window_s: float = 60):
    """On approval-pending, send turn/interrupt {threadId, turnId}
    (schema-verified). A resolved that precedes any response write is
    provider-side resolution, NOT success (plan §7); a post-interrupt
    response attempt is recorded as refused/ambiguous, never success."""
    t0 = time.time()
    req_id = None
    turn_id = None
    while time.time() - t0 < window_s and not a.timed_out:
        if req_id is None:
            req_id = a.approval_request_id()
        if turn_id is None:
            # The approval request itself carries turnId; turn/started nests
            # it at params.turn.id — prefer the request-bound identity.
            turn_id = a.approval_request_param("turnId")
            if turn_id is None:
                turn = a.first_param("turn/started", "turn")
                if isinstance(turn, dict):
                    turn_id = turn.get("id")
        if req_id is not None and turn_id is not None:
            break
        time.sleep(0.4)
    summary["approval_request_seen"] = req_id is not None
    summary["turn_id_seen"] = turn_id is not None
    if req_id is None or turn_id is None:
        summary["status"] = "FAILED: missing approval request or turnId"
        return
    resolved_before = a.count_method("serverRequest/resolved")
    summary["interrupt_seq"] = a.send(
        {"jsonrpc": "2.0", "id": 4, "method": "turn/interrupt",
         "params": {"threadId": tid, "turnId": turn_id}})
    t0 = time.time()
    while time.time() - t0 < 20 and not a.timed_out:
        if a.count_method("serverRequest/resolved") > resolved_before \
                and a.result_of(4) is not None:
            break
        time.sleep(0.4)
    summary["resolved_after_interrupt_before_any_response"] = (
        a.count_method("serverRequest/resolved") > resolved_before)
    summary["interrupt_result_received"] = a.result_of(4) is not None
    # Late response after provider-side resolution: record refusal only.
    late = a.respond_to_approval("accept")
    summary["late_response_seq"] = late
    time.sleep(5)  # bounded observation window for the provider reaction
    with a._lock:
        errs = [o for o in a.msgs if o.get("id") == req_id and "error" in o]
    summary["provider_error_on_late_response"] = bool(errs)
    summary["late_response_counts_as_success"] = False  # rule: resolved-before-write
    summary["status"] = "OK"


def _fail_and_cleanup(a: AppServer, run_dir, decision, reason):
    a.stop()  # idempotent: timer, owned PG, descriptors
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
    if pgid is None:
        print(f"cleanup_verified={decision} pgid=None (no owned PG was ever acknowledged)")
    else:
        ok, detail = _assert_pg_cleaned(pgid)
        if ok:
            print(f"cleanup_verified={decision} pgid={pgid} {detail}")
        else:
            print(f"WARNING: {decision} pgid={pgid} cleanup incomplete: {detail}")
            try:
                os.killpg(pgid, signal.SIGKILL)
            except (ProcessLookupError, PermissionError, OSError):
                pass
    if run_dir and os.path.isdir(run_dir):
        shutil.rmtree(run_dir, ignore_errors=True)


def _digest_and_stat(path):
    st = os.stat(path)
    return {"path_kind": "regular" if os.path.isfile(path) and not os.path.islink(path) else "other",
            "size": st.st_size, "sha256": sha256_file(path)}


def _spawned_image_info(a: "AppServer") -> dict:
    """H0-D: resolve the spawned image via the supervisor-returned PID/PGID
    contract. AppServer holds no direct Popen handle (no a.p)."""
    if a.timed_out or a._parent_pid is None:
        return {"spawned_image_error": "spawn failed or timed out (no supervisor-acknowledged PID)"}
    info = {"spawned_pid_source": "supervisor SCM_RIGHTS handshake",
            "spawned_pgid": a._pgid}
    try:
        comm = subprocess.run(["ps", "-p", str(a._parent_pid), "-o", "comm="],
                              capture_output=True, text=True, timeout=10).stdout.strip()
        info["spawned_image_comm"] = comm
    except Exception as e:
        info["spawned_image_error"] = str(e)
    return info


def cmd_launchchain():
    _reset_pseudonyms()
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
        # Recorded for CONTRAST only — evidence runs never execute the PATH codex.
        shim = os.path.realpath(codex)
        info["global_path_shim_not_used"] = {**_digest_and_stat(shim),
                        "realpath": "/opt/homebrew/lib/node_modules/@openai/codex/bin/codex.js"}
    # The executed provider: pinned exact-version 0.144.1 identity.
    info["pinned_codex"] = _pinned_codex_identity()
    info["pinned_shim"] = _digest_and_stat(_PINNED_CODEX_SHIM)
    info["pinned_native"] = _digest_and_stat(_PINNED_CODEX_NATIVE)
    a = AppServer()
    try:
        time.sleep(1.0)
        info.update(_spawned_image_info(a))
    finally:
        a.stop()
    out = os.path.join(TMP, "launch_chain.json")
    with open(out, "w") as f:
        json.dump(info, f, indent=2)
    print(f"launch chain -> {out}")


def cmd_attest():
    _reset_pseudonyms()
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
    if _lock_fd is None and os.path.exists(_LOCK_PATH):
        owner = ""
        try:
            with open(_LOCK_PATH) as f:
                owner = f.readline().strip()
        except OSError:
            pass
        print(f"REFUSED: foreign .probe.lock present ({owner}); "
              "confirm the recorded owner is dead before removing it explicitly")
        sys.exit(3)
    _release_lock()
    if os.path.isdir(TMP):
        shutil.rmtree(TMP)
        print(f"removed {TMP}")
    else:
        print("nothing to clean")


# ── no-model tests (each armed with a hard outer wall-clock watchdog) ──
def cmd_test_lock():
    """Gate 2: acquire/deny/release/re-acquire, one-outer-finally exception
    cleanup, busy rejection, and the foreign-lock ownership guard."""
    os.makedirs(TMP, exist_ok=True)
    rid1 = f"testlock_{int(time.time())}"
    if not _acquire_lock(rid1):
        print("TEST LOCK FAIL: first acquire failed"); sys.exit(1)
    print(f"LOCK_ACQUIRED: {rid1}")
    if _acquire_lock("testlock_2"):
        print("TEST LOCK FAIL: second acquire should have failed"); _release_lock(); sys.exit(1)
    print("LOCK_DENIED: second acquire correctly rejected")
    _release_lock()
    if not _acquire_lock("testlock_3"):
        print("TEST LOCK FAIL: re-acquire after release failed"); sys.exit(1)
    _release_lock()
    print("LOCK_RELEASED: acquire/deny/release/re-acquire ok")

    # Exception path: _with_run_lock must release from its single outer finally.
    class _Boom(Exception):
        pass

    def _raise():
        raise _Boom()
    try:
        _with_run_lock("testlock_exc", _raise)
        print("TEST LOCK FAIL: exception was swallowed"); sys.exit(1)
    except _Boom:
        pass
    if os.path.exists(_LOCK_PATH):
        print("TEST LOCK FAIL: lock leaked on exception path"); sys.exit(1)
    print("LOCK_EXCEPTION_CLEANUP: released by the single outer finally")

    # Busy path: _with_run_lock must exit(3) without running fn or touching
    # the (simulated) foreign lock.
    with open(_LOCK_PATH, "w") as f:
        f.write("foreign-owner pid=0\n")

    def _must_not_run():
        print("TEST LOCK FAIL: fn ran under a foreign lock")
        sys.exit(1)
    try:
        _with_run_lock("testlock_busy", _must_not_run)
        print("TEST LOCK FAIL: busy acquire did not exit"); sys.exit(1)
    except SystemExit as e:
        if e.code != 3:
            raise
    # Foreign-lock guard: _release_lock must NOT unlink a lock we do not hold.
    _release_lock()
    if not os.path.exists(_LOCK_PATH):
        print("TEST LOCK FAIL: foreign lock was deleted"); sys.exit(1)
    os.unlink(_LOCK_PATH)  # simulation created by this test; safe to remove
    print("LOCK_FOREIGN_GUARD: busy rejected with exit 3; foreign lock preserved")
    print("LOCK_TEST_PASS")


def cmd_test_lock_replacement():
    """H0-R1 gate: after the held lock's pathname is deleted and replaced by
    a foreign lock, _release_lock must close only its own fd and must NOT
    unlink the foreign lock (fd identity vs pathname identity comparison)."""
    os.makedirs(TMP, exist_ok=True)
    if os.path.exists(_LOCK_PATH):
        print("TEST REPLACEMENT FAIL: pre-existing lock — refusing to run"); sys.exit(1)
    if not _acquire_lock("replacement_owner"):
        print("TEST REPLACEMENT FAIL: acquire failed"); sys.exit(1)
    own = os.fstat(_lock_fd)
    os.unlink(_LOCK_PATH)  # simulate external deletion of OUR lock path
    with open(_LOCK_PATH, "w") as f:
        f.write("foreign-owner pid=0\n")  # foreign lock takes over the path
    foreign = os.stat(_LOCK_PATH)
    different = (own.st_dev, own.st_ino) != (foreign.st_dev, foreign.st_ino)
    _release_lock()
    survived = os.path.exists(_LOCK_PATH)
    print(f"own_inode={own.st_ino} foreign_inode={foreign.st_ino} "
          f"different={different} foreign_survived={survived}")
    if not different:
        print("TEST REPLACEMENT FAIL: inode did not change — vacuous scenario"); sys.exit(1)
    if not survived:
        print("TEST REPLACEMENT FAIL: foreign lock was deleted by release"); sys.exit(1)
    with open(_LOCK_PATH) as f:
        if f.readline().strip() != "foreign-owner pid=0":
            print("TEST REPLACEMENT FAIL: foreign lock content changed"); sys.exit(1)
    os.unlink(_LOCK_PATH)  # simulation created by this test; safe to remove
    # Positive control: when the pathname still refers to OUR inode, a normal
    # release must still unlink it (the fix is not "never unlink").
    if not _acquire_lock("replacement_after"):
        print("TEST REPLACEMENT FAIL: re-acquire after release failed"); sys.exit(1)
    _release_lock()
    if os.path.exists(_LOCK_PATH):
        print("TEST REPLACEMENT FAIL: normal release did not unlink own lock"); sys.exit(1)
    print("LOCK_REPLACEMENT_TEST_PASS foreign_preserved=True own_release_ok=True")


def cmd_test_response_no_deadlock():
    """Gate 3: the 0f67833 recursive response deadlock. Phase 1 proves the
    known-bad control (send under the state lock) deadlocks — i.e. this test
    CAN catch recursive locking. Phase 2 proves the production respond path
    completes in bound, claims exactly once, publishes an ordered seq and
    refuses duplicates."""
    global _FAULT_HOLD_LOCK_ACROSS_SEND
    req = {"jsonrpc": "2.0", "id": 7,
           "method": "item/commandExecution/requestApproval",
           "params": {"threadId": "t", "turnId": "u", "itemId": "i"}}

    # Phase 1 — known-bad control.
    bad = AppServer(_test_no_spawn=True)
    bad._sin = open(os.devnull, "w")
    bad._ingest(dict(req))
    _FAULT_HOLD_LOCK_ACROSS_SEND = True
    out = {}
    t = threading.Thread(
        target=lambda: out.setdefault("seq", bad.respond_to_approval("accept")),
        daemon=True)
    t.start()
    t.join(2.0)
    _FAULT_HOLD_LOCK_ACROSS_SEND = False
    if not t.is_alive():
        print("TEST RESPONSE FAIL: known-bad control did not deadlock — "
              "this test could not catch recursive locking")
        sys.exit(1)
    print("KNOWN_BAD_CONTROL: recursive send-under-lock deadlocked within the "
          "2.0s bound (daemon thread abandoned; dies with the process)")

    # Phase 2 — production path (the former recursive path, fixed).
    a = AppServer(_test_no_spawn=True)
    a._sin = open(os.devnull, "w")
    a._ingest(dict(req))
    done = {}
    t2 = threading.Thread(
        target=lambda: done.setdefault("seq", a.respond_to_approval("accept")),
        daemon=True)
    t2.start()
    t2.join(5.0)
    if t2.is_alive():
        print("TEST RESPONSE FAIL: production respond path did not finish within 5s")
        sys.exit(1)
    seq = done.get("seq")
    if seq != 2:
        print(f"TEST RESPONSE FAIL: expected outbound seq 2 after ingest seq 1, got {seq}")
        sys.exit(1)
    # Contested intermediate state: the claim is recorded at the
    # linearization point and the outbound record is ordered after the
    # inbound one in the same seq stream.
    with a._lock:
        claimed = set(a._responded_ids)
        trace = list(a.trace)
    if claimed != {7}:
        print(f"TEST RESPONSE FAIL: claim set wrong: {claimed}"); sys.exit(1)
    if [(r["dir"], r["seq"]) for r in trace] != [("provider->daemon", 1), ("daemon->provider", 2)]:
        print(f"TEST RESPONSE FAIL: trace order wrong: {trace}"); sys.exit(1)
    # Duplicate response must be refused at the claim linearization point.
    if a.respond_to_approval("accept") is not None:
        print("TEST RESPONSE FAIL: duplicate respond was not refused"); sys.exit(1)
    if a.stop() != a.stop():  # idempotent on an unspawned instance
        print("TEST RESPONSE FAIL: stop() not idempotent"); sys.exit(1)
    print("RESPONSE_NO_DEADLOCK_TEST_PASS claim_once=True ordered_seq=True duplicate_refused=True")


def cmd_test_prehandoff_hang():
    """Gate 4: a deterministic seam blocks the supervisor BEFORE the PID/FD
    handoff. Prove: the parent returns by deadline; the contested
    intermediate state (owned PG acknowledged and alive, handoff withheld)
    is observed via the READY barrier; the owned PG is removed; unrelated
    processes survive."""
    os.makedirs(TMP, exist_ok=True)
    before = _pgrep_codex()
    if before is None:
        print("OBSERVATION_FAILED: pgrep unavailable — cannot certify isolation")
        sys.exit(1)
    ready = threading.Event()
    holder = {}

    def on_ready(pgid):
        holder["pgid"] = pgid
        ready.set()

    res = {}
    t0 = time.monotonic()
    th = threading.Thread(
        target=lambda: res.setdefault("r", _spawn_with_timeout(
            _BENIGN_CHILD_CMD, 3.0,
            _fault_prehandoff_hang=True, _on_ready=on_ready)),
        daemon=True)
    th.start()
    if not ready.wait(15.0):
        print("TEST PREHANDOFF FAIL: READY barrier never reached"); sys.exit(1)
    pgid = holder["pgid"]
    # Contested intermediate state: PGID acknowledged, handoff withheld,
    # owned PG alive, spawn not yet returned.
    try:
        os.killpg(pgid, 0)
    except (ProcessLookupError, OSError):
        print("TEST PREHANDOFF FAIL: owned PG not alive during the pre-handoff window")
        sys.exit(1)
    if "r" in res:
        print("TEST PREHANDOFF FAIL: spawn returned before the timeout path")
        sys.exit(1)
    print(f"INTERMEDIATE_STATE: owned_pgid={pgid} alive, PID/FD handoff withheld")
    th.join(3.0 + 7.0)  # phase-2 timeout + kill/reap margin
    if th.is_alive():
        print("TEST PREHANDOFF FAIL: parent did not return by deadline"); sys.exit(1)
    elapsed = time.monotonic() - t0
    pid, rpgid, sin, sout, serr = res["r"]
    if pid is not None or rpgid != pgid or sin is not None or sout is not None:
        print(f"TEST PREHANDOFF FAIL: unexpected spawn result pid={pid} pgid={rpgid}")
        sys.exit(1)
    ok, detail = _assert_pg_cleaned(pgid)
    if not ok:
        print(f"TEST PREHANDOFF FAIL: {detail}"); sys.exit(1)
    after = _pgrep_codex()
    if after is None:
        print("OBSERVATION_FAILED: pgrep unavailable after cleanup"); sys.exit(1)
    if not before <= after:
        print(f"TEST PREHANDOFF FAIL: unrelated processes lost: {before - after}")
        sys.exit(1)
    print(f"PREHANDOFF_HANG_TEST_PASS elapsed={elapsed:.1f}s owned_pg_clean=True "
          f"unrelated_ok=True ({detail})")


def cmd_test_pg_cleanup():
    """Gate 5a: stop() kills exactly the owned PG; descriptors and timer are
    cleared once; stop() is idempotent; the wire fails closed after stop;
    unrelated processes are preserved. Benign child — no codex, no model."""
    _reset_pseudonyms()
    before = _pgrep_codex()
    if before is None:
        print("OBSERVATION_FAILED: pgrep unavailable — cannot certify isolation")
        sys.exit(1)
    print(f"unrelated_before={len(before)}")
    a = AppServer(deadline_s=60, cmd=_BENIGN_CHILD_CMD)
    if a.timed_out or a._pgid is None:
        print("TEST PG FAIL: spawn failed"); sys.exit(1)
    probe_pgid = a._pgid
    probe_pid = a._parent_pid
    print(f"probe_pid={probe_pid} probe_pgid={probe_pgid}")
    first = a.stop()
    second = a.stop()  # idempotent
    if first != second:
        print("TEST PG FAIL: stop() not idempotent"); sys.exit(1)
    if a._sin is not None or a._sout is not None or a._serr is not None \
            or a._deadline_timer is not None:
        print("TEST PG FAIL: descriptors/timer not cleared by stop()"); sys.exit(1)
    if a.send({"probe": True}) != -1:
        print("TEST PG FAIL: send() not fail-closed after stop"); sys.exit(1)
    ok, detail = _assert_pg_cleaned(probe_pgid)
    if not ok:
        print(f"TEST PG FAIL: {detail}"); sys.exit(1)
    after = _pgrep_codex()
    if after is None:
        print("OBSERVATION_FAILED: pgrep unavailable after stop"); sys.exit(1)
    if not before <= after:
        print(f"TEST PG FAIL: unrelated processes lost: {before - after}"); sys.exit(1)
    print(f"PG_CLEANUP_TEST_PASS ({detail}) unrelated_untouched={len(before & after)}")


def cmd_test_runtime_timeout():
    """Gate 5b (renamed from test_startup_timeout — it proves the RUNTIME
    deadline): the deadline fires, the owned PG is killed and cleaned, the
    caller returns, and the wire fails closed. The bounded poll below waits
    on elapsed wall-clock, which IS the property under test."""
    _reset_pseudonyms()
    a = AppServer(deadline_s=2, cmd=_BENIGN_CHILD_CMD)
    if a.timed_out or a._pgid is None:
        print("TEST RUNTIME FAIL: spawn failed"); sys.exit(1)
    pgid = a._pgid
    t0 = time.monotonic()
    while time.monotonic() - t0 < 10 and not a.timed_out:
        time.sleep(0.1)
    elapsed = time.monotonic() - t0
    if not a.timed_out:
        print(f"TEST RUNTIME FAIL: deadline did not fire within {elapsed:.1f}s")
        sys.exit(1)
    if a.send({"probe": True}) != -1:
        print("TEST RUNTIME FAIL: send() not fail-closed after timeout"); sys.exit(1)
    ok, detail = _assert_pg_cleaned(pgid)
    if not ok:
        print(f"TEST RUNTIME FAIL: {detail}"); sys.exit(1)
    a.stop()  # must be safe after the timeout path
    print(f"RUNTIME_TIMEOUT_TEST_PASS deadline_fired={elapsed:.1f}s ({detail})")


def cmd_test_launchchain_smoke():
    """Gate 6: launch-chain PID resolution uses the supervisor PID/PGID
    contract with no None dereference — on a live benign child AND on an
    unspawned instance (the removed code raised AttributeError on a.p)."""
    _reset_pseudonyms()
    a = AppServer(deadline_s=30, cmd=_BENIGN_CHILD_CMD)
    try:
        if a.timed_out or a._parent_pid is None:
            print("TEST LAUNCHCHAIN FAIL: spawn failed"); sys.exit(1)
        info = _spawned_image_info(a)
        if not info.get("spawned_image_comm"):
            print(f"TEST LAUNCHCHAIN FAIL: no comm resolved: {info}"); sys.exit(1)
        print(f"LAUNCHCHAIN_LIVE: comm={info['spawned_image_comm']} pgid={info['spawned_pgid']}")
    finally:
        a.stop()
    ok, detail = _assert_pg_cleaned(a._pgid)
    if not ok:
        print(f"TEST LAUNCHCHAIN FAIL: {detail}"); sys.exit(1)
    dead = AppServer(_test_no_spawn=True)
    info2 = _spawned_image_info(dead)  # must not raise
    if "spawned_image_error" not in info2:
        print(f"TEST LAUNCHCHAIN FAIL: unspawned instance did not report an error: {info2}")
        sys.exit(1)
    dead.stop()
    print("LAUNCHCHAIN_SMOKE_TEST_PASS live_resolved=True unspawned_safe=True")


def cmd_test_compile():
    """Verify the harness compiles cleanly."""
    import py_compile
    py_compile.compile(__file__, doraise=True)
    print("COMPILE_TEST_PASS")


# Hard outer wall-clock bound per no-model test: a hung test exits 124
# instead of hanging the gate (H0 gate requirement).
_TEST_WALLCLOCK_S = {
    "test_compile": 30,
    "test_lock": 30,
    "test_lock_replacement": 30,
    "test_response_no_deadlock": 60,
    "test_prehandoff_hang": 60,
    "test_pg_cleanup": 60,
    "test_runtime_timeout": 60,
    "test_launchchain_smoke": 60,
}


def _arm_test_watchdog(name: str):
    limit = _TEST_WALLCLOCK_S[name]

    def _abort():
        sys.stderr.write(f"HARD_TIMEOUT: {name} exceeded {limit}s wall-clock\n")
        sys.stderr.flush()
        os._exit(124)
    t = threading.Timer(limit, _abort)
    t.daemon = True
    t.start()


def main():
    args = sys.argv[1:]
    if len(args) == 1 and args[0] in _TEST_WALLCLOCK_S:
        _arm_test_watchdog(args[0])
    if args == ["schema"]:
        cmd_schema()
    elif args == ["schema_repro"]:
        cmd_schema_repro()
    elif args == ["schema_subset"]:
        cmd_schema_subset()
    elif args == ["init"]:
        cmd_init()
    elif args == ["approval", "accept"]:
        _approval_trace("accept")
    elif args == ["approval", "decline"]:
        _approval_trace("decline")
    elif args == ["approval", "duplicate"]:
        _lifecycle_trace("duplicate")
    elif args == ["approval", "timeout"]:
        _lifecycle_trace("timeout")
    elif args == ["approval", "cancel"]:
        _lifecycle_trace("cancel")
    elif args == ["launchchain"]:
        cmd_launchchain()
    elif args == ["attest"]:
        cmd_attest()
    elif args == ["clean"]:
        cmd_clean()
    elif args == ["test_lock"]:
        cmd_test_lock()
    elif args == ["test_lock_replacement"]:
        cmd_test_lock_replacement()
    elif args == ["test_response_no_deadlock"]:
        cmd_test_response_no_deadlock()
    elif args == ["test_prehandoff_hang"]:
        cmd_test_prehandoff_hang()
    elif args == ["test_pg_cleanup"]:
        cmd_test_pg_cleanup()
    elif args == ["test_runtime_timeout"]:
        cmd_test_runtime_timeout()
    elif args == ["test_launchchain_smoke"]:
        cmd_test_launchchain_smoke()
    elif args == ["test_compile"]:
        cmd_test_compile()
    else:
        sys.stderr.write("refused: argv not in the fixed CP0 allowlist\n")
        sys.exit(2)


if __name__ == "__main__":
    main()
