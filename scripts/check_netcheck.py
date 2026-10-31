#!/usr/bin/env python3
"""Local diagnostic-process smoke test. Separate processes, ONE host, loopback only.
This is not LAN/WAN, NAT, partition or firewall verification.
"""
import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[1]


def main():
    processes = []
    with tempfile.TemporaryDirectory(prefix="netcheck-smoke-") as temp:
        binary = str(Path(temp) / "netcheck")
        subprocess.run(["go", "build", "-race", "-o", binary, "./examples/netcheck"], cwd=ROOT, check=True)

        def start(name, invitation=None, expect_failure=False):
            state = str(Path(temp) / name)
            args = [binary, "-state", state, "-duration", "45s", "-probe-timeout", "2s"]
            if invitation:
                args += ["-invite", invitation, "-allow-cidrs", "127.0.0.1/32"]
            p = subprocess.Popen(args, cwd=ROOT, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                 stderr=subprocess.PIPE, text=True, bufsize=1)
            processes.append(p)
            events = queue.Queue()
            def collect():
                for line in p.stdout:
                    events.put(json.loads(line))
            threading.Thread(target=collect, daemon=True).start()
            if expect_failure:
                if p.wait(timeout=8) == 0:
                    raise RuntimeError("unreachable bootstrap unexpectedly succeeded")
                return p, events, None
            return p, events, wait(events, "ready")

        def wait(events, event):
            end = time.monotonic() + 8
            while time.monotonic() < end:
                try:
                    item = events.get(timeout=max(0.01, end-time.monotonic()))
                except queue.Empty:
                    break
                if item["event"] == event:
                    return item
                if item["event"] in ("command_error", "probe_timeout"):
                    raise RuntimeError(item)
            raise RuntimeError(f"timed out awaiting {event}")

        def command(p, text):
            p.stdin.write(text+"\n")
            p.stdin.flush()

        def stop(p):
            if p.poll() is None:
                command(p, "quit")
                if p.wait(timeout=5) != 0:
                    raise RuntimeError(p.stderr.read())

        try:
            a, ae, ar = start("A")
            if (Path(temp) / "A/network-id.txt").read_text().strip() != ar["network_id"]:
                raise RuntimeError("creator network state not persisted")
            a_inv = str(Path(temp) / "A/invitation.txt")
            b, be, br = start("B", a_inv)
            command(b, f"probe {ar['peer_id']} {ar['signing_id']}")
            if wait(be, "probe_ok")["signing_id"] != ar["signing_id"]:
                raise RuntimeError("wrong authenticated signer")
            command(b, f"probe {ar['peer_id']} {br['signing_id']}")
            # The command must fail closed against a substituted expected signer.
            end = time.monotonic()+5
            while time.monotonic()<end:
                event=be.get(timeout=5)
                if event["event"]=="command_error":break
                if event["event"] in ("probe_sent", "probe_ok"):raise RuntimeError("expected-identity substitution accepted")
            else:
                raise RuntimeError("no identity denial")
            stop(a)
            end = time.monotonic()+5
            while time.monotonic()<end:
                event=be.get(timeout=5)
                if event.get("event")=="peer_event" and event.get("type")=="peer_disconnected" and event.get("peer",{}).get("ID")==ar["peer_id"]:
                    break
            else:
                raise RuntimeError("old session did not disconnect")
            start("failed-C", a_inv, expect_failure=True)
            # Explicit fallback via B, not a claim of automatic partition healing.
            b_inv = str(Path(temp) / "B/invitation.txt")
            restored, re, rr = start("A", b_inv)
            if rr["peer_id"] != ar["peer_id"] or rr["signing_id"] != ar["signing_id"] or rr["network_id"] != ar["network_id"]:
                raise RuntimeError("identity/network changed across restart")
            command(restored, f"probe {br['peer_id']} {br['signing_id']}")
            wait(re, "probe_ok")
            stop(restored)
            stop(b)
            print("PASS netcheck: separate loopback processes, signer denial, bootstrap failure, persistent-identity fallback and process cleanup; cross-host NOT tested")
        finally:
            for p in processes:
                if p.poll() is None:
                    p.send_signal(signal.SIGINT)
            for p in processes:
                try:
                    p.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    p.kill()
                    p.wait()
                if p.returncode and not p.returncode == 1:
                    error = p.stderr.read()
                    if error:
                        print(error)


if __name__ == "__main__":
    main()
