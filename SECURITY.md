# Security policy and threat model

P2P-Park is experimental. Neither an independent protocol verification nor a
completed security audit is claimed. Use disposable data on a controlled local
network. The [Pass 1 review](docs/SECURITY_REVIEW.md) records the original baseline;
the [Pass 2 review](docs/PASS2_REVIEW.md) describes the budgets and the
[final review](docs/FINAL_REVIEW.md) records the prior portfolio merge/risk assessment. The
[engine extraction review](docs/ENGINE_REVIEW.md) covers the new API boundary.

## Assets and adversaries

Protect process availability, correspondence between connected peers and keys,
message integrity/confidentiality, replay state, DHT record integrity and local
storage. Treat TCP clients, authenticated peers, UDP advertisements, referrals,
relays, names, JSON and remote signed application data as potentially malicious.
An authenticated signer is not necessarily authorized, honest or a unique person.
Local host compromise and malicious local API callers are outside these controls.

## Implemented cryptography and trust

- Noise XX uses `github.com/flynn/noise`: X25519, ChaCha20-Poly1305 and BLAKE2s.
  Ordered per-direction AEAD nonces protect transport records. This is not X3DH.
- Ed25519 handshake signatures bind the signing key to the Noise static key in
  both directions. Hello IDs must match that static key. Optional outbound pins
  check an operator-supplied expected Noise key; discovered names/keys are never
  automatically promoted to trusted human identities. Park inbound admission remains
  open to arbitrary keys within the connection budget. The new engine facade
  can additionally restrict sessions and relayed gossip origins to a static
  map of approved Noise and Ed25519 key pairs.
- Gossip carries an Ed25519 origin signature over a domain-separated fixed-field
  JSON envelope, signing-key/static-key binding and timestamp. Relays preserve the
  signature. A relayed origin's signature proves its Ed25519 key possession;
  signing a public Noise key does not independently prove possession of that
  Noise private key. Contradictions of currently connected peer bindings are
  rejected, but unknown open-network transport-ID claims require separate trust.
  Applications must authorize by trusted signing keys, not claimed Noise IDs.
  Direct control-message origins come from the authenticated session.
  NAT relay routing metadata remains the seed's claim; opaque payloads need their
  own authentication before any future authorization use.
- Shared channels use XChaCha20-Poly1305 with random 24-byte nonces and the channel
  name as associated data. The gossip signer, not a plaintext `from` label,
  supplies the displayed origin. Key holders can still produce valid ciphertext.
- Immutable DHT values use SHA-256 content addressing. Mutable records use
  Ed25519 signatures over key, sequence, expiry and value; the key derives from
  signing key and name. Immutable expiry/creation metadata is not hash-bound.
  Quiz grants and points use Ed25519 signatures; bounded canonical grant IDs
  exclude ambiguous delimiter encodings. Signatures do not establish award fairness.

Park keys are generated on node startup and retained in process memory. The
engine optionally persists Ed25519 and Noise identity keys in a permission-checked
plaintext file. It does not encrypt keys at rest, defend against local host
compromise, or promise secure erasure. No automatic key rotation, PKI, trusted
name directory or durable pin database is implemented. Obtain expected keys out of band and update pins
after restarting ephemeral identities. There is no secure-memory erasure claim.

## Resource, discovery and replay boundaries

Frames, JSON envelopes, connection admission, outbound queues, DHT records,
lookup concurrency, discovery candidates, replay caches and principal application
maps have explicit component limits listed in the Pass 2 review. Setup is bounded
by five seconds; each complete message (including idle time) has a two-minute
watchdog. TCP dialing and writes have five-second bounds. Stop cancels network
I/O and joins tracked node workers. Alternative transports must unblock on Close.

Automatic discovery does not resolve DNS. It accepts only approved numeric CIDRs,
connected peers' own hosts, or loopback when bound to loopback, and checks advertised
peer IDs after Noise. Explicit operator bootstraps may resolve DNS; use pins when
identity matters. UDP source-IP/port binding, candidate limits and response pacing
reduce abuse but do not authenticate LAN advertisements or stop spoofed UDP.

Signed gossip has a five-minute lifetime, 30-second future clock allowance, and a
bounded origin-scoped replay cache. Capacity saturation drops new entries instead
of evicting live replay evidence. Decrypted channel ciphertexts have a separate
24-hour bounded replay cache. These caches do not survive restart, and a recipient
that has not previously seen a valid in-window message can accept it. Re-encryption
or deliberate new messages from an authorized signer are not identifiable replays.
An observer can rewrap copied channel ciphertext in its own signed gossip; a
recipient without a prior ciphertext-cache entry may accept it. The displayed
gossip signer is not proof of who originally encrypted that ciphertext.
Immutable and mutable derivations share a key namespace. Stored record types
cannot change while retained, so a type switch cannot bypass a retained sequence
check. This does not stop first-writer preemption: `GetValue` is untyped, and callers
must check the record type they intended. A future typed/key-namespace change is
outside the current protocol. Mutable rollback checks last only while retained;
grant deduplication is
persistent but total storage is capped.

Finite budgets do not prevent denial of service: Sybil peers can occupy slots,
fill stores/caches, consume CPU or block honest discovery. Broad CIDR approval
allows port probes within that range. There is no Byzantine DHT quorum, newest-value
guarantee, reliable delivery, Internet-scale DoS protection, traffic-analysis
resistance, or independently verified security guarantee. Grant sync is a bounded
best-effort batch, not a complete history reconciliation protocol. Do not use this
project for sensitive communication or expose it as a trusted public service.

## Engine extraction boundary

`engine` uses `p2p-engine/0.1.0`, separate from the unchanged Park `0.2.0` wire
protocol. It checks a public overlay NetworkID in Hello, exchanges admission
acknowledgements, and signs the overlay and direct recipient into gossip.
Signatures cannot be moved between overlay IDs or changed from direct messages
into broadcasts. Empty optional fields preserve legacy Park signed bytes.
Mismatched versions/domains fail before peer admission; no bridge or negotiation
is supplied. Same-revision nodes are required.

Open admission is still open to Sybil identities. Restricted admission checks a
static local map pinning both Noise and Ed25519 identities, including relayed
gossip origins, but does not provide
revocation distribution, key rotation, human identity or business authorization.
Use separately obtained pins/paired-key approvals. The engine exposes
`Message.ClaimedPeerID` separately from its authenticated `SigningKey`. A network ID is not a secret. Application
labels filter public API delivery; relays can read gossip and applications still
must validate content/authority. Admission does not certify DHT record authors.
DHT freshness/type/namespace limitations and volatile replay state remain.

Applications cannot send raw core control envelopes through the public API.
Messages retain existing signature, replay, frame, envelope and connection limits,
plus a 64 KiB application-data cap. Bounded output streams may drop data/events;
there is no reliable delivery. Close joins tracked work; cancellation/repeated
lifecycle tests exercise cleanup, not a proof of no leaks on every schedule.
Default engine configuration enables no LAN broadcaster or implicit disk peer
store. The game keeps its existing adapter, protocol and optional UDP discovery.
See the [API lifecycle and limits](engine/README.md) and
[extraction decisions](docs/ENGINE_ARCHITECTURE.md).

## Reporting vulnerabilities

Do not put secrets or exploitable details in public issues. If enabled, use
[GitHub private vulnerability reporting](https://github.com/TylerPetri/p2p-networking-project/security/advisories/new).
Otherwise arrange a private channel through the maintainer's
[GitHub profile](https://github.com/TylerPetri) before sending details. No monitored
security mailbox, response SLA or supported release series is established here.
The development branch receives fixes on a best-effort basis.

If credentials ever appear in history, revoke/rotate them first. Removing them
from the current tree does not remove history, forks, caches or clones. History
rewrites and visibility changes are separate maintainer decisions.
