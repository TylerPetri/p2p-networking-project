# Controlled owner-run cross-host verification

## Evidence boundary

| Scenario | Evidence in this pass |
| --- | --- |
| Unit/adversarial/race/fuzz and deterministic invitation interoperability | Automated local/CI tests |
| Three-node echo, Park and separate-process diagnostic round trips/fallback | **One host, loopback only** |
| UDP discovery implementation | Isolated UDP tests, not proof of physical multi-machine LAN discovery |
| Two independent machines on one LAN | **NOT EXECUTED** — owner procedure below |
| Different authorized networks, real partition/firewall recovery, NAT | **NOT EXECUTED** — owner procedure below |

The managed environment provides one executor, no configured VPN and no outbound
TCP destination grants. Containers, namespaces, loopback aliases or two processes
would not establish genuine cross-host evidence. No public development listener
or external route/firewall change was made here. Do not mark the unexecuted rows
passed from a local demonstration.

## Diagnostic consumer and authorization

`examples/netcheck` imports only public engine APIs and emits JSON lines. It stores
an opaque private identity plus network ID, writes a **public** invitation file,
and sends random nonce challenges. A probe passes only after receiving the same
nonce signed by the expected Ed25519 identity before its deadline. Send/enqueue
success alone never marks a probe passed. Unknown transport-ID claims do not
choose reply destinations. There are at most 16 pending probes, bounded commands
and payloads, a bounded process lifetime and unchanged engine security defaults.

Defaults bind loopback. Binding a non-loopback address requires the explicit
`-allow-nonloopback` flag. Use it only after the owner authorizes the exact hosts,
addresses, ports and network path. The commands below are **instructions**, not
an approval to expose nodes publicly. Use private LAN or existing authorized VPN/
routed addresses. Do not establish a public relay, open broad inbound ranges,
disable a firewall, weaken pins/replay/admission, or move private identity files
between hosts to make a test pass.

Commands accepted on stdin:

```text
peers
probe <Noise PeerID> <expected Ed25519 SigningID>
disconnect <Noise PeerID>
quit
```

Output includes ready/peers/peer_event, probe_sent/probe_ok/probe_timeout and
command_error. Probe results authenticate a key, not a human/community. CLI stdin
is process-owned, not an embeddable lifecycle API. The engine closes on quit,
EOF, duration or SIGINT; the process smoke test verifies exit and fallback. No
HTTP server or invitation-fetching service is provided.

## Setup: two real machines, three node processes

Use the same reviewed commit and toolchain on both machines; record `git rev-parse
HEAD`, `go version`, OS and actual topology. Build independently on each:

```sh
go build -o /tmp/p2p-netcheck ./examples/netcheck
umask 077
mkdir -p /tmp/p2p-phase4
```

Example addresses below **must be replaced by actual owner-authorized addresses**:
Host A `192.168.50.10`, Host B `192.168.50.11`. Verify them with `ip -br address`.
Use node A at A:44001, B at B:44002, C at A:44003; optional D at B:44004. Separate
processes A/C are on one physical/independent host; process B must be on another.
Keep independent state directories. Do not share identity.json. Repeat these assignments in each terminal on both hosts:

```sh
P2P_PHASE4_A_IP=192.168.50.10
P2P_PHASE4_B_IP=192.168.50.11
P2P_PHASE4_SCOPE="$P2P_PHASE4_A_IP/32,$P2P_PHASE4_B_IP/32"
```

The owner must configure its existing firewall/security groups for **only** these
source IPs and test ports, retaining SSH/administration policy. If the hosts already
use UFW, the following are narrowly scoped examples, not a command to enable UFW
or override another firewall. Record pre-existing rules; remove only rules added
for this test afterwards.

Host A, when authorized:

```sh
sudo ufw allow proto tcp from "$P2P_PHASE4_B_IP" to "$P2P_PHASE4_A_IP" port 44001 comment p2p-phase4
sudo ufw allow proto tcp from "$P2P_PHASE4_B_IP" to "$P2P_PHASE4_A_IP" port 44003 comment p2p-phase4
```

Host B, when authorized:

```sh
sudo ufw allow proto tcp from "$P2P_PHASE4_A_IP" to "$P2P_PHASE4_B_IP" port 44002 comment p2p-phase4
sudo ufw allow proto tcp from "$P2P_PHASE4_A_IP" to "$P2P_PHASE4_B_IP" port 44004 comment p2p-phase4
```

Other firewalls need equivalent owner-reviewed rules. Never use `ufw disable`,
flush rules, or authorize every host/port. No UDP/LAN-broadcast flag is needed;
this matrix uses explicit invitations and bounded peer referrals.

### 1. LAN baseline and authentication

Host A, terminal A:

```sh
/tmp/p2p-netcheck -state /tmp/p2p-phase4/A -bind "$P2P_PHASE4_A_IP:44001" \
  -allow-nonloopback -discover-cidrs "$P2P_PHASE4_SCOPE" -duration 20m \
  | tee /tmp/p2p-phase4/A.log
```

Read the ready event. Record NetworkID, PeerID and SigningID from this trusted
console. Transfer **only** `/tmp/p2p-phase4/A/invitation.txt` to Host B through an
authenticated channel, for example normal owner-configured SSH with host-key
verification (do not disable SSH verification):

```sh
scp owner@HOST_A:/tmp/p2p-phase4/A/invitation.txt /tmp/p2p-phase4/A-invitation.txt
```

Replace HOST_A/owner with the authorized SSH account/host. On B, set the public
network ID obtained from the trusted A console:

```sh
P2P_PHASE4_NETWORK='PASTE_64_HEX_NETWORK_ID_FROM_A'
/tmp/p2p-netcheck -state /tmp/p2p-phase4/B -bind "$P2P_PHASE4_B_IP:44002" \
  -allow-nonloopback -invite /tmp/p2p-phase4/A-invitation.txt \
  -network "$P2P_PHASE4_NETWORK" -allow-cidrs "$P2P_PHASE4_SCOPE" \
  -discover-cidrs "$P2P_PHASE4_SCOPE" -duration 20m | tee /tmp/p2p-phase4/B.log
```

Replace the quoted PASTE placeholder above with the actual 64-hex ID. Run `peers` on both.
Check each SigningID against the other host's trusted ready event. On B enter
`probe A_PEER_ID A_SIGNING_ID`, using those actual public values. Expect
probe_sent then probe_ok with the expected signer and same nonce. Repeat A→B.
Change the expected SigningID to B's own ID in B's probe command: expect
command_error, never probe_ok. A TCP connection or ready event without the
verified application round trip is insufficient evidence.

Host A, terminal C (set the same P2P_PHASE4_NETWORK first):

```sh
/tmp/p2p-netcheck -state /tmp/p2p-phase4/C -bind "$P2P_PHASE4_A_IP:44003" \
  -allow-nonloopback -invite /tmp/p2p-phase4/A/invitation.txt \
  -network "$P2P_PHASE4_NETWORK" -allow-cidrs "$P2P_PHASE4_SCOPE" \
  -discover-cidrs "$P2P_PHASE4_SCOPE" -duration 20m | tee /tmp/p2p-phase4/C.log
```

Wait for `peers` snapshots to show the expected three-node mesh. Probe B↔C using
trusted console SigningIDs. These are actual cross-host messages if A/B are truly
independent machines. All IP/peer/signing comparisons belong in the result record.

### 2. Bootstrap failure and fallback

Transfer C's public invitation to Host B via the same trusted channel. Enter quit
on A only. Verify B/C remain peers and fresh B↔C probes still pass. On B, attempt a
new D using A's now-stale invitation:

```sh
/tmp/p2p-netcheck -state /tmp/p2p-phase4/D -bind "$P2P_PHASE4_B_IP:44004" \
  -allow-nonloopback -invite /tmp/p2p-phase4/A-invitation.txt \
  -network "$P2P_PHASE4_NETWORK" -allow-cidrs "$P2P_PHASE4_SCOPE" -duration 1m
```

Expect nonzero exit with unavailable bootstrap, not successful readiness. Then
restart D with the same state and C's public invitation, adding the normal discovery
scope. Expect readiness and an authenticated D→C probe. Reusing state must preserve
D's own keys/network; never substitute another host's private identity. A published
multi-endpoint invitation is also covered by automated tests, but a real unavailable
address may consume its bounded five-second attempt before fallback.

### 3. Firewall restriction and partition/recovery

This optional Linux/nftables procedure needs explicit owner authorization/root and
knowledge of the existing firewall. It adds DROP rules for test connections only;
it does not open traffic or weaken engine controls. On Host B, first verify a
table named p2p_phase4 does not already belong to another task. If it exists, stop
and inspect; never flush/delete pre-existing policy. Keep a second administrative
session available. For the ports above, owner-created temporary rules are:

```sh
sudo nft add table inet p2p_phase4
sudo nft 'add chain inet p2p_phase4 input { type filter hook input priority -10; policy accept; }'
sudo nft 'add chain inet p2p_phase4 output { type filter hook output priority -10; policy accept; }'
sudo nft add rule inet p2p_phase4 input ip saddr "$P2P_PHASE4_A_IP" ip daddr "$P2P_PHASE4_B_IP" tcp sport '{ 44001, 44003 }' counter drop
sudo nft add rule inet p2p_phase4 input ip saddr "$P2P_PHASE4_A_IP" ip daddr "$P2P_PHASE4_B_IP" tcp dport '{ 44002, 44004 }' counter drop
sudo nft add rule inet p2p_phase4 output ip saddr "$P2P_PHASE4_B_IP" ip daddr "$P2P_PHASE4_A_IP" tcp sport '{ 44002, 44004 }' counter drop
sudo nft add rule inet p2p_phase4 output ip saddr "$P2P_PHASE4_B_IP" ip daddr "$P2P_PHASE4_A_IP" tcp dport '{ 44001, 44003 }' counter drop
sudo nft list table inet p2p_phase4
```

A table's accept policy does not override drops in existing firewall chains.
Rules target test-server ports and peers, not SSH port 22. Confirm the actual test
and administration tuples before applying; adapt only within owner-approved scope.

Probe B→C and C→B: expect probe_timeout/connection error and increasing DROP
counters, never a fabricated probe_ok. A queued Send can succeed during a blackhole;
that is why application acknowledgements are checked. A timeout alone does not
identify a firewall/NAT cause; preserve the rule counters and control baseline.

For a **new-connection firewall** test, quit D and start it against C while DROP is
active: expect bounded bootstrap failure/nonzero exit. For a **persistent partition**,
keep DROP active beyond the default two-minute complete-envelope idle watchdog
(allow 150 seconds) and inspect peers until affected sessions close. Do not change
the timeout or replay limits to force the test outcome.

Remove only the table this test created:

```sh
sudo nft delete table inet p2p_phase4
```

A short transient partition may recover through existing TCP retransmission; use
a new nonce probe to establish recovery. A long partition may require explicit
rejoin: quit/restart B or D with its same state and C's trusted invitation, then
verify fresh bidirectional probe_ok and unchanged keys/network. Do not claim
automatic partition healing from an explicit restart/rejoin test.

### 4. Different authorized networks

Repeat the same baseline/failure/partition procedure on two actual authorized
networks with an existing routed path or owner-authorized VPN. Replace A/B with
actual routed/VPN IPs and update all exact /32 scopes and firewall source rules.
Confirm route, effective source IP and security-group policy with the owner; NAT
may change observed source addresses and invalidate assumptions in the LAN rules.
Record the real topology and only the scenarios actually executed.

If neither side has a routable/reachable endpoint, record failure/not executable.
This engine has no automatic STUN/TURN, hole punching, UPnP, DNS bootstrap or public
relay service. Do not add public port forwarding, a relay host or another network
exposure without separate authorization. A successful authorized VPN test proves
that path, not arbitrary Internet connectivity or traversal of unrelated NATs.

## Cleanup and evidence

Enter quit/Ctrl-C on every process and confirm exit. Remove only temporary rules
created for this test; with UFW inspect status/unique p2p-phase4 comments and remove
those additions using matching delete rules, preserving pre-existing entries.
Delete disposable state only after reviewing needed evidence; never publish the
identity.json files. Invitation/log files are public routing/key metadata but may
still reveal topology, so redact unnecessary addresses before sharing publicly.
Use [the results template](CROSS_HOST_RESULTS_TEMPLATE.md). Keep every external
scenario NOT RUN until the owner records actual observations.
