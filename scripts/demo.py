#!/usr/bin/env python3
"""Build and exercise three localhost nodes; no LAN discovery or external seeds."""
import os
import re
from pathlib import Path
import signal
import subprocess
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[1]


def main():
    processes = []
    lines = {name: [] for name in ("seed", "alice", "bob")}
    lock = threading.Lock()
    with tempfile.TemporaryDirectory(prefix="p2p-park-demo-") as tmp:
        binary = str(Path(tmp) / "park-node")
        subprocess.run(["go", "build", "-o", binary, "./cmd/park-node"], cwd=ROOT, check=True)

        def collect(name, process):
            for line in process.stdout:
                with lock:
                    lines[name].append(line)
                # Do not put even disposable demo channel keys in CI output.
                if not re.fullmatch(r"\s*[0-9a-f]{64}\s*", line):
                    print(f"[{name}] {line}", end="", flush=True)

        def wait_for(predicate, description):
            end = time.monotonic() + 15
            while time.monotonic() < end:
                with lock:
                    if predicate():
                        return
                if any(p.poll() is not None for p in processes):
                    raise RuntimeError("a demo node exited early")
                time.sleep(0.05)
            raise RuntimeError(f"timed out: {description}")

        def launch(name, bootstrap=None):
            # Let the OS choose a port; obtain the seed's actual port from its log.
            data = str(Path(tmp) / name)
            env = dict(os.environ, P2P_PARK_DATA_DIR=data)
            args = [binary, "-name", name, "-bind", "127.0.0.1:0", "-lan=false", "-debug", "-data", data]
            if bootstrap:
                args += ["-bootstrap", bootstrap]
            else:
                args += ["-seed"]
            p = subprocess.Popen(args, cwd=ROOT, env=env, stdin=subprocess.PIPE,
                                 stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)
            processes.append(p)
            threading.Thread(target=collect, args=(name, p), daemon=True).start()
            return p

        try:
            launch("seed")
            wait_for(lambda: any("listening on " in s for s in lines["seed"]), "seed listen")
            with lock:
                line = next(s for s in lines["seed"] if "listening on " in s)
            addr = line.split("listening on ", 1)[1].split(",", 1)[0]
            alice = launch("alice", addr)
            bob = launch("bob", addr)
            wait_for(lambda: all(any("connected to peer" in s for s in lines[n]) for n in ("alice", "bob")), "peer connections")
            alice.stdin.write("/say portfolio-demo-message\n")
            alice.stdin.flush()
            wait_for(lambda: any("portfolio-demo-message" in s for s in lines["bob"]), "Alice's gossip at Bob")
            alice.stdin.write("/mkchan portfolio\n")
            alice.stdin.flush()
            wait_for(lambda: any(re.fullmatch(r"\s*[0-9a-f]{64}\s*", s) for s in lines["alice"]), "channel creation")
            with lock:
                key = next(s.strip() for s in lines["alice"] if re.fullmatch(r"\s*[0-9a-f]{64}\s*", s))
            bob.stdin.write(f"/joinchan portfolio {key}\n")
            bob.stdin.flush()
            wait_for(lambda: any('joined channel "portfolio"' in s for s in lines["bob"]), "channel join")
            alice.stdin.write("/encsay portfolio portfolio-encrypted-message\n")
            alice.stdin.flush()
            wait_for(lambda: any("portfolio-encrypted-message" in s for s in lines["bob"]), "authenticated channel decryption at Bob")
            print("PASS: three local nodes connected; Bob received signed gossip and decrypted Alice's channel message.")
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


if __name__ == "__main__":
    main()
