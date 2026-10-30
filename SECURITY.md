# Security policy and status

P2P-Park is experimental. No independent protocol verification or completed
security audit is claimed. Use disposable data and keys on an isolated local
network; do not rely on it for sensitive communication or expose it to hostile
Internet traffic. The [engineering security review](docs/SECURITY_REVIEW.md)
records the threat model, verified findings, scope and validation results.

## Implemented cryptography

- Noise XX through `github.com/flynn/noise`, using X25519, ChaCha20-Poly1305 and
  BLAKE2s, with ordered per-direction transport nonces. This is not X3DH and does
  not use XChaCha20-Poly1305 for the transport.
- Hello network IDs must equal the remote Noise static public key. Ed25519
  handshake signatures bind signing keys to that key in both directions.
  Names are untrusted labels. There is no PKI, key pinning, persistent identity,
  trusted name directory, or authorization policy. An attacker can substitute
  its own newly discovered identity; verify expected identities out of band.
- Shared-key channels use XChaCha20-Poly1305 with random 24-byte nonces. Any holder
  of a channel key can create valid messages; channel ciphertexts have no durable
  application replay ledger or authenticated channel-name associated data.
- Immutable DHT values use SHA-256 content addressing. Mutable records use
  Ed25519 signatures over key, sequence, expiry and value. Not all distributed
  data is signed; immutable expiry/creation metadata is not hash-bound.

A working cryptographic primitive is not independent verification of its use in
this protocol. Noise record replay/tampering is rejected within a connection;
application gossip deduplication expires and does not prevent replay across
sessions. Mutable sequence protection only lasts while the stored record exists.

## Operational boundaries

Frames are bounded before allocation; read tails are retained; short writes are
completed or fail closed; cipher states are serialized per direction. Peer setup
has a five-second timeout, TCP dialing and each TCP write have five-second bounds,
and node cancellation closes pending and established sessions. The Noise wrapper
itself accepts an `io.ReadWriteCloser`: callers using it directly must arrange
cancellation/deadlines and close on handshake failure. There is no steady-state
idle/read deadline, total JSON envelope cap, global connection budget, or total
DHT storage quota. A peer can exhaust resources despite bounded frames.

Discovery referrals, addresses, DHT routing membership, gossip `FromID` and chat
sender claims are untrusted. LAN advertisements are restricted to the UDP source
IP plus a validated port, but UDP spoofing is still possible. Peer-list discovery
can dial attacker-selected destinations reachable from the process. Sybil/eclipse
attacks, malicious relays, traffic analysis, local host compromise, durable replay
protection, authorization and Internet-scale denial of service remain outside the
implemented guarantees. Do not interpret rate limits or subnet diversity checks
as comprehensive defenses.

## Reporting vulnerabilities

Do not disclose secrets or exploitable details in a public issue. If enabled, use
[GitHub private vulnerability reporting](https://github.com/TylerPetri/p2p-networking-project/security/advisories/new).
If unavailable, arrange a private channel with the maintainer through their
[GitHub profile](https://github.com/TylerPetri) before sending details. No monitored
security mailbox, response SLA or supported release series is established here.
Only the current development branch receives fixes on a best-effort basis.

If a credential ever appears in Git history, revoke/rotate it first. Removing it
from the current tree does not remove it from history, forks, caches or clones.
History rewrites and visibility changes require explicit maintainer decisions.
