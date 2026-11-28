# Bounded network lab: terminals, shared links, stress and chaos

This is an experimental control-plane test lab using the public Go engine and
existing `examples/netcheck`. It runs real processes on real TCP/Noise connections,
waits for signed nonce responses, and records retry/repair failures. It is not a
production daemon, media implementation, security audit or exhaustive test of
every possible network schedule. No engine API, admission policy, crypto primitive,
wire/invitation/identity format or licensing changes are introduced.

## Prerequisites

Use Linux, macOS, or Linux inside WSL2; native Windows is not supported by the
engine's Unix LAN dependencies. Install Go >=1.24.1, a C compiler for `-race`,
Python 3.9+ and Make. Ensure `go version` in PATH selects Go. Default lab builds use
the race detector; `RACE=0` explicitly selects an ordinary diagnostic build.
Use a local filesystem supporting the identity-file API's hard links and POSIX
locks. In WSL, prefer its Linux filesystem for private state.

## First terminal: create the network and copy the link

From the repository root:

```sh
make net-host PORT=45001
```

Wait for the printed `LINK=p2p://invite/...`. Copy only the URI, including its
entire base64url suffix. The diagnostic automatically answers valid pings. It
prints the private event-log path and accepts these commands:

```text
peers
ping
join
join p2p://invite/...
probe NOISE_PEER_ID EXPECTED_SIGNING_ID
probe NOISE_PEER_ID EXPECTED_SIGNING_ID FRESH_32_HEX_NONCE
disconnect NOISE_PEER_ID
quit
```

`ping` checks up to 16 connected verified peers, or the originally pinned
bootstrap when disconnected. It retries within ATTEMPTS and the shared rate/time
budget, attempting original-link rejoin where available. A raw `probe` command
is a single diagnostic attempt. Nothing infers creator permissions from joining.
An empty peer list is shown as such, not a successful ping.

## Second and third terminals: join the SAME link

Replace the quoted placeholder with the exact URI printed in terminal one:

```sh
make net-join PORT=45002 LINK='p2p://invite/PASTE_THE_FULL_SUFFIX'
# In another terminal:
make net-join PORT=45003 LINK='p2p://invite/PASTE_THE_FULL_SUFFIX'
```

Type `peers`, then `ping`. A `probe_ok` includes the verified signing key, nonce
and round-trip milliseconds. Send/enqueue alone is never considered a response.
Each process has its own port and identity/state directory. Never give live nodes
the same STATE. Local defaults are `.netlab/host-45001` and
`.netlab/join-45002`, etc. To restart an instance, repeat its command with the
same port/state/link. Identity and overlay are reloaded, not silently rotated.
The terminal startup uses bounded retry; it does not supervise the host forever
or silently restart a crashed manual terminal. The automated profiles do supervise
only their own test children.

Manual terminals default to one hour; automated profiles default to 180 seconds
of network work after compilation. Invitations expire after one hour. Recreating
the host with the same private state and fixed port can restore the old link
while it is unexpired. A new identity, new port, expired link or unavailable sole
bootstrap needs refreshed discovery information. An immutable A-only invitation
cannot magically discover B after A is permanently gone.

## One command against that existing network

```sh
make net-attach LINK='p2p://invite/PASTE_THE_FULL_SUFFIX' NET_REPORT=.netlab/attached.json
```

This creates bounded owned clients on distinct OS-selected loopback ports, all
using that SAME invitation. It checks authenticated responses and signer rejection,
runs rate-limited traffic, and detects a SIGKILLed owned client during an
operation, restarting it within its configured budget with the same
identity/port. It never kills the borrowed first-terminal host. A PASS means the
selected diagnostic operations completed within their recovery budget; inspect
first-attempt failures, retries and repairs rather than assuming no loss.

For an isolated network whose seed can safely be crashed by the controller:

```sh
make net-local NET_REPORT=.netlab/local.json
make net-stress COUNT=1000 RATE=20 DURATION=300 NET_REPORT=.netlab/stress.json
make net-chaos NET_REPORT=.netlab/chaos.json
```

The `all` local profile (net-local's default) covers baseline request/reply,
expected-key substitution rejection, stress, client crash, seed loss with
survivor traffic, explicit rejoin through a separately authenticated survivor,
identity/port continuity, delayed responses and omitted pongs. Surviving peers'
public invitations are retained by the controller before killing its own seed;
this is explicit fallback, not new invitation indirection or automatic partition
healing. Receiver delay/loss is an application fault, not kernel packet loss.

## The full suite, with one Make command

```sh
# All phases run even when another phase fails:
make net-all NET_REPORT=.netlab/all.json
# Also exercise the first terminal's actual link:
make net-all LINK='p2p://invite/PASTE_THE_FULL_SUFFIX' NET_REPORT=.netlab/all-linked.json
```

The suite runs controller unit tests, vet, uncached ordinary/race suites,
dependency boundaries, invitation vectors, independent consumer, module checksum
verification, pinned redacted history secret scanning, repeated
transport/DHT/channel and engine/application tests, four existing fuzz targets,
echo/Park/netcheck demos, the Make terminal flow, isolated owned chaos and
optional same-link clients. It then runs the existing explicit qualification
gates. Ordinary tests keep their normal short, current-behavior scope; longer
stress and qualification are explicit commands.

**Overall net-all currently exits nonzero.** Receiver-pressure feedback and
concurrent discovery/manual-join settlement remain unqualified, and real-device
media is NOT RUN. Failed gates are not reclassified as expected-success. The
suite records separate phase exit codes and continues through other checks.
`make net-qualify` invokes those release gates directly. A successful bounded
recovery profile does not waive the stricter no-loss/no-failed-trial requirements.

`NET_REPORT` points to the aggregate JSON. A fresh adjacent log directory retains
phase commands, statuses, logs and nested reports; reruns preserve old logs and
update the chosen summary. Individual profiles report observed attempts,
acknowledged operations, exhausted operations, startup failures, join failures,
retry counts, process restarts, identities/ports, injected faults and operation
durations. Operation duration includes pacing, backoff and repair; the Go
`probe_ok.rtt_ms` is measured from its specific probe. Neither is a production SLO.

## Limits and fault settings

| Setting | Default / allowed range |
| --- | --- |
| `NODES` | 3; 2..8 owned processes (local includes seed; attach/remote are clients) |
| `COUNT` | 20 stress operations; 1..10000 |
| `RATE` | 5; 1..20 controller requests/second shared across clients and repairs |
| `ATTEMPTS` | 3; 1..5 per idempotent diagnostic operation/startup |
| `RESTARTS` | 3; 0..10 planned restarts per owned node |
| `TIMEOUT` | 2; 1..10 seconds per probe/join attempt |
| `DURATION` | 180 automatic / 3600 terminal; 10..3600 seconds |
| `SOAK_ROUNDS` | 20 for full-suite qualification; 1..1000 |
| `FUZZ_SECONDS` | 5 per target in full suite; 1..60 |
| `DROP_EVERY` | 0 off; 1..10000 omits every Nth pong |
| `RESPONSE_DELAY` | 0s; 0..2s, e.g. 150ms; delayed responses use a bounded queue of 16 |
| `STATE` | Explicit private directory for manual instances; distinct per live node |

Examples for manual fault experiments:

```sh
make net-host PORT=45001 DROP_EVERY=2 RESPONSE_DELAY=150ms
make net-attach LINK='p2p://invite/...' COUNT=50 ATTEMPTS=3 TIMEOUT=2
```

Retries use fresh attempt nonces, capped exponential jittered backoff (at most
2 seconds per pause), original expected identities, unchanged overlay checks and
independent destination approval. A repair never resets an operation's budget.
Latency samples cap at 10000 and record truncation; summaries include retry and
first-failure counts. No unbounded worker-per-packet, unbounded retry loop or
queue growth is added.
These controls apply to this controller; they are not an engine-wide rate promise.
Only idempotent probes are automatically resent. State-changing actions need the
separate stable-ID/acknowledgement/reconciliation model in
[RELIABILITY_QUALIFICATION.md](RELIABILITY_QUALIFICATION.md).

The reply fault queue is 16; diagnostic pending probes are 16; controller event
queues are 512, terminal command queues 64; per-node event files cap at 2 MiB
and explicitly count truncated lines. A restart cycle may use at most ATTEMPTS
startup attempts; total process starts are bounded by NODES * ATTEMPTS *
(1 + RESTARTS) and the duration budget. All attempts remain in the report. Observation queue overflow fails the run.
Startup/operation retries, process lifetime and suite phases all have deadlines.
Process locks survive into owned Go children, preventing concurrent reuse of a
live node's private state. Terminal EOF, quit, Ctrl-C, duration and controller
cleanup close owned processes; abrupt controller SIGKILL can leave its child
until that child's bounded lifetime expires. No foreign PID is accepted as input.

## Actual other-host testing: same link, no automatic public deployment

The link must advertise an endpoint reachable from the other machine. A link
containing 127.0.0.1 refers to the receiving machine itself; it cannot cross hosts.
Example private LAN addresses below MUST be replaced with owner-approved ones.
Keep authentication/replay/admission/resource controls enabled.

On host A (example 192.168.50.10):

```sh
make net-host PORT=45001 BIND=192.168.50.10:45001 \
  ADVERTISE=192.168.50.10:45001 ACK_NONLOCAL=1 \
  DISCOVER_CIDRS=192.168.50.11/32
```

Authorize inbound **TCP 45001 only from host B's actual source IP** in host A's
existing firewall. Do not disable firewalls or open broad ranges. Transfer the
public link through a trusted channel and compare expected public key IDs.
No private keys, passwords or identity files should be transferred. Remote
load mode does not implicitly approve private/loopback destinations: set the
operator-approved ALLOW_CIDRS explicitly for a private LAN/VPN endpoint.

On host B, run a bounded outbound test (its own listeners stay loopback by default):

```sh
make net-remote LINK='p2p://invite/PASTE_HOST_A_LINK' ACK_REMOTE=1 \
  ALLOW_CIDRS=192.168.50.10/32 COUNT=100 RATE=5 NET_REPORT=.netlab/remote.json
# All local regressions plus the same remote link, with per-phase results:
make net-remote-all LINK='p2p://invite/PASTE_HOST_A_LINK' ACK_REMOTE=1 \
  ALLOW_CIDRS=192.168.50.10/32 COUNT=100 RATE=5 NET_REPORT=.netlab/remote-all.json
```

Remote automation never kills/restarts the remote bootstrap. It supervises its
own clients and records unsupported remote faults as unperformed. The isolated
local-chaos phase remains loopback even when remote environment settings exist.
For delay/loss on the remote responder, its owner may explicitly start it with
DROP_EVERY/RESPONSE_DELAY; the controller does not configure a remote machine.
A loopback bootstrap is rejected by remote mode rather than renamed WAN evidence.

For a bidirectional LAN peer rather than outbound-only testing, use a second
manual terminal on host B with a fixed listener:

```sh
make net-join PORT=45002 LINK='p2p://invite/PASTE_HOST_A_LINK' \
  BIND=192.168.50.11:45002 ADVERTISE=192.168.50.11:45002 ACK_NONLOCAL=1 \
  ALLOW_CIDRS=192.168.50.10/32 DISCOVER_CIDRS=192.168.50.10/32
```

Then authorize host B TCP 45002 from host A's exact source IP and probe both
ways. Use narrow /32 scopes for additional authorized machines. Multi-process
automation uses IP:0 to avoid collisions and refuses one shared advertised port;
do not open an ephemeral inbound range merely to make a test pass.

For Windows use WSL2 Linux; a genuinely reachable WSL mirrored/routed endpoint
and Windows/Hyper-V/Linux firewall rules may all be needed. See the existing
[cross-host plan](CROSS_HOST_TEST_PLAN.md) for scoped firewall/partition/recovery
procedures and the [results template](CROSS_HOST_RESULTS_TEMPLATE.md) for evidence.
macOS Application Firewall permits the built executable; managed/PF rules can
still block it. Record and remove only test-specific firewall changes you made.

Different networks need an already authorized routed/VPN path or reachable
owner-managed endpoint. The engine does not implement STUN/TURN, hole punching,
UPnP or a public relay service. A shared link does not create a route through NAT.
Numeric address approval does not establish application trust or grant roles.

Scripts cannot independently prove two physical computers were used. Remote
reports identify their control-plane scope and retain physical-host/media rows
as NOT RUN/owner-evidence-required. Record actual hosts, OS/toolchains, interfaces,
source/destination IPs, pins, firewall rules, direct/relayed paths and observations
in the owner template. Localhost, containers or one host's LAN IP are not proof
of genuine cross-host, WAN, mobile or playback success.

## CI, artifacts and cleanup

`make net-ci` runs short controller tests, the owned local smoke/chaos profile,
the real Make host/join/ping flow, borrowed-seed isolation, false-remote rejection
and exhausted-retry checks. CI uploads only the summary JSON, never the private node-state directories.
The existing CI retains ordinary/race/security suites.
Long full-suite and strict qualification commands remain explicit.

`.netlab/` is ignored by Git. It contains plaintext private identity files: never
publish it or reuse/copy its identities between physical hosts. Reports/logs
contain public keys and topology and may still need redaction before sharing.
Default automated states/binaries are disposable and removed after owned-process
cleanup; optional `--artifacts NEW_EMPTY_DIRECTORY` retains a profile's private
state/logs and refuses to overwrite old evidence. Manual state persists for
restart tests. To end terminals, type quit/Ctrl-C and confirm process exit before
removing only your own disposable directories. No cleanup command deletes
arbitrary user state, branches, or someone else's running process.

The existing best-effort queue loss, duplicate-dial, unsigned invitation-source,
Sybil/resource, replay, DHT freshness and application authority limitations remain.
This lab measures selected behavior and bounded recovery; it does not make the
project production-ready or independently security-audited.
