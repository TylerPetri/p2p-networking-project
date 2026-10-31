# Owner connectivity results — NOT EXECUTED in managed verification

Copy this template for a separately authorized owner run. Do not prefill PASS.
Use local date/time and timezone for the actual run, plus monotonic probe evidence.
Do not include private keys, account credentials or unnecessary personal metadata.

- Reviewed commit(s), toolchains, OS/architecture:
- Owner authorization and exact host/port/network scope:
- Independent machine A / machine B descriptions (redact unnecessary identifying data):
- LAN / routed networks / VPN / NAT / security groups / firewall topology:
- Effective source IPs and approved invitation/referral scopes:
- Public network, PeerID and SigningID references compared through trusted channels:
- How public invitation files were transferred/authenticated:

| Scenario | NOT RUN / PASS / FAIL / BLOCKED | Probe nonce/expected signer and observed result | Relevant logs/counters and limits |
| --- | --- | --- | --- |
| A→B and B→A same-LAN authenticated round trips | NOT RUN | | |
| Expected-identity substitution rejected | NOT RUN | | |
| Three-node mesh with B↔C crossing real hosts | NOT RUN | | |
| Seed A stopped; B↔C fresh messages continue | NOT RUN | | |
| Stale seed fails within attempt/context bounds | NOT RUN | | |
| Rejoin through reachable C preserves own identity | NOT RUN | | |
| New connection under authorized DROP policy fails | NOT RUN | | |
| Established-flow partition yields timeout/errors | NOT RUN | | |
| Persistent partition closes affected sessions | NOT RUN | | |
| Recovery with existing TCP or explicit rejoin (name which) | NOT RUN | | |
| Different authorized networks bidirectional probes | NOT RUN | | |
| Process exit, port release and firewall cleanup | NOT RUN | | |

Record unexpected failures rather than disabling pinning, scopes, replay checks,
limits or firewalls. Distinguish transport admission from application receipt;
probe_sent is not probe_ok. Distinguish controlled refusal from an unexplained
blackhole. State any unexecuted cases and why. No row establishes arbitrary NAT
traversal, Internet-wide reachability or production readiness.
