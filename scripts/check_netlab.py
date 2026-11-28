#!/usr/bin/env python3
"""End-to-end Make/terminal UX and retry-budget rejection. One host only."""
import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import sys
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[1]


class Terminal:
    def __init__(self, mode, state, link="", duration=90):
        self.lines = queue.Queue(maxsize=256)
        env = dict(os.environ, PORT="0", STATE=str(state), LINK=link, DURATION=str(duration), BIND="127.0.0.1:0",
                   ADVERTISE="", ALLOW_CIDRS="127.0.0.1/32", DISCOVER_CIDRS="", DROP_EVERY="0", RESPONSE_DELAY="0s")
        self.process = subprocess.Popen(["make", "net-"+mode], cwd=ROOT, env=env, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1,
                                        start_new_session=True)
        def collect():
            for line in self.process.stdout:
                self.lines.put(line)
            self.process.stdout.close()
        self.reader = threading.Thread(target=collect, daemon=True)
        self.reader.start()

    def await_line(self, predicate, seconds=60):
        deadline = time.monotonic()+seconds
        while time.monotonic()<deadline:
            try:
                line = self.lines.get(timeout=0.1)
            except queue.Empty:
                if self.process.poll() is not None:
                    raise RuntimeError("terminal exited before expected output")
                continue
            if predicate(line): return line
        raise RuntimeError("terminal event deadline")

    def command(self, line):
        self.process.stdin.write(line+"\n")
        self.process.stdin.flush()

    def close(self):
        if self.process.poll() is None:
            try:
                self.command("quit")
                self.process.wait(timeout=8)
            except (BrokenPipeError, subprocess.TimeoutExpired):
                os.killpg(self.process.pid, signal.SIGTERM)
                self.process.wait(timeout=10)
        self.process.stdin.close()
        self.reader.join(timeout=2)
        if self.reader.is_alive() or self.process.returncode != 0:
            raise RuntimeError("terminal did not exit cleanly")


def main():
    processes = []
    with tempfile.TemporaryDirectory(prefix="netlab-terminal-e2e-") as temp:
        try:
            host = Terminal("host", Path(temp)/"host"); processes.append(host)
            link = host.await_line(lambda line: line.startswith("LINK="))[5:].strip()
            joiner = Terminal("join", Path(temp)/"joiner", link); processes.append(joiner)
            joiner.await_line(lambda line: line.startswith("LINK="))
            joiner.command("ping")
            event = json.loads(joiner.await_line(lambda line: '"event": "probe_ok"' in line))
            if len(event["signing_id"]) != 64: raise RuntimeError("missing verified signer")
            joiner.command("join")
            joiner.await_line(lambda line: '"event": "joined"' in line)
            # A borrowed local seed remains untouched while owned clients churn.
            subprocess.run([sys.executable, "scripts/netlab.py", "attach", "--link", link,
                            "--scenario", "all", "--nodes", "2", "--count", "2", "--rate", "10",
                            "--output", str(Path(temp)/"attach.json")], cwd=ROOT,
                           check=True, timeout=90, stdout=subprocess.DEVNULL)
            if host.process.poll() is not None: raise RuntimeError("borrowed host was stopped by attach mode")
            # Scope mismatches must fail rather than relabel localhost as remote.
            result = subprocess.run([sys.executable, "scripts/netlab.py", "remote", "--link", link,
                                     "--ack-remote", "--scenario", "smoke", "--output", str(Path(temp)/"remote.json")],
                                    cwd=ROOT, timeout=60, stdout=subprocess.DEVNULL)
            report = json.loads((Path(temp)/"remote.json").read_text())
            if result.returncode == 0 or report["status"] != "FAIL" or report["physical_cross_host_verification"] != "NOT_RUN":
                raise RuntimeError("loopback was accepted as real cross-host evidence")
            # All replies intentionally omitted: finite attempts MUST fail.
            result = subprocess.run([sys.executable, "scripts/netlab.py", "local", "--scenario", "smoke",
                                     "--nodes", "2", "--drop-every", "1", "--attempts", "2", "--timeout", "1",
                                     "--output", str(Path(temp)/"exhausted.json")], cwd=ROOT, timeout=60,
                                    stdout=subprocess.DEVNULL)
            report = json.loads((Path(temp)/"exhausted.json").read_text())
            if result.returncode == 0 or report["probe_attempts"] != 2 or report["failed_operations"] != 1:
                raise RuntimeError("lost-response retry budget was masked or exceeded")
            result = subprocess.run([sys.executable, "scripts/netlab.py", "local", "--scenario", "chaos",
                                     "--restarts", "0", "--output", str(Path(temp)/"no-restarts.json")],
                                    cwd=ROOT, timeout=60, stdout=subprocess.DEVNULL)
            report = json.loads((Path(temp)/"no-restarts.json").read_text())
            if result.returncode == 0 or report["process_restarts"] != 0 or "restart budget exhausted" not in report.get("error", ""):
                raise RuntimeError("owned crash exceeded the restart limiter")
            print("PASS terminal shared-link ping; attach seed isolation; false remote rejected; exhausted retry/restart budgets fail")
        finally:
            errors = []
            for terminal in reversed(processes):
                try: terminal.close()
                except Exception as error: errors.append(str(error))
            if errors: raise RuntimeError("; ".join(errors))


if __name__ == "__main__":
    main()
