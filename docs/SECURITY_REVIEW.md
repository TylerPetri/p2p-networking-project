# Engineering security review — 2026-10-30

Historical Pass 1 findings and test evidence. [Pass 2](PASS2_REVIEW.md) supersedes
the remaining-limitations and publication recommendations below.

This is a source review and targeted testing effort, not an independent security
assessment or a completed security audit. Baseline: commit
`3750f897c38bf1ceacd256d66f52041e939bffc8`.

## Threat model (established before changes)

Assets: process availability, transport confidentiality/integrity, correspondence
between connected peers and routing IDs, signing-key ownership, DHT record
integrity, local persistent state. Trust boundaries: TCP/Noise framing; handshake
identity to Hello/Identify/NAT registration; JSON decoding; UDP discovery and
advertised addresses; DHT RPC replies and records; persisted grants and peer lists.

Adversaries include unauthenticated TCP clients, active network attackers, and
connected peers supplying malformed frames, false identities, replayed records,
poisoned referrals, or slow/incomplete streams. Names, addresses and discovered
keys are untrusted. Noise key possession does not establish a person's identity
or authorize a peer. No PKI, trusted key directory, Sybil resistance or Internet
DoS guarantee is assumed. Local host/key compromise is outside scope.

Review priorities: validate lengths before allocation; preserve stream semantics;
serialize cipher nonce access; fail closed after partial I/O/authentication errors;
bind claimed network IDs to Noise static keys; bound setup time and release
connections on shutdown; validate DHT signatures/keys/sequences and RPC sources;
document replay, discovery and resource limitations.

## Findings by severity

Severity reflects impact in the experimental application, not a CVSS assessment.
No critical finding was established; that is not evidence that none exists.

| Severity | Baseline finding and consequence | Change / evidence |
| --- | --- | --- |
| High | A four-byte transport length was allocated without a maximum, allowing a peer to request up to roughly 4 GiB before authentication of the frame. | Reject ciphertext lengths outside 16..65,535 before body allocation; split larger writes. `TestRejectFrameLengthBeforeBody`, `TestStreamSmallReadsLargeWriteAndShortWrites`. |
| High | Hello could claim an arbitrary network ID; handshake signing keys were unsigned claims, responder identity payload was absent, and Identify/NAT could replace the claimed user. | Export authenticated Noise peer static key; require Hello equality; exchange domain-separated Ed25519 static-key bindings in both XX directions; prevent identity replacement. `TestSessionRejectsSpoofedHelloAndSigningIdentity`, `TestIdentifyAndNATCannotReplaceHandshakeIdentity`. These checks do not add a trusted identity directory. |
| High | Points signature validation used `PlayerID != signerID`, rejecting legitimate snapshots and permitting a signer to claim another player's ID. | Correct equality. `TestSignedSnapshotBindsPlayerIDToSigner`; reproduced rejection on the baseline, with forged-ID rejection checked on the fix. |
| High | An invalid application-channel nonce length reached AEAD `Open`, which panics. A connected peer could crash a recipient processing a matching encrypted channel. | Check nonce length first. `TestDecryptRejectsMalformedNonceAndTamper`; panic reproduced on the baseline. |
| High | Noise setup had no timeout; Stop closed the listener but left established and pending connection reads blocked. | Five-second total setup watchdog; cancellation closes sessions; bounded TCP dial/write; discovery loop observes shutdown; CLI defers cleanup. `TestStopClosesPendingHandshake`, `TestHandshakeSetupTimeout`. Steady-state resource limits remain incomplete. |
| Medium | Transport and handshake writes ignored short successful writes, silently corrupting framing; retries after partial failure could desynchronize nonces. | Complete short writes, reject zero progress, close and remember transport failures. `TestHandshakeFraming`, `TestFailedWriteIsTerminal`; both fail against baseline. |
| Medium | Small read buffers discarded plaintext tails; a fresh JSON decoder after Hello discarded read-ahead. | Retain decrypted tails and the existing buffered decoder. Small-read/large-write test and the valid/coalesced-Hello session test. |
| Medium | Cipher states had no read/write synchronization; concurrent callers could race nonces or interleave frame bytes. Some peer metadata reads occurred outside their mutex. | Independent direction locks; capture metadata under node mutex. `TestHandshakeAndConcurrentDuplex`, `TestConcurrentIdentityMetadata`, repeated existing P2P race harnesses. |
| Medium | DHT RPC responses matched only RPC ID, not source peer or expected response kind. Routing observation before response delivery could block a waiter behind eviction pings. | Bind pending RPCs to peer and response kind; deliver replies before routing observation; discard mismatched/unsolicited replies. `TestRPCRejectsWrongPeerAndKind`. RPC IDs are random; source spoofing requires knowledge of a pending ID. |
| Medium | LAN pongs could advertise an arbitrary third-party host and induce a dial. | Use UDP source IP and a numeric port; deduplicate normalized addresses. `TestLANAdvertisementUsesSenderAddress`. This does not solve spoofed UDP or peer-list dial referrals. |
| Low | Linux build used an unavailable `syscall.SO_REUSEPORT`; documentation claimed X3DH, production readiness and universal signed data without implementation support. | Use the existing x/sys dependency's Unix constant; replace unsupported claims; add CI and a localhost demo. |

The selected framing, short-write, nonce-length and points tests were also run
against a detached baseline checkout and failed as expected. The oversized-frame
case was not executed against the vulnerable allocator to avoid an unnecessary
multi-gigabyte allocation. Identity/DHT source checks were reviewed in source and
exercised adversarially after the changes.

## DHT, discovery and replay review

Existing record validation checks nil/type, 64 KiB value limit, expiry, immutable
SHA-256 keys, mutable public-key length, derived key and Ed25519 signature. The
in-memory store copies byte slices and serializes updates; mutable sequences must
increase. Added adversarial tests alter values, names, signatures, sequence and
expiry, submit wrong hashes/types/sizes, and concurrently replay the same mutable
record. Only one duplicate-sequence insertion succeeds. These tests do not prove
the persistence/republish protocol correct.

Lookups check node-ID derivation against peer ID, address syntax and referral
counts, and bound rounds/candidate counts. Routing buckets have subnet diversity
limits; RPCs have global/per-peer in-flight limits and per-peer token buckets.
None establishes trust in an advertised address or resistance to identities an
attacker can cheaply create. Replies now require the expected connected peer and
kind. The connected transport ID supplied by P2P is used for DHT request origins,
rather than the untrusted envelope `FromID`.

Noise's ordered per-direction AEAD nonces reject repeated, reordered or modified
ciphertexts within a session. Fresh handshakes derive session keys; this review
did not independently verify Noise or its dependency. Tests exercise valid XX in
both directions, repeated ciphertext and modified ciphertext rejection. Nonce
rollover/rekey interoperability and cross-session replay have not been exhaustively
tested. There is no protocol-level durable gossip replay ledger: deduplication is
short-lived. Shared-key ciphertext can be replayed under a new gossip ID. Mutable
record replay protection disappears after eviction, expiry cleanup or restart.

## Remaining limitations and follow-up priorities

- **High — availability:** bounded frames do not bound a JSON document spanning
  many frames. There are no global active/pending connection limits, total DHT
  record quotas or signing-key admission controls. Per-peer rate-limit state
  grows with identities; expired records are not an availability guarantee.
  Authenticated peers can still exhaust memory, CPU, sockets and disk. Add an
  envelope limit, admission budgets, record quotas and load/slow-peer tests before
  hostile-network use.
- **High — automatic dial trust:** peer-list referrals and Hello listen addresses
  can target arbitrary reachable hosts/ports. LAN source-IP binding only narrows
  one path. Add destination policy, referral budgets and expected-key verification
  when dialing referrals before use in privileged networks. The isolated demo
  disables LAN and uses only its own local processes.
- **Medium — identity and sender authenticity:** there is no pinned expected key
  or human identity verification. Signing a Noise key does not make a discovered
  name trustworthy. Gossip `FromID`, chat labels, relayed messages and some quiz
  interactions are not end-to-end authenticated. A relay/member can forge unsigned
  origins. A seed can observe traffic and deny service. Shared channel keys confer
  group membership, not per-sender authenticity.
- **Medium — DHT semantics:** valid mutable records need not be the newest across
  replicas. Lookup may return the first valid value; local publish ignores a store
  sequence rejection, and a first lookup can return a record even if cache insertion
  is rejected. Immutable expiry/creation metadata is not content-hash protected.
  No maximum future TTL/name length, persistent anti-rollback state, Byzantine
  quorum, record ownership policy or Sybil/eclipse guarantee is implemented.
- **Medium — lifecycle/timeouts:** the five-second setup timer bounds the entire
  setup; each TCP write/dial is bounded separately. Quiet established connections
  have no read/idle deadline, so partial frames can hold a reader until shutdown.
  DHT RPCs have timers but are not all context-cancellable; value lookup checks
  context between rounds. Stop triggers cleanup but does not join every goroutine.
  Direct callers of Noise constructors must manage failure cleanup themselves.
  Alternative transports must unblock reads/writes on Close.
- **Low — persistence/platform/verification scope:** identities are ephemeral.
  Concurrent peer-store saves still use the same temporary filename and can lose
  updates; ignored save errors reduce observability. Unix LAN code is not portable
  to Windows. Dependency vulnerability analysis, fuzzing campaigns, formal protocol
  analysis, key lifecycle design, independent review and Internet-scale load testing
  were not completed. Race tests cannot prove all schedules safe. Existing encrypted
  channel stress harnesses do not themselves assert successful decryption; the new
  channel unit test and the localhost demo provide narrower behavioral checks.

## Validation results

Executed on Linux/amd64 using Go 1.27.3 with CGO/race support:

| Check | Result |
| --- | --- |
| Baseline `go test -race ./...` | Failed Linux discovery build (`syscall.SO_REUSEPORT`); built P2P and DHT packages passed. |
| `go vet ./...` | Passed. |
| `go test -race -count=1 ./...` | Passed all packages, including network and new adversarial tests. |
| Transport, DHT, channel and points packages with `-race -count=20` | Passed. |
| Relevant session/gossip/NAT/encrypted/DHT P2P tests with `-race -count=10` | Passed. |
| Session, shutdown and concurrent identity metadata tests with `-race -count=20` | Passed. |
| `P2P_LAN_UDP_TEST=1 go test -race -count=1 ./internal/discovery` | Passed, including opt-in UDP harnesses on the test environment. |
| `python3 scripts/demo.py` | Passed: three loopback processes; Bob received Alice's gossip. |
| `git diff --check` | Passed. |

New CI repeats the principal checks without privileged credentials or deployment.
Hosted CI status must be read from the PR; local results do not establish that
GitHub Actions ran or passed.

## Git-history risk assessment

Fetched origin branches and tags without shallow history; `git ls-remote origin`
advertised only `main` at the baseline SHA and HEAD pointing to it. The baseline
has **one root commit** and no tags. `git fsck --full --no-reflogs` reported no
object errors or dangling objects. Reviewed file inventory and commit metadata;
ran **Gitleaks v8.24.3** with default rules and
`git --log-opts='--all --full-history' --redact`: **one commit scanned, no leaks
found** (about 186 KiB scanned). The final changed tree is scanned again before
submission. No credential rotation or history rewrite was performed.

The commit's author/committer metadata contains a personal email address. Public
history exposes that metadata; this is a privacy consideration, not a detected
credential. The embedded PGP block is a commit signature, not a private key.
Scanner patterns can miss secrets. Deleted remote refs, inaccessible historical
refs, server caches, releases, issue/PR attachments, forks and previous clones
cannot be ruled out by this scan. GitHub metadata already reported the repository
as public when reviewed; visibility was not changed. If any credential was ever
exposed elsewhere, revoke it regardless of this clean scan.

## PUBLICATION GO/NO-GO

**NO-GO for endorsing the current default-branch snapshot as a security-ready
portfolio release.** Review this PR and explicitly accept/document the remaining
availability and discovery risks before selecting a portfolio revision. The
original license wording required clarification. The owner has since authorized
a limited local evaluation/recruitment permission; see the current LICENSE.txt.
The original implementation remains proprietary, not open source.

The proposed branch is suitable for engineering review and a disposable localhost
demonstration, with the limitations prominently disclosed. It is **NO-GO for
production, sensitive data, hostile-network exposure or claims of independently
verified protocol security**. This recommendation does not authorize a visibility
change, merge or deployment; none was performed.
