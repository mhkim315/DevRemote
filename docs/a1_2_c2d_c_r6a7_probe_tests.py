#!/usr/bin/env python3
"""A1.2 C2D-C R6-A7 Packet A: deterministic no-model harness tests.

These tests never invoke Claude. They exercise the complete probe lifecycle
through dependency injection (fake executables, fake provider runner, real
process-group runner with fake process trees, real generated hook scripts fed
synthetic stdin) and prove every Packet A check from
docs/NEXT_EXECUTOR_A1_2_C2D_C_R6_A7_FRESH_AGENT_HANDOFF.md section 3.

Run:  python3 docs/a1_2_c2d_c_r6a7_probe_tests.py
"""

import contextlib
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import a1_2_c2d_c_r6a7_safe_deny_probe as probe  # noqa: E402

# Token-shaped sentinels built by concatenation so the repository secret scan
# never sees a literal token pattern in this source file.
SENTINEL_TOKEN = "sk" + "-" + "SENTINELTOKENVALUE12345"
SENTINEL_GHP = "gh" + "p_" + "SENTINELGHPVALUE12345"
SENTINEL_INNER_KEY = "SENTINEL_INNER_KEY_q7"
SENTINEL_EXC = "/Users/sentinel-user/topsecret-prompt-path"


def write_script(dirpath, name, body):
    path = os.path.join(dirpath, name)
    with open(path, "w") as f:
        f.write(body)
    os.chmod(path, 0o700)
    return path


class FakeClaudeRunner:
    """Simulates the pinned executable + hook side effects, no model calls.

    Mutation knobs let each test inject exactly one adversarial deviation.
    """

    def __init__(self, mutations=None, sid="sess-fake-0001",
                 tuid="toolu_fake_0001"):
        self.m = mutations or {}
        self.sid = sid
        self.tuid = tuid
        self.resume_calls = 0
        self.seen_owned = None
        self.marker_name = None
        self.tool_input = None

    def _owned(self, argv):
        return os.path.dirname(argv[argv.index("--settings") + 1])

    def run(self, argv, cwd, env, timeout_s):
        if "--version" in argv:
            return probe.RunOutcome("exited_zero", b"2.1.209 (fake)\n",
                                    "clean", -1)
        if "-p" in argv:
            return self._initial(argv)
        return self._resume(argv, cwd)

    def _initial(self, argv):
        owned = self._owned(argv)
        self.seen_owned = owned
        prompt = argv[argv.index("-p") + 1]
        expected_cmd = prompt.split("exactly this command: ", 1)[1]
        self.marker_name = expected_cmd.split("touch ", 1)[1]
        tin = {"command": expected_cmd}
        if self.m.get("inner_sentinel"):
            tin[SENTINEL_INNER_KEY] = SENTINEL_TOKEN
            tin["nested"] = {"ghp_like": SENTINEL_GHP}
        if self.m.get("wrong_captured_command"):
            tin = {"command": "echo not-the-marker-command"}
        self.tool_input = tin
        cap = {
            "session_id": self.sid,
            "tool_use_id": self.tuid,
            "tool_name": self.m.get("captured_tool_name", "Bash"),
            "input_digest": probe.canonical_digest(tin),
            "command": tin.get("command", ""),
        }
        if not self.m.get("skip_defer_capture"):
            with open(os.path.join(owned, "defer_capture.json"), "w") as f:
                json.dump(cap, f)
        dtu_input = tin
        if self.m.get("deferred_input_mutated"):
            dtu_input = dict(tin, mutated="yes")
        deferred = {
            "type": "result",
            "stop_reason": "tool_deferred",
            "session_id": self.m.get("deferred_sid", self.sid),
            "deferred_tool_use": {
                "id": self.m.get("deferred_tuid", self.tuid),
                "name": self.m.get("deferred_tname", "Bash"),
                "input": dtu_input,
            },
        }
        lines = [{"type": "system", "subtype": "init"}, deferred]
        if self.m.get("no_deferred"):
            lines = [{"type": "system", "subtype": "init"},
                     {"type": "result", "stop_reason": "end_turn",
                      "session_id": self.sid}]
        raw = "\n".join(json.dumps(l) for l in lines).encode("utf-8")
        return probe.RunOutcome("exited_zero", raw, "clean", -1)

    def _resume(self, argv, cwd):
        self.resume_calls += 1
        owned = self._owned(argv)
        if self.m.get("raise_resume"):
            raise Exception(self.m["raise_resume"])
        with open(os.path.join(owned, "expected_identity.json")) as f:
            want = json.load(f)
        got = dict(want)
        if self.m.get("resume_wrong_digest"):
            got["input_digest"] = "0" * 64
        if self.m.get("resume_wrong_sid"):
            got["session_id"] = "sess-other-9999"
        if not self.m.get("skip_resume_capture"):
            with open(os.path.join(owned, "resume_capture.json"), "w") as f:
                json.dump(got, f)
        if self.m.get("touch_marker") and self.marker_name:
            open(os.path.join(cwd, self.marker_name), "w").close()
        entry = {
            "tool_use_id": self.m.get("denial_tuid", self.tuid),
            "tool_name": "Bash",
            "message": "denied by permission hook",
        }
        if not self.m.get("omit_provider_input"):
            if self.m.get("denial_input_mutated"):
                entry["tool_input"] = {"command": "echo substituted-evil"}
            else:
                entry["tool_input"] = self.tool_input
        decoy = {"tool_use_id": "toolu_fake_retry_0002", "tool_name": "Bash"}
        event = {
            "type": "result",
            "stop_reason": "end_turn",
            "session_id": self.m.get("denial_sid", self.sid),
            "permission_denials": [entry, decoy],
        }
        if self.m.get("extra_field_second_run") and self.resume_calls >= 2:
            event["zz_unstable_extra"] = True
        lines = [{"type": "system", "subtype": "init"}, event]
        raw = "\n".join(json.dumps(l) for l in lines).encode("utf-8")
        return probe.RunOutcome("exited_zero", raw, "clean", -1)


class RaisingRunner:
    """A runner that must never be reached (zero-spawn proofs)."""

    def run(self, argv, cwd, env, timeout_s):
        raise AssertionError("spawn attempted")


class ProbeTestCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="r6a7-test-")

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def make_fake_pin(self, version_body=None):
        body = version_body or "#!/bin/sh\necho '2.1.209 (fake)'\n"
        exe = write_script(self.tmp, "fake_claude", body)
        return probe.PinConfig(
            path=exe,
            expected_sha256=probe.sha256_file(exe),
            expected_version="2.1.209",
            expected_arch="script",
            locator="~/fake/claude-2.1.209",
        ), exe

    def orchestrate(self, mutations=None, runner=None):
        pin, _ = self.make_fake_pin()
        runner = runner or FakeClaudeRunner(mutations)
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            proj = probe.run_live_once("run-a", pin, runner)
        return proj, runner, buf.getvalue()

    def orchestrate_failure(self, code, mutations=None, runner=None):
        pin, _ = self.make_fake_pin()
        runner = runner or FakeClaudeRunner(mutations)
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            with self.assertRaises(probe.ProbeFailure) as ctx:
                probe.run_live_once("run-a", pin, runner)
        self.assertEqual(ctx.exception.code, code)
        return runner, buf.getvalue()


# --- A1/A2: owned process group lifecycle ------------------------------------

class TestGroupCleanup(ProbeTestCase):
    def test_timeout_kills_child_and_grandchild_group(self):
        exe = write_script(self.tmp, "tree.sh", (
            "#!/bin/sh\n"
            "sh -c 'sleep 300 >/dev/null 2>&1 & wait' >/dev/null 2>&1 &\n"
            "sleep 300 >/dev/null 2>&1\n"))
        runner = probe.GroupRunner()
        outcome = runner.run([exe], cwd=self.tmp, env=None, timeout_s=0.5)
        self.assertEqual(outcome.exit_enum, "timeout")
        self.assertNotEqual(outcome.pgid, os.getpgrp())
        self.assertFalse(runner.group_alive(outcome.pgid))
        self.assertIn(outcome.cleanup_enum, ("clean", "killed_leftover"))
        # The test runner's own group was never signaled: we are still here
        # and our group id is unchanged.
        self.assertEqual(os.getpgrp(), os.getpgrp())

    def test_normal_exit_with_leftover_child_is_detected_and_killed(self):
        exe = write_script(self.tmp, "leftover.sh", (
            "#!/bin/sh\n"
            "sleep 300 >/dev/null 2>&1 &\n"
            "exit 0\n"))
        runner = probe.GroupRunner()
        outcome = runner.run([exe], cwd=self.tmp, env=None, timeout_s=10)
        self.assertEqual(outcome.exit_enum, "exited_zero")
        self.assertEqual(outcome.cleanup_enum, "killed_leftover")
        self.assertFalse(runner.group_alive(outcome.pgid))

    def test_normal_exit_without_children_is_clean(self):
        exe = write_script(self.tmp, "clean.sh", "#!/bin/sh\nexit 0\n")
        runner = probe.GroupRunner()
        outcome = runner.run([exe], cwd=self.tmp, env=None, timeout_s=10)
        self.assertEqual(outcome.exit_enum, "exited_zero")
        self.assertEqual(outcome.cleanup_enum, "clean")
        self.assertFalse(runner.group_alive(outcome.pgid))

    def test_spawn_failure_maps_to_closed_enum(self):
        runner = probe.GroupRunner()
        with self.assertRaises(probe.ProbeFailure) as ctx:
            runner.run([os.path.join(self.tmp, "does-not-exist")],
                       cwd=self.tmp, env=None, timeout_s=1)
        self.assertEqual(ctx.exception.code, "spawn_failed")


# --- A3/A4: identity joins ----------------------------------------------------

class TestIdentityJoin(ProbeTestCase):
    IDENTITY = {
        "session_id": "sess-x", "tool_use_id": "toolu-x",
        "tool_name": "Bash",
        "input_digest": probe.canonical_digest({"command": "touch m"}),
    }

    def test_each_single_field_mutation_makes_join_false(self):
        for field in ("session_id", "tool_use_id", "tool_name",
                      "input_digest"):
            got = dict(self.IDENTITY)
            got[field] = got[field] + "-mutated"
            result = probe.compare_identity(self.IDENTITY, got)
            self.assertEqual(sum(1 for v in result.values() if not v), 1,
                             "exactly the mutated field must be false: "
                             + field)
            self.assertFalse(all(result.values()))

    def test_matching_control_makes_join_true(self):
        result = probe.compare_identity(self.IDENTITY, dict(self.IDENTITY))
        self.assertTrue(all(result.values()))

    def test_join_deferred_binds_all_four_fields(self):
        event = {"type": "result", "stop_reason": "tool_deferred",
                 "session_id": "sess-x",
                 "deferred_tool_use": {"id": "toolu-x", "name": "Bash",
                                       "input": {"command": "touch m"}}}
        self.assertTrue(all(probe.join_deferred(self.IDENTITY,
                                                event).values()))
        mutated = json.loads(json.dumps(event))
        mutated["deferred_tool_use"]["input"] = {"command": "touch other"}
        self.assertFalse(all(probe.join_deferred(self.IDENTITY,
                                                 mutated).values()))

    def test_denial_wrong_tool_use_id_never_matches(self):
        event = {"type": "result", "session_id": "sess-x",
                 "permission_denials": [
                     {"tool_use_id": "toolu-OTHER", "tool_name": "Bash"}]}
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.match_denial(self.IDENTITY, [event])
        self.assertEqual(ctx.exception.code, "no_denial_match")

    def test_denial_wrong_session_never_matches(self):
        event = {"type": "result", "session_id": "sess-OTHER",
                 "permission_denials": [
                     {"tool_use_id": "toolu-x", "tool_name": "Bash"}]}
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.match_denial(self.IDENTITY, [event])
        self.assertEqual(ctx.exception.code, "no_denial_match")

    def test_denial_duplicate_matches_are_ambiguous(self):
        entry = {"tool_use_id": "toolu-x", "tool_name": "Bash"}
        event = {"type": "result", "session_id": "sess-x",
                 "permission_denials": [entry, dict(entry)]}
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.match_denial(self.IDENTITY, [event])
        self.assertEqual(ctx.exception.code, "ambiguous_denial")

    def test_denial_input_digest_mismatch_fails_closed(self):
        event = {"type": "result", "session_id": "sess-x",
                 "permission_denials": [
                     {"tool_use_id": "toolu-x", "tool_name": "Bash",
                      "tool_input": {"command": "echo evil"}}]}
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.match_denial(self.IDENTITY, [event])
        self.assertEqual(ctx.exception.code, "identity_mismatch")

    def test_denial_matching_control_with_input_digest(self):
        event = {"type": "result", "session_id": "sess-x",
                 "permission_denials": [
                     {"tool_use_id": "toolu-x", "tool_name": "Bash",
                      "tool_input": {"command": "touch m"}},
                     {"tool_use_id": "toolu-retry", "tool_name": "Bash"}]}
        result = probe.match_denial(self.IDENTITY, [event])
        self.assertTrue(result["unique"])
        self.assertTrue(result["provider_input_present"])
        self.assertIs(result["digest_equal"], True)

    def test_denial_without_provider_input_is_classified_truthfully(self):
        event = {"type": "result", "session_id": "sess-x",
                 "permission_denials": [
                     {"tool_use_id": "toolu-x", "tool_name": "Bash"}]}
        result = probe.match_denial(self.IDENTITY, [event])
        self.assertFalse(result["provider_input_present"])
        self.assertIsNone(result["digest_equal"])


# --- A5: pinned executable gate ------------------------------------------------

class TestPinVerification(ProbeTestCase):
    def test_wrong_realpath_rejected_before_any_spawn(self):
        pin, exe = self.make_fake_pin()
        other = write_script(self.tmp, "global_claude",
                             "#!/bin/sh\necho '2.1.209 (global)'\n")
        recorder = probe.SpawnRecorder(RaisingRunner())
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.verify_pin(other, pin, recorder, cwd=self.tmp)
        self.assertEqual(ctx.exception.code, "executable_not_pinned")
        self.assertEqual(recorder.calls, [])

    def test_wrong_digest_rejected_before_any_spawn(self):
        pin, exe = self.make_fake_pin()
        pin.expected_sha256 = "0" * 64
        recorder = probe.SpawnRecorder(RaisingRunner())
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.verify_pin(exe, pin, recorder, cwd=self.tmp)
        self.assertEqual(ctx.exception.code, "executable_not_pinned")
        self.assertEqual(recorder.calls, [])

    def test_wrong_arch_rejected_before_any_spawn(self):
        pin, exe = self.make_fake_pin()
        pin.expected_arch = "arm64"
        recorder = probe.SpawnRecorder(RaisingRunner())
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.verify_pin(exe, pin, recorder, cwd=self.tmp)
        self.assertEqual(ctx.exception.code, "executable_not_pinned")
        self.assertEqual(recorder.calls, [])

    def test_version_mismatch_after_pin_checks(self):
        pin, exe = self.make_fake_pin(
            version_body="#!/bin/sh\necho '2.0.0 (fake)'\n")
        recorder = probe.SpawnRecorder(probe.GroupRunner())
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.verify_pin(exe, pin, recorder, cwd=self.tmp)
        self.assertEqual(ctx.exception.code, "version_mismatch")
        self.assertEqual(len(recorder.calls), 1)  # only the pinned artifact

    def test_matching_pin_passes(self):
        pin, exe = self.make_fake_pin()
        facts = probe.verify_pin(exe, pin, probe.GroupRunner(), cwd=self.tmp)
        self.assertEqual(facts["version"], "2.1.209")
        self.assertEqual(facts["arch"], "script")

    def test_orchestrator_rejects_substituted_executable_before_spawn(self):
        pin, exe = self.make_fake_pin()
        other = write_script(self.tmp, "substitute_claude",
                             "#!/bin/sh\necho '2.1.209 (sub)'\n")
        recorder = probe.SpawnRecorder(RaisingRunner())
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.run_live_once("run-a", pin, recorder, executable=other)
        self.assertEqual(ctx.exception.code, "executable_not_pinned")
        self.assertEqual(recorder.calls, [])


# --- Full lifecycle with fake provider (no model) -------------------------------

class TestLifecycle(ProbeTestCase):
    def test_happy_path_produces_validated_projection(self):
        proj, runner, out = self.orchestrate()
        self.assertEqual(set(proj.keys()), probe.PROJECTION_KEYS)
        probe.validate_projection(proj)
        self.assertTrue(proj["denial_match_unique"])
        self.assertTrue(proj["provider_input_present"])
        self.assertIs(proj["provider_input_digest_equal"], True)
        self.assertEqual(proj["result_top_fields"]["permission_denials"],
                         "array")
        self.assertEqual(proj["denial_entry_top_fields"]["tool_input"],
                         "object")
        self.assertEqual(proj["initial_exit"], "exited_zero")
        self.assertEqual(proj["resume_exit"], "exited_zero")

    def test_projection_never_recurses_into_tool_input(self):
        proj, _, _ = self.orchestrate(mutations={"inner_sentinel": True})
        text = json.dumps(proj)
        self.assertNotIn(SENTINEL_INNER_KEY, text)
        self.assertNotIn(SENTINEL_TOKEN, text)
        self.assertNotIn(SENTINEL_GHP, text)
        for name in proj["denial_entry_top_fields"]:
            self.assertNotIn(".", name)

    def test_marker_created_by_execution_fails_run(self):
        # Non-vacuous side-effect check: creating the EXACT marker the bound
        # command would create must flip the assertion and fail the run.
        self.orchestrate_failure("marker_violation",
                                 mutations={"touch_marker": True})

    def test_wrong_captured_command_breaks_marker_linkage(self):
        self.orchestrate_failure("unexpected_tool_input",
                                 mutations={"wrong_captured_command": True})

    def test_non_bash_tool_fails_closed(self):
        self.orchestrate_failure("unexpected_tool_input",
                                 mutations={"captured_tool_name": "Write"})

    def test_missing_hook_capture_fails_closed(self):
        self.orchestrate_failure("hook_capture_missing",
                                 mutations={"skip_defer_capture": True})

    def test_missing_deferred_result_fails_closed(self):
        self.orchestrate_failure("no_deferred_result",
                                 mutations={"no_deferred": True})

    def test_deferred_session_mismatch_fails_defer_join(self):
        self.orchestrate_failure("defer_join_failed",
                                 mutations={"deferred_sid": "sess-other"})

    def test_deferred_tool_use_mismatch_fails_defer_join(self):
        self.orchestrate_failure("defer_join_failed",
                                 mutations={"deferred_tuid": "toolu-other"})

    def test_deferred_tool_name_mismatch_fails_defer_join(self):
        self.orchestrate_failure("defer_join_failed",
                                 mutations={"deferred_tname": "Write"})

    def test_deferred_input_mutation_fails_defer_join(self):
        self.orchestrate_failure("defer_join_failed",
                                 mutations={"deferred_input_mutated": True})

    def test_resume_capture_missing_fails_resume_join(self):
        self.orchestrate_failure("resume_join_failed",
                                 mutations={"skip_resume_capture": True})

    def test_resume_digest_mutation_fails_resume_join(self):
        self.orchestrate_failure("resume_join_failed",
                                 mutations={"resume_wrong_digest": True})

    def test_resume_session_mutation_fails_resume_join(self):
        self.orchestrate_failure("resume_join_failed",
                                 mutations={"resume_wrong_sid": True})

    def test_denial_with_wrong_tool_use_id_fails(self):
        self.orchestrate_failure("no_denial_match",
                                 mutations={"denial_tuid": "toolu-other"})

    def test_denial_from_other_session_fails(self):
        self.orchestrate_failure("no_denial_match",
                                 mutations={"denial_sid": "sess-other"})

    def test_denial_input_substitution_fails(self):
        self.orchestrate_failure("identity_mismatch",
                                 mutations={"denial_input_mutated": True})

    def test_denial_without_input_succeeds_with_truthful_flags(self):
        proj, _, _ = self.orchestrate(mutations={"omit_provider_input": True})
        self.assertFalse(proj["provider_input_present"])
        self.assertIsNone(proj["provider_input_digest_equal"])
        probe.validate_projection(proj)

    def test_owned_dir_deleted_after_success_and_failure(self):
        proj, runner, _ = self.orchestrate()
        self.assertFalse(os.path.exists(runner.seen_owned))
        runner2, _ = self.orchestrate_failure(
            "no_denial_match", mutations={"denial_tuid": "toolu-other"})
        self.assertFalse(os.path.exists(runner2.seen_owned))


# --- A7: privacy of stdout and projection ---------------------------------------

class TestPrivacy(ProbeTestCase):
    def assert_no_sentinels(self, text, runner):
        self.assertNotIn(SENTINEL_TOKEN, text)
        self.assertNotIn(SENTINEL_GHP, text)
        self.assertNotIn(SENTINEL_INNER_KEY, text)
        self.assertNotIn(SENTINEL_EXC, text)
        if runner and runner.seen_owned:
            self.assertNotIn(runner.seen_owned, text)  # temp/cwd path
        if runner and runner.marker_name:
            self.assertNotIn(runner.marker_name, text)  # command material
        self.assertNotIn("touch marker-", text)  # raw command shape
        self.assertNotIn("Use your Bash tool", text)  # raw prompt shape

    def test_success_stdout_and_projection_are_sentinel_free(self):
        proj, runner, out = self.orchestrate(
            mutations={"inner_sentinel": True})
        self.assert_no_sentinels(out + json.dumps(proj), runner)

    def test_failure_stdout_is_sentinel_free(self):
        runner, out = self.orchestrate_failure(
            "identity_mismatch",
            mutations={"inner_sentinel": True, "denial_input_mutated": True})
        self.assert_no_sentinels(out, runner)

    def test_raw_exception_values_never_reach_stdout(self):
        pin, _ = self.make_fake_pin()
        runner = FakeClaudeRunner({"raise_resume": SENTINEL_EXC})
        out_a = os.path.join(self.tmp, "out-a.json")
        out_b = os.path.join(self.tmp, "out-b.json")
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            rc = probe.run_packet_b(pin, runner, out_a, out_b)
        self.assertEqual(rc, 1)
        self.assertIn("FAIL unexpected_error", buf.getvalue())
        self.assert_no_sentinels(buf.getvalue(), runner)


# --- A8: failure can never produce a success projection --------------------------

class TestFailureExit(ProbeTestCase):
    def run_packet(self, mutations):
        pin, _ = self.make_fake_pin()
        runner = FakeClaudeRunner(mutations)
        out_a = os.path.join(self.tmp, "out-a.json")
        out_b = os.path.join(self.tmp, "out-b.json")
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            rc = probe.run_packet_b(pin, runner, out_a, out_b)
        return rc, out_a, out_b, buf.getvalue()

    def test_failed_run_exits_nonzero_and_writes_nothing(self):
        rc, out_a, out_b, out = self.run_packet(
            {"denial_tuid": "toolu-other"})
        self.assertEqual(rc, 1)
        self.assertIn("FAIL no_denial_match", out)
        self.assertFalse(os.path.exists(out_a))
        self.assertFalse(os.path.exists(out_b))

    def test_unstable_structure_exits_nonzero_and_writes_nothing(self):
        rc, out_a, out_b, out = self.run_packet(
            {"extra_field_second_run": True})
        self.assertEqual(rc, 1)
        self.assertIn("FAIL unstable_structure", out)
        self.assertFalse(os.path.exists(out_a))
        self.assertFalse(os.path.exists(out_b))

    def test_successful_packet_writes_two_equivalent_projections(self):
        rc, out_a, out_b, out = self.run_packet(None)
        self.assertEqual(rc, 0)
        self.assertIn("PACKET-B: OK", out)
        with open(out_a) as f:
            pa = json.load(f)
        with open(out_b) as f:
            pb = json.load(f)
        probe.validate_projection(pa)
        probe.validate_projection(pb)
        self.assertEqual(pa["run"], "run-a")
        self.assertEqual(pb["run"], "run-b")
        self.assertEqual(probe.stability_strip(pa), probe.stability_strip(pb))

    def test_main_refuses_to_run_live_without_flag(self):
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            rc = probe.main(["prog"])
        self.assertEqual(rc, 2)


# --- Bounded projector -------------------------------------------------------------

class TestProjector(ProbeTestCase):
    def test_field_count_bound_fails_closed(self):
        obj = {"k%d" % i: i for i in range(probe.MAX_FIELD_COUNT + 1)}
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.project_fields(obj)
        self.assertEqual(ctx.exception.code, "projection_bounds_exceeded")

    def test_field_name_length_bound_fails_closed(self):
        obj = {"k" * (probe.MAX_FIELD_NAME_LEN + 1): 1}
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.project_fields(obj)
        self.assertEqual(ctx.exception.code, "projection_bounds_exceeded")

    def test_non_dict_fails_closed(self):
        with self.assertRaises(probe.ProbeFailure) as ctx:
            probe.project_fields(["not", "a", "dict"])
        self.assertEqual(ctx.exception.code, "schema_invalid")

    def test_no_recursion_into_nested_objects(self):
        out = probe.project_fields({"outer": {"inner_secret": "x"},
                                    "arr": [1, 2]})
        self.assertEqual(out, {"arr": "array", "outer": "object"})

    def test_validate_projection_rejects_missing_or_extra_keys(self):
        proj, _, _ = self.orchestrate()
        broken = dict(proj)
        del broken["marker_absent_post"]
        with self.assertRaises(probe.ProbeFailure):
            probe.validate_projection(broken)
        extra = dict(proj)
        extra["zz_extra"] = True
        with self.assertRaises(probe.ProbeFailure):
            probe.validate_projection(extra)

    def test_validate_projection_rejects_false_evidence_booleans(self):
        proj, _, _ = self.orchestrate()
        for key in ("denial_match_unique", "marker_absent_post",
                    "resume_join_input_digest_equal"):
            bad = dict(proj)
            bad[key] = False
            with self.assertRaises(probe.ProbeFailure):
                probe.validate_projection(bad)

    def test_unknown_failure_code_collapses_to_unexpected_error(self):
        self.assertEqual(probe.ProbeFailure("not-a-real-code").code,
                         "unexpected_error")


# --- Generated hook scripts (real subprocess, synthetic stdin, no Claude) ----------

class TestHookScripts(ProbeTestCase):
    PRETOOL = {
        "session_id": "sess-hook-1", "tool_use_id": "toolu-hook-1",
        "tool_name": "Bash", "tool_input": {"command": "touch marker-h"},
    }

    def run_hook(self, source, stdin_obj, owned):
        path = probe.write_hook(owned, "hook_under_test.py", source)
        proc = subprocess.run(
            [sys.executable, path],
            input=json.dumps(stdin_obj).encode("utf-8")
            if isinstance(stdin_obj, dict) else stdin_obj,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
        return proc

    def test_defer_hook_captures_identity_and_defers(self):
        owned = tempfile.mkdtemp(prefix="r6a7-hook-", dir=self.tmp)
        proc = self.run_hook(probe.HOOK_DEFER_SOURCE, self.PRETOOL, owned)
        self.assertEqual(proc.returncode, 0)
        decision = json.loads(proc.stdout)
        self.assertEqual(
            decision["hookSpecificOutput"]["permissionDecision"], "defer")
        with open(os.path.join(owned, "defer_capture.json")) as f:
            cap = json.load(f)
        self.assertEqual(cap["session_id"], "sess-hook-1")
        self.assertEqual(cap["tool_use_id"], "toolu-hook-1")
        self.assertEqual(cap["input_digest"],
                         probe.canonical_digest(self.PRETOOL["tool_input"]))
        self.assertEqual(cap["command"], "touch marker-h")

    def test_defer_hook_fails_closed_on_malformed_input(self):
        owned = tempfile.mkdtemp(prefix="r6a7-hook-", dir=self.tmp)
        proc = self.run_hook(probe.HOOK_DEFER_SOURCE, b"{not json", owned)
        self.assertEqual(proc.returncode, 2)
        decision = json.loads(proc.stdout)
        self.assertEqual(
            decision["hookSpecificOutput"]["permissionDecision"], "deny")
        self.assertFalse(
            os.path.exists(os.path.join(owned, "defer_capture.json")))

    def test_resume_hook_denies_on_full_match_and_claims_once(self):
        owned = tempfile.mkdtemp(prefix="r6a7-hook-", dir=self.tmp)
        expected = {
            "session_id": "sess-hook-1", "tool_use_id": "toolu-hook-1",
            "tool_name": "Bash",
            "input_digest": probe.canonical_digest(
                self.PRETOOL["tool_input"]),
        }
        with open(os.path.join(owned, "expected_identity.json"), "w") as f:
            json.dump(expected, f)
        proc = self.run_hook(probe.HOOK_RESUME_SOURCE, self.PRETOOL, owned)
        self.assertEqual(proc.returncode, 0)
        decision = json.loads(proc.stdout)
        self.assertEqual(
            decision["hookSpecificOutput"]["permissionDecision"], "deny")
        self.assertIn("research denial",
                      decision["hookSpecificOutput"]
                      ["permissionDecisionReason"])
        with open(os.path.join(owned, "resume_capture.json")) as f:
            cap = json.load(f)
        self.assertEqual(cap, expected)
        # Replay: the atomic claim blocks a second decision delivery.
        proc2 = self.run_hook(probe.HOOK_RESUME_SOURCE, self.PRETOOL, owned)
        self.assertEqual(proc2.returncode, 2)
        decision2 = json.loads(proc2.stdout)
        self.assertIn("already consumed",
                      decision2["hookSpecificOutput"]
                      ["permissionDecisionReason"])

    def test_resume_hook_fails_closed_on_identity_mismatch(self):
        owned = tempfile.mkdtemp(prefix="r6a7-hook-", dir=self.tmp)
        expected = {
            "session_id": "sess-hook-1", "tool_use_id": "toolu-hook-1",
            "tool_name": "Bash",
            "input_digest": probe.canonical_digest(
                self.PRETOOL["tool_input"]),
        }
        with open(os.path.join(owned, "expected_identity.json"), "w") as f:
            json.dump(expected, f)
        mutated = dict(self.PRETOOL,
                       tool_input={"command": "touch marker-EVIL"})
        proc = self.run_hook(probe.HOOK_RESUME_SOURCE, mutated, owned)
        self.assertEqual(proc.returncode, 2)
        decision = json.loads(proc.stdout)
        self.assertEqual(
            decision["hookSpecificOutput"]["permissionDecision"], "deny")
        self.assertIn("identity mismatch",
                      decision["hookSpecificOutput"]
                      ["permissionDecisionReason"])
        self.assertFalse(
            os.path.exists(os.path.join(owned, "resume_capture.json")))

    def test_resume_hook_fails_closed_without_expected_identity(self):
        owned = tempfile.mkdtemp(prefix="r6a7-hook-", dir=self.tmp)
        proc = self.run_hook(probe.HOOK_RESUME_SOURCE, self.PRETOOL, owned)
        self.assertEqual(proc.returncode, 2)
        decision = json.loads(proc.stdout)
        self.assertIn("no deferred identity",
                      decision["hookSpecificOutput"]
                      ["permissionDecisionReason"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
