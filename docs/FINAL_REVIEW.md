# Final review and merge preparation — 2026-10-30

Scope: the complete PR #1 diff against `main` at
`3750f897c38bf1ceacd256d66f52041e939bffc8`, continuing from Pass 2 head
`77f6d4b90b93b0ba2130326cf4ee7ffed67c9750`. This review checks correctness and
regressions for an experimental networking/distributed-systems portfolio. It is
not a formal verification, independent cryptographic audit or production security
certification. The [Pass 2 review](PASS2_REVIEW.md) preserves earlier attack paths
and numeric resource budgets.

## Final correction

**High integrity defect, corrected:** retained DHT records could change type.
Immutable keys are `SHA256(value)` and mutable keys are `SHA256(pubKey || name)`.
Anyone knowing a mutable record's public key/name can construct an immutable value
`pubKey || name` at that same key. This does not require a SHA-256 collision or the
owner's private key. Before the fix, that unsigned immutable record could replace
retained signed mutable data and bypass its sequence rule. The reverse type change
could also replace retained immutable content.

The store now rejects a different type while a record is retained, using
`ErrRecordTypeConflict`. The regression test reproduced both overwrites on the
Pass 2 code, then verifies rejection, unchanged data/byte accounting and no
propagation/ownership marking after rejection. No key derivation or wire protocol
was redesigned.

**Remaining namespace limitation, documented:** the namespace is still shared.
First-writer preemption can deny an intended type; expiry/deletion/restart loses
retained type/sequence evidence. `GetValue` is untyped, so a caller expecting a
mutable record must check that type and its expected key. This is acceptable for
the controlled DHT experiment, not a signed mutable database guarantee. A future
typed API or domain-separated key namespace would be a separate protocol change.

## Protection checks against implementation

| Area | Final assessment |
| --- | --- |
| Framing and messages | Ciphertext lengths are checked before allocation (16..65,535 bytes). Large writes split into bounded frames; newline-delimited JSON has a separate 128 KiB limit across frames. |
| Partial I/O | `io.ReadFull` handles fragmentation; unread plaintext and Hello decoder read-ahead are retained. Full writes handle short progress, reject zero progress, and close on terminal framing/authentication/I/O failure. |
| Peer identity | Both XX directions carry verified Ed25519 signing-key/Noise-static-key bindings. Hello IDs match Noise static keys. Optional outbound pins and referred IDs are checked before admission; Identify/NAT cannot replace the signing identity. |
| Signed gossip | Fixed-field, domain-separated Ed25519 envelope signatures cover payload, origin binding and timestamp. Relays preserve signatures; direct control origins use the authenticated session ID. |
| Replay | Origin-scoped gossip entries cover the five-minute lifetime plus future allowance; encrypted ciphertext hashes have a separate bounded 24-hour cache. Capacity fails closed. State is volatile and does not give durable exactly-once delivery. |
| Channel isolation | XChaCha20-Poly1305 authenticates the channel name as associated data; wrong-channel and malformed-nonce inputs fail. The outer signature identifies the gossip signer, not necessarily the original ciphertext creator. |
| DHT | RPCs require pending ID, source peer and response kind. Stored records validate key/hash/signature/size/expiry; retained sequences and types cannot be bypassed by replacement. Rejected data is not published. No quorum/freshness guarantee. |
| Referrals | Numeric endpoints, scope restrictions, observed-IP Hello hosts, bounded candidates/workers and expected-key checks remain in place. Same-host and approved-range ports can still be probed. |
| Resources | Connection admission precedes dial/worker creation; queues, documents, caches, maps and store budgets are finite. Complete-message, setup, dial and write timeouts prevent indefinite socket retention under the documented transport contract. These are component limits, not hard RSS/fairness guarantees. |
| Lifecycle | Cancellation closes streams and tracked node sessions/writers/workers are joined. Instance-checked cleanup cannot remove a replacement peer. The separate LAN manager exits through cancellation and its bounded UDP timeout. |

## Protocol and demo compatibility

The shipped app uses the shared `proto.ApplicationProtocol` constant,
`park-p2p/0.2.0`. Tests complete a real Noise handshake with a valid identity,
then verify that old `0.1.0`, empty and unknown future Hello versions produce the
specific incompatible-protocol error with zero admitted peers. Valid `0.2.0`
continues to accept coalesced Hello/following-envelope input. The internal Node
abstraction can explicitly use test protocol strings; the CLI offers no version
override.

There is no protocol negotiation or rolling-upgrade promise. Run the same final
revision on all demo nodes. Early experimental PR revisions carrying the same
`0.2.0` label are not an interoperability commitment or build fingerprint. Existing
valid grants are recognized without rewriting the grant database; this is not a
general persisted-state migration guarantee. Ephemeral identities and channel
keys are recreated after restart, so obtain new pins and share new keys as needed.

The actual `scripts/demo.py` builds one binary, launches three loopback processes
with OS-selected ports and isolated temporary state, disables LAN discovery, waits
for connections, and asserts signed chat plus shared-channel decrypt output at Bob.
It suppresses generated channel keys from logs and cleans up processes/state. The
interactive README commands use explicit ports and isolated state directories;
occupied ports must be changed. LAN/WAN/NAT interoperability is not established by
this localhost demo. Only Linux/amd64 was executed during this review; macOS is
an intended Unix target, not a tested result. Windows LAN code remains unsupported.

## Risk triage and scope decision

| Remaining risk | Severity outside the controlled demo | Classification / decision |
| --- | --- | --- |
| Sybil peers occupy slots, fill finite stores/caches, consume CPU/bandwidth | High availability | Acceptable controlled-demo limitation, explicitly documented. No global admission fairness or Internet DoS guarantee. |
| Initial keys/names are untrusted; optional pins depend on an independent trust decision | Medium | Acceptable limitation requiring documentation and narrow operator choices. No PKI or human-identity proof. |
| Port probes on connected hosts or approved CIDRs; spoofed UDP hints | Medium | Acceptable controlled-network limitation; document narrow scopes and avoid privileged/shared networks. |
| Volatile replay evidence, first-seen in-window delivery, clock skew and copied ciphertext rewrapped by another signer | Medium | Document explicitly. A recipient without a prior ciphertext entry can accept a copied ciphertext in a new valid outer signature; displayed signer does not prove original encryption authorship. |
| DHT first-valid freshness, shared type namespace/first-writer preemption, mutable rollback after state loss, unsigned immutable metadata | Medium | Document explicitly; not a consistent or Byzantine-resilient database. Retained-type overwrite defect was corrected before merge. |
| Arbitrary signers issue unfair scores/grants; seed withholding/metadata trust | Medium | Acceptable demonstration authority model; signatures prove issuance, not entitlement or fairness. |
| Untrusted terminal text and labels include display controls | Medium when peers are untrusted | Explicitly documented; use only controlled peers and disposable terminal/data. No terminal hardening claim. |
| Best-effort grant batches, process-local persistence coordination, unsupported repeated/concurrent Start or custom transports ignoring Close | Low to medium | Documented API/operational limits; no new lifecycle architecture added in the final pass. |

No other merge-blocking correctness defect was established by this review. That is
not evidence of absence. There is no independent protocol/dependency security
audit, exhaustive fuzz campaign, formal analysis, secure-memory guarantee or
long-duration hostile-load/RSS ceiling measurement.

## Ownership and dependency review

[LICENSE.txt](../LICENSE.txt) is unchanged. No open-source license, ownership
assignment or new demo-use permission was automatically applied. The owner retains
rights to original code; upstream MIT/BSD permissions and attribution obligations
remain independent. [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md) contains the
pinned runtime inventory and unchanged upstream license/patent texts.

The current original-code license says "confidential" and forbids use/copy/modify
without permission while describing evaluation/demo availability. Decide whether
to remove that word, clarify original-code versus third-party ownership and grant
a narrow build/run evaluation permission. These wording decisions do not require
open-sourcing the original code. They do not block public code review under the
current notice, but demo instructions assume author permission.

No dependency source or executable release is shipped. A future binary release
must include the MIT/BSD notices and assess actual platform/toolchain libraries:
the local CGO-enabled Linux build links glibc, and a BoringCrypto build would need
additional BoringSSL/OpenSSL notices. No binary-distribution compliance certification
is claimed.

## Final verification and merge recommendation

Verification commands are the README/CI commands: full uncached ordinary/race
suites, vet, ten repeated P2P race runs, twenty repeated Noise/DHT/channel race
runs, opt-in UDP discovery, three native fuzz targets and the three-process
signed/encrypted demo. Final commit identity, command results and final-head
GitHub Actions evidence are recorded in
[PR #1](https://github.com/TylerPetri/p2p-networking-project/pull/1) after the final
commit is tested; earlier green CI is not used as a substitute.

**MERGE GO for an experimental portfolio once that final-head verification is
green and the maintainer accepts the disclosed limitations.** Stop hardening at
this scope and recommend merging; do not merge automatically. Production,
sensitive communications, unrestricted hostile-network service and claims of
independently verified security remain **NO-GO**. Repository visibility, published
Git history and deployments are unchanged.
