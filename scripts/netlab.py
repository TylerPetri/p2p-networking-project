#!/usr/bin/env python3
"""Bounded real-process network lab. No engine protocol changes or remote process control."""
import argparse
from collections import deque
import fcntl
import ipaddress
import json
import os
from pathlib import Path
import queue
import random
import secrets
import signal
import subprocess
import sys
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[1]
STOP = threading.Event()
EVENTS = {"ready", "peers", "probe_ok", "probe_timeout", "command_error", "joined", "join_failed"}


class Failure(RuntimeError):
    pass


class Budget:
    """Shared controller pacing; retries consume the same rate budget as first attempts."""
    def __init__(self, rate, duration):
        self.interval = 1.0 / rate
        self.next = time.monotonic()
        self.deadline = self.next + duration

    def take(self):
        now = time.monotonic()
        delay = max(0, self.next - now)
        if now + delay >= self.deadline or STOP.wait(delay):
            raise Failure("run duration/cancellation budget exhausted")
        self.next = time.monotonic() + self.interval

    def backoff(self, attempt):
        delay = min(2.0, 0.125 * (2 ** attempt) * random.uniform(0.75, 1.25))
        if time.monotonic() + delay >= self.deadline or STOP.wait(delay):
            raise Failure("retry duration/cancellation budget exhausted")


def retry(operation, attempts, pause, observe):
    """Only idempotent diagnostics are retried; no state-changing action protocol."""
    for attempt in range(attempts):
        try:
            return operation(attempt)
        except Failure as error:
            observe(attempt, str(error))
            if attempt + 1 == attempts:
                raise
            pause(attempt)


class Node:
    def __init__(self, binary, state, config, report):
        self.binary, self.state, self.config, self.report = str(binary), Path(state).resolve(), config, report
        self.process = None
        self.ready = None
        self.threads = []
        self.events = queue.Queue(maxsize=512)
        self.reader_error = None
        self.last_errors = deque(maxlen=4)
        self.restarts = 0
        self.log_bytes = 0
        self.log_truncated = 0
        self.log_lock = threading.Lock()
        self.state.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.lock = (self.state / "lab.lock").open("a+")
        try:
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            self.lock.close()
            raise Failure("state directory is already owned by another lab instance")
        self.log = (self.state / "events.jsonl").open("a", encoding="utf-8")
        self.log_bytes = (self.state / "events.jsonl").stat().st_size
        os.chmod(self.state / "events.jsonl", 0o600)

    def record(self, stream, line):
        encoded = json.dumps({"stream": stream, "line": line}, ensure_ascii=True) + "\n"
        with self.log_lock:
            if self.log_bytes + len(encoded.encode()) <= 2 * 1024 * 1024:
                self.log.write(encoded)
                self.log.flush()
                self.log_bytes += len(encoded.encode())
            else:
                self.log_truncated += 1

    def start(self, link="", bind=None, drop_every=None, response_delay=None):
        if self.process and self.process.poll() is None:
            raise Failure("refusing to start a second live process with the same identity")
        self.events = queue.Queue(maxsize=512)
        self.reader_error = None
        self.last_errors.clear()
        cfg = self.config
        address = bind or cfg.bind
        command = [self.binary, "-state", str(self.state), "-bind", address,
                   "-duration", f"{cfg.duration}s", "-probe-timeout", f"{cfg.timeout}s",
                   "-join-timeout", f"{cfg.timeout}s", "-allow-cidrs", cfg.allow,
                   "-discover-cidrs", cfg.discover,
                   "-drop-every", str(cfg.drop_every if drop_every is None else drop_every),
                   "-response-delay", cfg.response_delay if response_delay is None else response_delay]
        if cfg.advertise:
            command += ["-advertise", cfg.advertise]
        if cfg.nonlocal_bind:
            command += ["-allow-nonloopback"]
        if link:
            command += ["-link", link]
        self.process = subprocess.Popen(command, cwd=ROOT, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, text=True, bufsize=1, start_new_session=True,
                                        pass_fds=(self.lock.fileno(),))
        self.threads = []

        def collect(pipe, stream):
            try:
                for line in pipe:
                    if len(line) > 65536:
                        self.reader_error = "oversized process output"
                        break
                    self.record(stream, line.rstrip("\n"))
                    if stream == "stderr": self.last_errors.append(line.rstrip("\n")[:512])
                    if stream == "stdout":
                        event = json.loads(line)
                        if not isinstance(event, dict) or "event" not in event:
                            raise ValueError("invalid diagnostic event")
                        if event["event"] in EVENTS:
                            try:
                                self.events.put_nowait(event)
                            except queue.Full:
                                self.reader_error = "bounded observation queue overflow"
                                break
            except (ValueError, OSError) as error:
                self.reader_error = str(error)
            finally:
                pipe.close()

        for pipe, stream in ((self.process.stdout, "stdout"), (self.process.stderr, "stderr")):
            thread = threading.Thread(target=collect, args=(pipe, stream), daemon=True)
            thread.start()
            self.threads.append(thread)
        self.ready = self.wait({"ready"}, cfg.timeout + 3)
        return self.ready

    def send(self, command):
        if len(command) > 4095 or "\n" in command or "\r" in command:
            raise Failure("invalid/bounded diagnostic command")
        if not self.process or self.process.poll() is not None:
            raise Failure("diagnostic process exited")
        try:
            self.process.stdin.write(command + "\n")
            self.process.stdin.flush()
        except (BrokenPipeError, OSError) as error:
            raise Failure(str(error)) from error

    def wait(self, kinds, timeout, nonce=None, errors=True):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if STOP.is_set():
                raise Failure("cancelled")
            if self.reader_error:
                raise Failure(self.reader_error)
            try:
                event = self.events.get(timeout=min(0.1, max(0.001, deadline - time.monotonic())))
            except queue.Empty:
                if self.process.poll() is not None:
                    raise Failure(f"process exited with status {self.process.returncode}; inspect private events.jsonl")
                continue
            if errors and event["event"] in ("command_error", "join_failed"):
                raise Failure(json.dumps(event, ensure_ascii=True))
            if event["event"] in kinds and (nonce is None or event.get("nonce") == nonce):
                return event
        raise Failure("deadline awaiting " + ",".join(sorted(kinds)))

    def peers(self):
        self.send("peers")
        return self.wait({"peers"}, self.config.timeout + 1)["peers"] or []

    def join(self, link, budget):
        budget.take()
        self.report["join_attempts"] += 1
        self.send("join " + link)
        result = self.wait({"joined"}, self.config.timeout + 2)
        self.report["join_failures"] += len(result.get("failures", []))

    def stop(self, crash=False):
        p = self.process
        if p is None:
            return
        if p.poll() is None:
            if crash:
                os.killpg(p.pid, signal.SIGKILL)  # only the process group we created
            else:
                try:
                    self.send("quit")
                except Failure:
                    pass
            try:
                p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(p.pid, signal.SIGINT)
                try:
                    p.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    os.killpg(p.pid, signal.SIGKILL)
                    p.wait(timeout=2)
        if p.stdin:
            p.stdin.close()
        for thread in self.threads:
            thread.join(timeout=2)
            if thread.is_alive():
                raise Failure("process output worker failed to stop")
        if self.reader_error:
            raise Failure(self.reader_error)
        self.process = None
        if not crash and p.returncode != 0:
            raise Failure(f"unexpected diagnostic exit {p.returncode}: " + "; ".join(self.last_errors))

    def restart(self, link, budget, drop_every=None, response_delay=None):
        if self.restarts >= self.config.restarts:
            raise Failure("process restart budget exhausted")
        old = self.ready
        self.stop(crash=True)
        self.restarts += 1
        self.report["process_restarts"] += 1
        ready = start_retry(self, link, budget, bind=old["address"], drop_every=drop_every, response_delay=response_delay)
        if any(ready[key] != old[key] for key in ("peer_id", "signing_id", "network_id")):
            raise Failure("restart changed persistent identity/network")
        return ready

    def close(self):
        try:
            self.stop()
        finally:
            self.log.close()
            self.lock.close()


def start_retry(node, link, budget, **kwargs):
    def attempt(index):
        budget.take()
        node.report["process_starts"] += 1
        if link:
            node.report["join_attempts"] += 1
        try:
            ready = node.start(link, **kwargs)
            node.report["join_failures"] += len(ready.get("join_failures") or [])
            return ready
        except Failure:
            node.stop()
            raise
    def failure(index, error):
        node.report["startup_failures"] += 1
        if link:
            node.report["join_failures"] += 1
        node.record("controller", json.dumps({"startup_attempt": index+1, "error": error}))
    return retry(attempt, node.config.attempts, budget.backoff, failure)


def ping(source, target, invitation, cfg, budget, report):
    started = time.monotonic()
    report["operations"] += 1
    operation_id = secrets.token_hex(16)

    def attempt(index):
        budget.take()
        report["probe_attempts"] += 1
        nonce = secrets.token_hex(16)  # fresh attempt correlation; never accept an old pong
        source.send(f"probe {target['peer_id']} {target['signing_id']} {nonce}")
        event = source.wait({"probe_ok", "probe_timeout"}, cfg.timeout + 1, nonce=nonce)
        if event["event"] != "probe_ok" or event["signing_id"] != target["signing_id"]:
            raise Failure("no authenticated response before probe deadline")
        return event

    def failed(index, error):
        report["attempt_failures"] += 1
        if index == 0:
            report["first_attempt_failures"] += 1
        if len(report["failures"]) < 100:
            report["failures"].append({"operation_id": operation_id, "attempt": index + 1, "error": error})

    def pause(index):
        report["retries"] += 1
        budget.backoff(index)
        # Repair using an explicitly supplied invitation, retaining all pins,
        # overlay checks and destination policy. A failed repair is recorded;
        # it never resets the original operation's retry budget.
        if invitation:
            crashed = source.process is None or source.process.poll() is not None
            if crashed and source.restarts >= cfg.restarts:
                raise Failure("owned-process restart budget exhausted")
            try:
                if crashed:
                    report["detected_crash_restarts"] = report.get("detected_crash_restarts", 0) + 1
                    source.restart(invitation, budget)
                else:
                    source.join(invitation, budget)
            except Failure as error:
                report["join_failures"] += 1
                source.record("controller", json.dumps({"repair_error": str(error)}))

    try:
        event = retry(attempt, cfg.attempts, pause, failed)
    except Failure:
        report["failed_operations"] += 1
        raise
    report["acknowledged_operations"] += 1
    if len(report["rtt_ms"]) < 10000:
        report["rtt_ms"].append(round((time.monotonic() - started) * 1000, 3))
    else:
        report["latency_samples_truncated"] = report.get("latency_samples_truncated", 0) + 1
    return event


def build(binary, race):
    command = [(os.environ.get("GO") or "go"), "build"]
    if race:
        command.append("-race")
    subprocess.run(command + ["-o", str(binary), "./examples/netcheck"], cwd=ROOT, check=True, timeout=180)


def run_profile(cfg):
    report = {"schema": 1, "mode": cfg.mode, "scenario": cfg.scenario, "status": "FAIL",
              "scope": "one host, real TCP/Noise processes; application faults, not packet-level netem",
              "operations": 0, "probe_attempts": 0, "acknowledged_operations": 0, "failed_operations": 0,
              "attempt_failures": 0, "first_attempt_failures": 0, "retries": 0,
              "join_attempts": 0, "join_failures": 0, "process_restarts": 0, "process_starts": 0, "startup_failures": 0,
              "faults": [], "checks": [], "failures": [], "rtt_ms": [], "nodes": [],
              "media": "NOT_RUN", "physical_cross_host_verification": "NOT_RUN",
              "limits": {"attempts": cfg.attempts, "restarts_per_node": cfg.restarts, "rate_per_second": cfg.rate,
                         "duration_seconds": cfg.duration, "nodes": cfg.nodes, "count": cfg.count}}
    report["commit"] = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    report["toolchain"] = subprocess.check_output([(os.environ.get("GO") or "go"), "version"], text=True).strip()
    report["dirty"] = bool(subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).strip())
    nodes = []
    started = time.monotonic()
    temporary = tempfile.TemporaryDirectory(prefix="p2p-netlab-")
    root = Path(temporary.name)
    if cfg.artifacts:
        root = cfg.artifacts.resolve()
        root.mkdir(parents=True, exist_ok=True, mode=0o700)
        if any(root.iterdir()):
            raise Failure("artifacts directory must be empty/new; existing identities/evidence are never overwritten")
    report["artifacts"] = str(root) if cfg.artifacts else "temporary; removed after cleanup"
    budget = None
    binary = root / "netcheck"
    try:
        build(binary, cfg.race)
        budget = Budget(cfg.rate, cfg.duration)
        if cfg.mode in ("remote", "attach"):
            report["scope"] = ("operator-run non-loopback endpoint probes; physical host separation requires owner evidence" if cfg.mode == "remote" else "borrowed localhost seed; same-link owned client processes")
            first = Node(binary, root / "client-0", cfg, report)
            nodes.append(first)
            ready = start_retry(first, cfg.link, budget)
            connected = (ready.get("joined") or {}).get("Connected", [])
            if not connected:
                raise Failure("no authenticated bootstrap")
            candidates = [p for p in connected if ipaddress.ip_address(p["address"].rsplit(":", 1)[0].strip("[]")).is_loopback == (cfg.mode == "attach")]
            if not candidates:
                raise Failure("endpoint scope differs from selected attach/remote mode; localhost cannot be relabelled cross-host")
            pin = candidates[0]
            report["bootstrap_target"] = pin
            target = {"peer_id": pin["peer_id"], "signing_id": pin["signing_id"]}
            host_link = cfg.link
            for index in range(1, cfg.nodes):
                node = Node(binary, root / f"client-{index}", cfg, report)
                nodes.append(node)
                start_retry(node, host_link, budget)
        else:
            seed = Node(binary, root / "seed", cfg, report)
            nodes.append(seed)
            ready = start_retry(seed, "", budget)
            target = ready
            report["bootstrap_target"] = {k:ready[k] for k in ("peer_id","signing_id","address","network_id")}
            host_link = ready["invitation"]
            for index in range(1, cfg.nodes):
                node = Node(binary, root / f"peer-{index}", cfg, report)
                nodes.append(node)
                start_retry(node, host_link, budget)
        for node in nodes:
            report["nodes"].append({k: node.ready[k] for k in ("peer_id", "signing_id", "network_id", "address")})
        clients = nodes if cfg.mode in ("remote", "attach") else nodes[1:]
        # Baseline requires a signed nonce response, never just Send or ready.
        for node in clients:
            ping(node, target, host_link, cfg, budget, report)
        report["checks"].append("same-link authenticated baseline")
        # A deliberately wrong expected signer must be rejected before a ping.
        client = clients[0]
        client.send(f"probe {target['peer_id']} {client.ready['signing_id']}")
        event = client.wait({"command_error", "probe_ok"}, cfg.timeout + 1, errors=False)
        if event["event"] != "command_error":
            raise Failure("identity substitution was accepted")
        report["checks"].append("wrong expected signer rejected")
        if cfg.scenario in ("stress", "all"):
            for index in range(cfg.count):
                ping(clients[index % len(clients)], target, host_link, cfg, budget, report)
            report["checks"].append("rate-limited request/response stress")
        if cfg.scenario in ("chaos", "all"):
            victim = clients[-1]
            victim.stop(crash=True)
            report["faults"].append("SIGKILL owned client; operation retry must detect and restart same identity/port/network")
            ping(victim, target, host_link, cfg, budget, report)
            if cfg.mode == "local":
                seed, survivor = nodes[0], nodes[1]
                # Retain a separately authenticated survivor invitation before
                # killing the seed. The immutable original link cannot invent
                # a reachable bootstrap after its only endpoint disappears.
                survivor_link, survivor_ready = survivor.ready["invitation"], survivor.ready
                for peer in nodes[2:]:
                    ping(peer, survivor_ready, survivor_link, cfg, budget, report)
                seed.stop(crash=True)
                report["faults"].append("SIGKILL owned seed; connected survivors remain")
                for peer in nodes[2:]:
                    ping(peer, survivor_ready, survivor_link, cfg, budget, report)
                seed.restart(survivor_link, budget, drop_every=2, response_delay="150ms")
                target, host_link = seed.ready, seed.ready["invitation"]
                report["faults"].append("owned responder delay=150ms, omit every second pong")
                # Two operations force at least one omitted reply; retries and
                # operation-level elapsed time remain in the report.
                for _ in range(2):
                    ping(survivor, target, host_link, cfg, budget, report)
                report["checks"].append("survivor communication; bounded fallback rejoin; delayed/lost response retry")
            else:
                report["checks"].append("owned client crash/restart; remote server never controlled")
        report["status"] = "PASS"
    except (Failure, OSError, subprocess.SubprocessError, ValueError) as error:
        report["error"] = str(error)
    except KeyboardInterrupt:
        report["status"] = "FAIL"
        report["error"] = "cancelled"
        raise
    finally:
        for node in reversed(nodes):
            try:
                node.close()
            except (Failure, OSError, subprocess.SubprocessError) as error:
                report["status"] = "FAIL"
                report.setdefault("cleanup_errors", []).append(str(error))
        report["log_lines_truncated"] = sum(n.log_truncated for n in nodes)
        report["duration_seconds"] = round(time.monotonic() - started, 3)
        times = sorted(report.pop("rtt_ms"))
        report["operation_ms"] = {"p50": times[len(times)//2], "max": times[-1]} if times else {}
        if cfg.output:
            cfg.output.parent.mkdir(parents=True, exist_ok=True)
            cfg.output.write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, indent=2))
        temporary.cleanup()
    return 0 if report["status"] == "PASS" else 1


def terminal(cfg):
    state = cfg.state or ROOT / ".netlab" / f"{cfg.mode}-{cfg.port}"
    build_root = ROOT / ".netlab"
    build_root.mkdir(mode=0o700, exist_ok=True)
    binary = build_root / f"netcheck-{os.getpid()}"
    build(binary, cfg.race)
    counters = {name:0 for name in ("operations","probe_attempts","acknowledged_operations","failed_operations","attempt_failures","first_attempt_failures","retries","join_attempts","join_failures","process_restarts","process_starts","startup_failures")}
    counters.update(failures=[],rtt_ms=[])
    node = None
    try:
        node = Node(binary, state, cfg, counters)
        budget = Budget(cfg.rate,cfg.duration)
        ready = start_retry(node,cfg.link,budget)
        print("LINK=" + ready["invitation"], flush=True)
        print("Commands: peers | ping | join | probe PEER SIGNER | disconnect PEER | quit", flush=True)
        # Forward user commands; ping is a convenience over verified peer
        # snapshots, not a new engine acknowledgement API.
        print(json.dumps({"event":"startup","failed_attempts":counters["startup_failures"],"log":str(node.state/"events.jsonl")}),flush=True)
        commands = queue.Queue(maxsize=64)
        input_done = threading.Event()
        def read_commands():
            while True:
                line = sys.stdin.readline(4097)
                if not line: break
                if len(line) > 4096:
                    while line and not line.endswith("\n"): line = sys.stdin.readline(4097)
                    line = "oversized-command\n"
                while node.process and node.process.poll() is None:
                    try:
                        commands.put(line, timeout=0.1)
                        break
                    except queue.Full:
                        if STOP.is_set(): return
            input_done.set()
        threading.Thread(target=read_commands, daemon=True).start()
        while node.process.poll() is None:
            try:
                line = commands.get(timeout=0.1)
            except queue.Empty:
                if input_done.is_set(): break
                continue
            if line is None: break
            line = line.strip()
            if line == "quit": break
            try:
                if line == "ping":
                    peers = node.peers()
                    targets = [{"peer_id":p["ID"],"signing_id":p["SigningID"]} for p in peers[:16]]
                    if not targets:
                        pins = (ready.get("joined") or {}).get("Connected",[])
                        targets = [{"peer_id":p["peer_id"],"signing_id":p["signing_id"]} for p in pins]
                    if not targets: print(json.dumps({"event":"no_connected_peers"}), flush=True)
                    for target in targets:
                        print(json.dumps(ping(node,target,cfg.link,cfg,budget,counters)),flush=True)
                elif line == "peers":
                    print(json.dumps({"peers": node.peers()}), flush=True)
                else:
                    budget.take()
                    node.send(line)
                    if line.startswith("probe "):
                        print(json.dumps(node.wait({"probe_ok", "probe_timeout"}, cfg.timeout + 1)), flush=True)
                    elif line.startswith("join"):
                        print(json.dumps(node.wait({"joined"}, cfg.timeout + 2)), flush=True)
                    elif not line.startswith("disconnect "):
                        print(json.dumps(node.wait({"command_error"}, cfg.timeout + 1, errors=False)), flush=True)
            except Failure as error:
                print(json.dumps({"event":"command_error","error":str(error)}),flush=True)
    finally:
        try:
            if node is not None: node.close()
        finally:
            binary.unlink(missing_ok=True)
    return 0


def options(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("host", "join", "local", "attach", "remote"))
    parser.add_argument("--link", default=os.getenv("LINK", ""))
    parser.add_argument("--port", type=int, default=int((os.getenv("PORT") or "45001")))
    parser.add_argument("--bind", default=os.getenv("BIND", ""))
    parser.add_argument("--advertise", default=os.getenv("ADVERTISE", ""))
    parser.add_argument("--allow", default=os.getenv("ALLOW_CIDRS") or None)
    parser.add_argument("--discover", default=os.getenv("DISCOVER_CIDRS", ""))
    parser.add_argument("--state", type=Path, default=os.getenv("STATE") or None)
    parser.add_argument("--artifacts", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--scenario", choices=("smoke", "stress", "chaos", "all"), default="all")
    parser.add_argument("--nodes", type=int, default=int((os.getenv("NODES") or "3")))
    parser.add_argument("--count", type=int, default=int((os.getenv("COUNT") or "20")))
    parser.add_argument("--rate", type=int, default=int((os.getenv("RATE") or "5")))
    parser.add_argument("--attempts", type=int, default=int((os.getenv("ATTEMPTS") or "3")))
    parser.add_argument("--restarts", type=int, default=int((os.getenv("RESTARTS") or "3")))
    parser.add_argument("--timeout", type=int, default=int((os.getenv("TIMEOUT") or "2")))
    parser.add_argument("--duration", type=int, default=int(os.getenv("DURATION")) if os.getenv("DURATION") else None)
    parser.add_argument("--race", action="store_true", default=(os.getenv("RACE") or "1") == "1")
    parser.add_argument("--nonlocal-bind", action="store_true", default=os.getenv("ACK_NONLOCAL", "0") == "1")
    parser.add_argument("--ack-remote", action="store_true", default=os.getenv("ACK_REMOTE", "0") == "1")
    parser.add_argument("--drop-every", type=int, default=int((os.getenv("DROP_EVERY") or "0")))
    parser.add_argument("--response-delay", default=(os.getenv("RESPONSE_DELAY") or "0s"))
    cfg = parser.parse_args(argv)
    if cfg.duration is None: cfg.duration = 3600 if cfg.mode in ("host","join") else 180
    if cfg.allow is None: cfg.allow = "" if cfg.mode == "remote" else "127.0.0.1/32"
    limits = (("port", 0, 65535), ("nodes", 2, 8), ("count", 1, 10000), ("rate", 1, 20),
              ("attempts", 1, 5), ("restarts", 0, 10), ("timeout", 1, 10), ("duration", 10, 3600), ("drop_every", 0, 10000))
    for name, low, high in limits:
        if not low <= getattr(cfg, name) <= high:
            parser.error(f"{name} must be {low}..{high}")
    if cfg.mode in ("join", "attach", "remote") and not cfg.link:
        parser.error("LINK or --link is required")
    if cfg.link and (len(cfg.link) > 2048 or not cfg.link.startswith("p2p://invite/") or any(c.isspace() for c in cfg.link)):
        parser.error("invalid/bounded invitation URI; Go parser validates canonical bytes and fields")
    if cfg.mode == "remote" and not cfg.ack_remote:
        parser.error("remote load requires ACK_REMOTE=1 / --ack-remote for an owner-authorized endpoint")
    if not cfg.bind:
        cfg.bind = f"127.0.0.1:{cfg.port}" if cfg.mode in ("host", "join") else "127.0.0.1:0"
    try:
        host, port = cfg.bind.rsplit(":", 1)
        address = ipaddress.ip_address(host.strip("[]"))
        if cfg.mode in ("local","attach") and not address.is_loopback:
            raise ValueError()
        if address.is_unspecified or not 0 <= int(port) <= 65535 or (not address.is_loopback and not cfg.nonlocal_bind):
            raise ValueError()
    except ValueError:
        parser.error("BIND must be a numeric interface:port; non-loopback needs ACK_NONLOCAL=1")
    if cfg.mode in ("local", "attach", "remote") and cfg.bind.rsplit(":", 1)[1] != "0":
        parser.error("automated profiles require BIND=IP:0 so owned nodes get distinct ports")
    if cfg.mode in ("local", "attach", "remote") and cfg.advertise:
        parser.error("automated multi-node profiles cannot share one advertised port; use reachable interfaces or terminal nodes")
    return cfg


def main():
    os.umask(0o077)
    cfg = options()
    def stop(signum, frame):
        STOP.set()
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, stop)
    try:
        return terminal(cfg) if cfg.mode in ("host", "join") else run_profile(cfg)
    except (Failure, OSError, subprocess.SubprocessError) as error:
        print(json.dumps({"status": "FAIL", "error": str(error)}), file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        STOP.set()
        return 130


if __name__ == "__main__":
    raise SystemExit(main())
