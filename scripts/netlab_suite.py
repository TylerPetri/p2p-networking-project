#!/usr/bin/env python3
"""Explicit long suite: execute all phases, preserve every failure/NOT_RUN, never mask release gates."""
import argparse
import signal
import tempfile
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]


def integer(name, default, low, high):
    number = int(os.getenv(name) or default)
    if not low <= number <= high:
        raise ValueError(f"{name} must be {low}..{high}")
    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--remote", action="store_true")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    os.umask(0o077)
    rounds = integer("SOAK_ROUNDS", 20, 1, 1000)
    fuzz = integer("FUZZ_SECONDS", 5, 1, 60)
    if args.remote and (not os.getenv("LINK") or os.getenv("ACK_REMOTE") != "1"):
        parser.error("remote suite requires LINK and ACK_REMOTE=1 for an owner-authorized endpoint")
    go = os.getenv("GO") or "go"
    python = sys.executable
    args.output = args.output.resolve()
    args.output.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    log_root = Path(tempfile.mkdtemp(prefix=args.output.stem+"-",dir=args.output.parent))
    phases = [
        ("controller-tests", [python, "-m", "unittest", "discover", "-s", "scripts", "-p", "test_netlab.py"], 60),
        ("boundary", [python, "scripts/check_engine_boundary.py"], 120),
        ("invitation-vectors", [python, "scripts/check_invitation_vectors.py"], 60),
        ("external-consumer", [python, "scripts/check_external_consumer.py"], 180),
        ("module-integrity", [go, "mod", "verify"], 120),
        ("vet", [go, "vet", "./..."], 180),
        ("uncached", [go, "test", "-count=1", "-timeout=3m", "./..."], 240),
        ("race", [go, "test", "-race", "-count=1", "-timeout=3m", "./..."], 240),
        ("transport-stress", [go, "test", "-race", "-count=20", "-timeout=3m", "./internal/crypto/noiseconn", "./internal/dht", "./internal/crypto/channel"], 240),
        ("concurrency", [go, "test", "-race", "-count=10", "-timeout=3m", "./engine", "./qualification"], 240),
        ("echo", [go, "run", "./examples/echo"], 120),
        ("park", [python, "scripts/demo.py"], 120),
        ("netcheck", [python, "scripts/check_netcheck.py"], 120),
        ("history-secret-scan", [go, "run", "github.com/zricethezav/gitleaks/v8@v8.24.3", "git", "--log-opts=--all --full-history", "--redact", "."], 240),
        ("terminal-e2e", [python, "scripts/check_netlab.py"], 180),
    ]
    for package, target in (("engine", "FuzzInvitationParse"), ("internal/p2p", "FuzzEnvelopeDecoder"),
                            ("internal/dht", "FuzzRecordValidation"), ("internal/crypto/channel", "FuzzChannelDecrypt")):
        phases.append((target, [go, "test", "./" + package, "-run", "^$", "-fuzz", "^" + target + "$", "-fuzztime", str(fuzz) + "s", "-parallel=2"], fuzz + 120))
    phases.append(("owned-local-chaos", [python, "scripts/netlab.py", "local", "--output", str(log_root / "local.json")], integer("DURATION",180,10,3600)+360))
    if os.getenv("LINK"):
        mode = "remote" if args.remote else "attach"
        phases.append((mode + "-same-link", [python, "scripts/netlab.py", mode, "--output", str(log_root / "same-link.json")], integer("DURATION",180,10,3600)+360))
    phases.append(("qualification", [python, "scripts/qualify.py", "--gate", "all", "--soak-rounds", str(rounds), "--output", str(log_root / "qualification.json")], max(600, rounds*6+180)))
    results = []
    try:
        for name, command, timeout in phases:
            started = time.monotonic()
            path = log_root / (name + ".log")
            print("RUN " + name, flush=True)
            # Stream directly to a bounded-duration file, not an unbounded in-memory capture.
            with path.open("w") as output:
                try:
                    environment = dict(os.environ, GOTOOLCHAIN="local", P2P_QUALIFY="0", P2P_SOAK_ROUNDS="3")
                    if name in ("owned-local-chaos", "terminal-e2e"):
                        environment.update(LINK="", BIND="127.0.0.1:0", ADVERTISE="", ALLOW_CIDRS="127.0.0.1/32", DISCOVER_CIDRS="", ACK_NONLOCAL="0", ACK_REMOTE="0")
                    process = subprocess.Popen(command, cwd=ROOT, stdout=output, stderr=subprocess.STDOUT,
                                               start_new_session=True, env=environment)
                    try:
                        code = process.wait(timeout=timeout)
                    except (subprocess.TimeoutExpired, KeyboardInterrupt):
                        os.killpg(process.pid, signal.SIGTERM)
                        try:
                            process.wait(timeout=90)  # let owned netlab children clean up
                        except subprocess.TimeoutExpired:
                            os.killpg(process.pid, signal.SIGKILL)
                            process.wait(timeout=5)
                        if sys.exc_info()[0] is KeyboardInterrupt:
                            raise
                        code = 124
                except OSError as error:
                    output.write(str(error)+"\n")
                    code = 127
            result = {"name": name, "command": command, "status": "PASS" if code == 0 else "FAIL",
                      "exit_code": code, "seconds": round(time.monotonic()-started, 3), "log": str(path)}
            results.append(result)
            print(result["status"] + " " + name + " exit=" + str(code), flush=True)
    finally:
        report = {"schema": 1, "status": "PASS" if len(results) == len(phases) and all(r["status"] == "PASS" for r in results) else "FAIL",
                  "commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
                  "scope": "all phases retain their own scope; localhost is not WAN/media qualification",
                  "physical_cross_host_verification": "OWNER_EVIDENCE_REQUIRED" if args.remote else "NOT_RUN",
                  "media": "NOT_RUN", "results": results}
        args.output.write_text(json.dumps(report, indent=2) + "\n")
        print("OVERALL " + report["status"] + "; full report=" + str(args.output), flush=True)
    return 0 if report["status"] == "PASS" else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (ValueError, OSError) as error:
        print(json.dumps({"status": "FAIL", "error": str(error)}), file=sys.stderr)
        raise SystemExit(1)
