#!/usr/bin/env python3
"""Explicit, bounded reliability gates. FAIL and NOT_RUN both exit nonzero.

Ordinary CI tests describe current behavior; delivery acceptance asks for the
missing receipt/receiver-pressure guarantee. Media always requires real evidence.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]


def run_check(name, packages, names, env, timeout, count=1):
    command = ["go", "test", "-json", "-race", "-count=" + str(count),
               "-timeout=" + str(timeout) + "s", "-run", "^(" + "|".join(names) + ")$", *packages]
    started = time.monotonic()
    try:
        completed = subprocess.run(command, cwd=ROOT, env=env, text=True,
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                   timeout=timeout + 15)
        code, output = completed.returncode, completed.stdout
    except subprocess.TimeoutExpired as error:
        code = 124
        output = error.stdout or b""
        if isinstance(output, bytes):
            output = output.decode(errors="replace")
        output += "\nqualification command timed out\n"
    outcomes, evidence = {}, []
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if event.get("Test") in names and event.get("Action") in ("pass", "fail", "skip"):
            outcomes[event["Test"]] = event["Action"]
        message = event.get("Output", "").strip()
        if any(marker in message for marker in ("DELIVERY ", "ROOM ", "METRICS ", "COMPAT ", "RECONNECT ", "FAIL receiver-pressure")):
            evidence.append(message)
    # A zero exit with no matched tests, or a skipped required gate, is not PASS.
    passed = code == 0 and all(outcomes.get(test) == "pass" for test in names)
    return {"name": name, "status": "PASS" if passed else "FAIL", "command": command,
            "return_code": code, "duration_seconds": round(time.monotonic() - started, 3),
            "required_test_outcomes": outcomes, "evidence": evidence, "log": output}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gate", choices=("all", "behavior", "room", "capacity", "delivery", "reconnect", "media"), default="all")
    parser.add_argument("--soak-rounds", type=int, default=3, help="capacity rounds/pressure repetitions, 1..1000; explicit long run")
    parser.add_argument("--output", type=Path, help="JSON evidence including full command logs; outside the source tree recommended")
    args = parser.parse_args()
    if not 1 <= args.soak_rounds <= 1000:
        parser.error("--soak-rounds must be 1..1000")
    env = dict(os.environ, GOTOOLCHAIN="local", P2P_QUALIFY="1", P2P_SOAK_ROUNDS=str(args.soak_rounds))
    results = []
    selected = lambda name: args.gate in ("all", name)
    if selected("behavior"):
        results.append(run_check("documented-best-effort-behavior", ["./engine"],
                                ["TestSendStalledConsumerCurrentBehavior", "TestPublishZeroPeersCurrentBehavior"], env, 90))
        results.append(run_check("fanout-disconnect-and-wire-compatibility", ["./internal/p2p"],
                                ["TestDeliveryFanoutIsolatesFullPeerQueue", "TestDeliveryDisconnectAfterSuccessfulEnqueue", "TestDeliveryCoreWireVersionMatrix"], env, 90))
        results.append(run_check("invitation-and-access-control-regressions", ["./engine"],
                                ["TestInvitationGoldenVectors", "TestInvitationFixtureLiveContracts", "TestInvitationPinsMembershipIsolationAndPreflight", "TestInvitationMultipleBootstrapsAndConcurrentJoins", "TestInvitationDoesNotTrustMaliciousReferrals", "TestInvitationCloseCancelsBootstrapAndJoinsWorkers"], env, 90))
    if selected("room"):
        results.append(run_check("application-room-model", ["./qualification"],
                                ["TestRoomRecoveryAuthorizationAndIdempotency", "TestRoomProcessRestartAfterLostAcknowledgement", "TestRoomRejectsStaleOrGappedRecovery"], env, 90))
    if selected("capacity"):
        results.append(run_check("bounded-local-churn", ["./qualification"], ["TestBoundedChurnRecovery"], env, max(90, args.soak_rounds * 2 + 30)))
        results.append(run_check("repeated-stalled-consumer", ["./engine"], ["TestSendStalledConsumerCurrentBehavior"], env, max(90, args.soak_rounds * 2 + 30), count=args.soak_rounds))
    if selected("reconnect"):
        results.append(run_check("concurrent-discovery-manual-join-acceptance", ["./qualification"],
                                ["TestQualificationConcurrentDiscoveryJoinStability"], env, max(90, args.soak_rounds * 2 + 30)))
    if selected("delivery"):
        results.append(run_check("receiver-pressure-feedback-acceptance", ["./engine"],
                                ["TestQualificationReceiverPressureFeedback"], env, 90))
    if selected("media"):
        results.append({"name": "real-device-streaming", "status": "NOT_RUN",
                        "reason": "Browser/mobile media application, authorized devices and network-path evidence are unavailable; use docs/STREAMING_QUALIFICATION.md. Loopback cannot satisfy this gate."})
    report = {"schema": 1, "commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
              "dirty": bool(subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).strip()),
              "toolchain": subprocess.check_output(["go", "version"], env=env, text=True).strip(),
              "platform": sys.platform, "gate": args.gate, "soak_rounds": args.soak_rounds,
              "scope": "Experimental one-host tests and application model; not production certification or independent audit",
              "status": "PASS" if all(r["status"] == "PASS" for r in results) else "FAIL", "results": results}
    if args.output:
        args.output.write_text(json.dumps(report, indent=2) + "\n")
    for result in results:
        print(result["status"] + " " + result["name"])
        for message in result.get("evidence", []):
            print("  " + message)
        if result["status"] == "FAIL":
            print("  see full command log in --output JSON; exit=" + str(result.get("return_code")))
    print("Overall " + report["status"] + "; scope=" + args.gate)
    return 0 if report["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
