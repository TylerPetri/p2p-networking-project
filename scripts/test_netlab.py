"""Controller invariants: retry/rate/resource limits and fail-closed options."""
import contextlib
import io
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch

import netlab


class ControllerTests(unittest.TestCase):
    def tearDown(self):
        netlab.STOP.clear()

    def test_retry_exhaustion_never_becomes_success(self):
        calls, failures, pauses = [], [], []
        def fail(index):
            calls.append(index)
            raise netlab.Failure("response lost")
        with self.assertRaises(netlab.Failure):
            netlab.retry(fail, 3, pauses.append, lambda i, error: failures.append(i))
        self.assertEqual(calls, [0, 1, 2])
        self.assertEqual(failures, [0, 1, 2])
        self.assertEqual(pauses, [0, 1])

    def test_retry_success_retains_attempt_failures(self):
        failed = []
        def operation(index):
            if index < 2:
                raise netlab.Failure("temporary disconnect")
            return {"authenticated": True}
        self.assertEqual(netlab.retry(operation, 3, lambda i: None, lambda i, e: failed.append(i)), {"authenticated": True})
        self.assertEqual(failed, [0, 1])

    def test_shared_budget_stops_before_deadline(self):
        budget = netlab.Budget(20, 10)
        budget.deadline = time.monotonic() - 1
        with self.assertRaises(netlab.Failure):
            budget.take()
        budget = netlab.Budget(1, 10)
        netlab.STOP.set()
        with self.assertRaises(netlab.Failure):
            budget.take()

    def test_global_rate_paces_every_request(self):
        budget = netlab.Budget(5, 10)
        with patch.object(netlab.STOP, "wait", return_value=False) as wait:
            budget.take()
            budget.take()
            self.assertGreater(wait.call_args_list[-1].args[0], 0.15)

    def test_private_state_has_exclusive_owner(self):
        with tempfile.TemporaryDirectory() as temp:
            node = netlab.Node("unused", temp, None, {})
            try:
                with self.assertRaises(netlab.Failure):
                    netlab.Node("unused", temp, None, {})
            finally:
                node.close()
            again = netlab.Node("unused", temp, None, {})
            again.close()

    def test_log_disk_use_is_capped_and_truncation_reported(self):
        with tempfile.TemporaryDirectory() as temp:
            node = netlab.Node("unused", temp, None, {})
            try:
                for _ in range(4):
                    node.record("test", "x" * 800000)
                self.assertGreater(node.log_truncated, 0)
                self.assertLessEqual((Path(temp)/"events.jsonl").stat().st_size, 2*1024*1024)
            finally:
                node.close()

    def test_remote_requires_explicit_load_authorization(self):
        with patch.dict(os.environ, {}, clear=True), contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit):
                netlab.options(["remote", "--link", "p2p://invite/abc"])

    def test_bad_limits_and_nonloopback_bind_fail_closed(self):
        cases = [["local", "--join-timeout", "31"], ["local", "--attempts", "6"], ["local", "--nodes", "100"],
                 ["local", "--rate", "999"], ["local", "--duration", "0"],
                 ["host", "--bind", "0.0.0.0:45001"],
                 ["host", "--bind", "192.168.1.3:45001"],
                 ["join", "--link", "p2p://invite/" + "a"*3000],
                 ["join", "--link", "p2p://invite/abc\nquit"],
                 ["local", "--bind", "127.0.0.1:45001"],
                 ["local", "--bind", "192.168.1.3:0", "--nonlocal-bind"]]
        with patch.dict(os.environ, {}, clear=True), contextlib.redirect_stderr(io.StringIO()):
            for case in cases:
                with self.subTest(case=case), self.assertRaises(SystemExit):
                    netlab.options(case)

    def test_profile_defaults_are_bounded(self):
        with patch.dict(os.environ, {}, clear=True):
            cfg = netlab.options(["local"])
        self.assertEqual((cfg.attempts, cfg.restarts, cfg.rate, cfg.duration), (3, 3, 5, 180))
        self.assertEqual(cfg.bind, "127.0.0.1:0")
        self.assertEqual(cfg.join_timeout,22)
        self.assertTrue(cfg.race)


class RemoteScopeTests(unittest.TestCase):
    def test_remote_does_not_implicitly_approve_local_destinations(self):
        with patch.dict(os.environ, {}, clear=True):
            cfg = netlab.options(["remote", "--ack-remote", "--link", "p2p://invite/abc"])
        self.assertEqual(cfg.allow, "")
