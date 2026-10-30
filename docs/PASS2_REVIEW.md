# Pass 2 engineering review — 2026-10-30

Continuation of `portfolio/security-review`, PR #1. Starting point:
`d2cb3ad202305752794676f54f47758cbe533043`. The repository was already public.
No merge, visibility change, history rewrite or deployment is part of this work.
This is source review plus targeted testing, not an independent security audit.

## Findings, actual attack paths and disposition

| Severity at start | Attack path and impact | Protection added / remaining boundary |
| --- | --- | --- |
| High | Authenticated peer sends one JSON value over arbitrarily many legal Noise frames; `json.Decoder` accumulates it without a document limit. | Read bounded newline-delimited envelopes before JSON decoding: 128 KiB including newline. Oversize/unterminated values are rejected, including whitespace floods. Buffered Hello continuation is retained. Outbound messages use the same size cap. |
| High | Many clients occupy handshakes/live sockets; slow established peers hold readers; repeated discovery dials or full send queues amplify goroutines. | One default 32-slot budget covers dialing, pending handshakes and live connections. Admission happens before goroutine creation/dial. Duplicate address dials are coalesced. Complete-message watchdog is two minutes, including idle wait; setup/dial/write limits remain five seconds. Queue saturation removes a peer without creating a goroutine per message. Node Stop joins sessions/writers and tracked accept/discovery/referral/DHT workers. TCP dialing and DHT RPC waits observe cancellation. |
| High | A connected peer's peer list, Hello or DHT referrals induce connections to internal services or arbitrary hosts, and a substitute key can answer a referral. | Advertised Hello hosts are replaced by observed transport IP. Bounded automatic discovery accepts numeric endpoints only, rejects link-local/multicast/unspecified addresses, and needs an approved CIDR, a connected peer's own host, or loopback when bound to loopback. Referred Noise IDs must match after XX. Optional operator address/key pins protect explicit outbound connections. DNS remains available for explicit operator bootstraps only. Same-host ports and operator-approved CIDRs remain probeable. |
| High | A peer changes gossip `FromID` to another connected player's network ID; quiz answers can be credited to that player, and chat labels imply a false origin. | Ed25519-signed, timestamped gossip with verified signing-key/Noise-key binding. Signature covers the envelope and payload; relays preserve it. Quiz answers use the signed origin signing key; result notifications require the recorded quiz creator. Unsigned/tampered gossip is dropped. Direct grant-sync/control origins are normalized to the session ID. Names remain untrusted labels; signatures do not make a signer honest. |
| High | A signer supplies unlimited valid immutable records, quiz opens, grants, points identities or peer advertisements; per-record checks do not bound total retained memory/disk. | DHT count/byte budgets, application map/record caps, bounded metadata, validated grant persistence, bounded sync response batches, peer-store inventory/file limits, serialized persistence snapshots, and DHT limiter cleanup/caps. New entries are rejected at capacity. An attacker can still fill finite capacity and deny honest admission. |
| Medium | An observer replays a valid message under a new gossip ID or after reconnect; short dedupe TTL does not match message authenticity lifetime. | Signed IDs cannot be changed without the origin key. Freshness is five minutes with 30-second future allowance. Replay entries are origin-scoped, bounded and retained beyond that complete window; saturation fails closed. Decrypted ciphertext gets a separate key/nonce/ciphertext replay cache and channel-name AEAD associated data. State is volatile, so restart/unseen-recipient replay remains possible. |
| Medium | A stale mutable DHT record is locally rejected but `PublishRecord` ignores that result and propagates it; lookup can return a cache-rejected older value. | Propagate store errors; identical retained records may be republished, stale/conflicting records cannot. Lookup validates cached records and, on rejected insertion, returns validated retained data or an error. Record-store boundary now validates keys/signatures itself. No cross-replica newest-value or Byzantine quorum guarantee. |
| Medium | Expiry/name/signature metadata is unbounded relative to intended policy; disconnected identities accumulate in DHT rate maps. | Names capped at 256 bytes, signatures/public keys at their library sizes, nonzero expiry at most 30 days ahead; 64 KiB value cap retained. Rate state is released on disconnect, with a hard identity cap for standalone DHT callers. Immutable expiry/creation metadata is still outside the value hash. |
| Medium | Legacy grant canonicalization uses NUL separators but accepts NUL in variable IDs: `(a\0b,c)` and `(a,b\0c)` sign identical bytes. Altered grant IDs can bypass GrantID dedupe. Different grantors can also collide on a chosen ID. | Require generated canonical lowercase hex IDs (16-byte grant/quiz IDs, 32-byte public-key IDs) before legacy signature verification; namespace grant dedupe/persistence by grantor. A regression test demonstrates the old encoding collision and rejects both ambiguous inputs. No new cryptographic primitive was introduced. |
| Medium | An old session's delayed cleanup removes a replacement connection with the same ID after reconnect. | Remove-peer cleanup compares the actual peer instance under the node mutex. Cancelled peers stop their read loop; writer completion is joined. |
| Low | LAN reply floods and repeated persistence saves cause avoidable work; concurrent snapshot renames can replace newer state. | UDP collection stops after 256 datagrams / 64 candidates; each responder paces replies to at most one every 20 ms. Discovery attempts are sequential, max 16 per cycle. Saves serialize snapshot plus rename; inventories are capped. UDP source spoofing is not solved. |

## Identity and encryption lifecycle

1. **Creation:** `crypto/rand` generates Ed25519 and X25519 identities in memory.
   No private-key persistence was added. Channel keys/nonces use established
   XChaCha20-Poly1305/randomness APIs. The optional pin map is copied on node
   construction to avoid concurrent caller mutation; it contains public IDs only.
2. **Handshake:** Noise XX remains the established library's protocol, with both
   signing-key/static-key bindings checked. The five-second total setup timer also
   covers Hello writes/reads. Expected referred/pinned keys are checked before
   Hello admission. Hello must use the same application protocol version.
3. **Transport:** existing bounded frames, per-direction cipher locks, full writes,
   read tails and terminal authentication/I/O failures are retained. The additional
   envelope limit spans frames; a stalled/idle reader is closed by the message
   watchdog. All nodes must use `park-p2p/0.2.0` for the new gossip/channel semantics.
4. **Application:** Ed25519 signs a domain-separated fixed-field JSON gossip
   envelope. Verification binds the public key to its network ID and checks time,
   signature, replay namespace and capacity. AEAD associated data binds encrypted
   messages to channel names. An embedded plaintext `from` label is not the sender
   authority. Points/grants/records retain their independent signature checks.
5. **Disconnect/reconnect:** network cancellation closes pending/live streams;
   session admission and workers are tracked and joined. Old-instance cleanup
   cannot remove new sessions. Node-level gossip replay state survives a reconnect,
   but neither identity nor volatile replay state survives process restart.
6. **Malformed input:** document, metadata, collection, canonical-ID and cryptographic
   length checks reject input before state insertion. Fuzz seeds/runs exercise JSON,
   DHT record and AEAD nonce/ciphertext handling. This is not exhaustive fuzzing or
   independent validation of Noise, Ed25519 or the entire composed protocol.

## Default component budgets and cleanup

| Component | Bound / behavior |
| --- | --- |
| Pending + live + outbound-dial connections | 32 shared slots; configurable by trusted embedding code; release after session writer cleanup |
| Ciphertext frame / JSON envelope | 65,535 bytes / 128 KiB including newline |
| Setup / TCP dial or write / complete-message watchdog | 5 s / 5 s / 2 min, including idle time; quiet peers may disconnect and bootstrap again |
| Outbound queue / incoming application queue | 16 envelopes per peer / 128 envelopes |
| Untrusted message rate | 200/s per session with 4,000 burst; disconnect on excess; not a network-wide fair scheduler |
| Referral queue / worker / peer-list ingestion | 64 / one node worker / first 16 entries per message |
| LAN collection / reply pacing / manager attempts | 256 received datagrams, 64 unique candidates / <=50 replies/s per responder / <=16 sequential attempts per cycle |
| Gossip replay | 8,192 origin-scoped entries; five-minute signature lifetime + 30 s skew; fail closed at capacity |
| Decrypted channel replay | 4,096 fixed-size hashes, 24 hours; fail closed at capacity; no plaintext/key storage in entries |
| DHT record cache | 256 records and 8 MiB accounted data; 64 KiB value, 256-byte name; expired records reclaimed on admission |
| DHT lookups / pending RPCs | <=3 concurrent queries per lookup, K<=20, <=32 rounds; 2,048 global pending RPCs / four per peer |
| DHT limiter identities / persisted peer inventories | 1,024 / 1,024 entries each; peer-store reads capped at 1 MiB |
| Points identities / quiz opens / local quizzes / awards per local quiz | 2,048 / 1,024 / 128 / 1,024 |
| Grants / learned grant names / grant sync batch | 10,000 in ledger and persistent store / 2,048 / 128 |

These are component budgets, not measured hard RSS/disk ceilings. As an example,
32 peers each holding 16 distinct maximum envelopes can retain roughly 64 MiB of
payload; the incoming queue adds up to roughly 16 MiB, before object overhead,
read/decrypt/marshal buffers, maps, stacks, signatures, databases and Go GC. The
DHT 8 MiB accounting includes fixed allowance and value/key/signature/name lengths,
not precise allocator usage. A full store/replay cache rejects new content; it does
not evict live security evidence to satisfy a sender. This intentionally trades
availability for bounded state. Admission fairness and global byte/CPU scheduling
remain follow-up work.

`TestAdmissionAndShutdownResourceCleanup` asserts excess sockets are closed,
tracked slots/peers are released and goroutine count returns within a small margin.
It is a focused leak check, not a proof about every lifecycle or custom transport.
Alternative transports must make Close unblock pending I/O. The CLI's single
stdin scanner can remain blocked until input/process exit. LAN discovery has its
own bounded timeout and its manager exits on node cancellation; it is not part of
the node's worker WaitGroup. No heap profiler/load campaign established an RSS
ceiling, and no hostile Internet deployment was performed.

## Remaining exploitable risks

- **Medium to high availability risk on hostile networks:** Sybil peers can occupy
  all connection slots, fill permitted stores/replay caches, generate valid expensive
  signatures or consume bandwidth. Bounds limit growth; they cannot ensure service
  for honest users. IP/subnet admission fairness, operator authorization and global
  CPU/byte budgets are not implemented. Keep the demo on controlled networks.
- **Medium trust/discovery risk:** optional address pins do not establish initial
  trust, persist across restarts, apply to all inbound keys or authenticate names.
  Same-host referrals can probe other ports; approved CIDRs permit probes throughout
  that range. DNS in explicit unpinned bootstraps can select an unexpected endpoint.
  UDP source spoofing can influence permitted discovery hints. Use narrow scopes
  and independently obtained pins, not broad CIDRs or automatic first-use trust.
- **Medium replay/freshness risk:** a recipient with no replay entry can accept an
  unexpired signed message; restart loses volatile replay/ciphertext evidence.
  Clock skew affects acceptance. A key holder can deliberately issue/re-encrypt
  repeated content. Mutable sequences lose rollback protection after expiry,
  deletion or restart. DHT lookup may return the first valid retained/remote value,
  not the newest across replicas; no consensus/quorum protocol was added.
- **Medium application authority risk:** signatures authenticate key possession,
  not quiz fairness or entitlement to award scores. Any signer can issue valid
  grants within caps. The legacy seed relay is a routing convenience; its unsigned
  routing metadata is not a basis for application authorization. A malicious relay
  can withhold traffic. The UI is a demonstration, not a hardened terminal client;
  untrusted user text/labels can include display-control content.
- **Low to medium best-effort state limitations:** sync now returns at most 128
  grants and does not paginate through the entire historical backlog. Bounded
  caches can permanently reject new entries until expiry/manual lifecycle action.
  Persistence writes serialize within a store instance and use rename, but are
  not fsync-hardened for power loss or coordinated across multiple processes
  sharing one data directory. Existing valid grants are recognized without rewriting old storage.
  Unix socket discovery still lacks Windows portability. Node lifecycle is one
  Start followed by Stop; concurrent/repeated Start is not a supported API contract.

## Validation and reproducibility

Local environment: managed Linux x86_64 (Go reports linux/amd64), Go 1.27.3, CGO/race
support and Python 3. Real loopback TCP and isolated-environment UDP sockets were
used. The code was not tested on macOS/Windows, alternate transports or real WAN/NAT
networks. No independent audit, formal analysis, exhaustive fuzzing, dependency
security audit, long-duration load/heap profiling or secure-memory review occurred.

Executed checks (logs retained in the task workspace's `/tmp`):

- Full uncached `go test -count=1 ./...` and `go test -race -count=1 ./...`: passed.
- `go vet ./...`: passed.
- Relevant P2P concurrency/admission/replay/cleanup tests: twenty race runs passed after correcting discovery/manual-dial coalescing;
  DHT/Noise/channel/grant packages: twenty race runs passed.
- `P2P_LAN_UDP_TEST=1 go test -race -count=1 ./internal/discovery`: passed.
- Ten-second native fuzz runs, two workers each: envelope decoder **279,183**
  executions; DHT records **363,595**; channel decrypt **682,278**; all passed.
  Race builds exercise fuzz seeds; these mutation campaigns used ordinary builds.
- Three real localhost processes: signed Alice-to-Bob gossip and successful
  channel-name-bound encrypted delivery/decryption passed. Demo channel keys are
  suppressed from console/CI output; temporary state and processes are cleaned up.
- Resource tests cover byte/count quota boundaries, stale propagation rejection,
  ambiguous grant IDs, persistent grant replay/quota, pin mismatch/success,
  stalled readers, instance-safe reconnection cleanup and bounded decoder reads.

CI now also runs the ordinary uncached suite and five-second fuzz smoke runs.
The pre-Pass-2 GitHub Actions run
[41137190133](https://github.com/TylerPetri/p2p-networking-project/actions/runs/41137190133)
passed on the Pass 1 head. Final Pass 2 hosted evidence is recorded in
[PR #1](https://github.com/TylerPetri/p2p-networking-project/pull/1) after pushing;
local results alone do not establish GitHub Actions success.

## Portfolio / merge assessment

Recommend merging for an **experimental engineering portfolio** only after review
and green CI for the final head. This is a merge recommendation, not authorization
to perform a merge. There is still **NO-GO for production, sensitive communication,
unrestricted hostile-network service or independently audited-security claims**.
No license, visibility or repository metadata setting was changed in Pass 2.
The owner subsequently authorized the limited local evaluation/recruitment
permission in the current LICENSE.txt; the original implementation remains
proprietary, not open source.

Suggested GitHub description:

> Experimental Go P2P network with Noise XX, signed gossip, bounded peer lifecycle,
> Kademlia-style DHT, adversarial tests and a reproducible local demo.

Suggested topics: `golang`, `peer-to-peer`, `distributed-systems`, `noise-protocol`,
`cryptography`, `dht`, `kademlia`, `gossip-protocol`, `concurrency`, `networking`.
These are recommendations only; do not label the project as audited or production secure.
