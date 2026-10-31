# Experimental networking engine API

Import `p2p-park/engine` from the existing Go module. Start with
[`examples/echo`](../examples/echo/main.go): `go run ./examples/echo` creates three
loopback nodes, persists disposable identities, connects them and verifies a
signed message exchanged between peers. It closes all nodes and removes its
state. No game packages, server, LAN broadcast or public bootstrap are required.

For an external local consumer, use `require p2p-park v0.0.0` and
`replace p2p-park => /absolute/path/to/checkout` in its go.mod. No separate module
is published. This is an experimental API without a stable-version promise.
Technical reuse does not expand the repository's proprietary evaluation license.

## Configuration and creation

`New(Config)` validates configuration, copies identity/trust inputs and allocates
a node; it does not listen. Check its error and call `Close` even if not started.
Obtain `NetworkID` once with `NewNetworkID` and distribute it with bootstrap
addresses. It is a public overlay identifier, not a password or admission token.
All nodes must use the same network ID and protocol `p2p-engine/0.1.0`.

| Config field | Meaning / default |
| --- | --- |
| `NetworkID` | Required 64 lowercase hex characters (32 random bytes recommended). |
| `Application` | Required 1–64 ASCII letters, digits, `.`, `_`, `-`; message namespace. |
| `Identity` | Optional opaque persistent identity; nil generates ephemeral keys. |
| `Bind` | TCP listen address; defaults to `127.0.0.1:0`. |
| `Name` | Untrusted display label, at most 256 bytes. |
| `Bootstraps` | Operator-selected TCP addresses; asynchronously attempted after startup. `Start` is not bootstrap-success acknowledgement. |
| `PeerPins` | Address to expected Noise PeerID; obtain through a trusted channel. |
| `DiscoveryCIDRs` | Additional numeric referral scopes; defaults remain same-host / loopback policy. Not a firewall on explicit `Connect`. |
| `Admission` | `Open` (default) or `Restricted`. |
| `ApprovedPeers` | Map of Noise PeerID to hex Ed25519 SigningID. Both keys must be approved; empty restricted map denies all remote peers. Each endpoint applies its own policy. |
| `MaxConnections` | Pending + established session budget; nonpositive defaults to 32. |
| `MessageTimeout` | Deadline for a complete incoming envelope, including idle wait; nonpositive defaults to two minutes. |
| `PeerStorePath` | Optional routing-peer persistence; empty disables disk peer store. Never share this path across nodes/overlays. |

`GenerateIdentity` creates keys; `LoadOrCreateIdentity(path)` atomically creates a
private JSON file or validates and loads it. A corrupt/permissive file is an error,
not a request to silently change identity. Concurrent creation converges on one
complete file. Requires a local filesystem supporting hard links; existing files
must be regular with no group/other permission bits. Keep parent paths trusted.
Keys are plaintext, not encrypted or securely erased; directory durability across
power loss and malicious local filesystem changes are not guaranteed. Back up
privately; loss changes identity. Never commit this file. `PeerID`, `SigningID` and
`SigningPublicKey` expose only public identity; returned public-key bytes are copied.

For restricted admission, exchange both public IDs over a trusted channel:

```go
cfg.Admission = engine.Restricted
cfg.ApprovedPeers = map[string]string{
    approvedIdentity.PeerID(): approvedIdentity.SigningID(),
}
```

A signature over a public Noise key alone is not transferable proof of possessing
that Noise private key. Noise XX proves it on direct sessions; restricted relay
trust requires the separately approved signing key. Do not learn approvals from
untrusted gossip. `Engine.SigningID()` identifies its own signing key.

## Lifecycle and concurrency

`Start(ctx)` listens and launches tracked networking workers. Its context owns the
whole lifetime; cancellation calls `Close`. A pre-cancelled context leaves the
instance retryable. A listen/start failure closes the instance. Start once; after
closure create a new Engine, reusing the Identity if continuity is desired.
`Close` is safe concurrently and repeatedly, closes sockets, cancels/joins tracked
workers, and closes `Messages`, `Events` and `Done`. Drain buffered messages before
assuming a closed channel is empty. `Done` closes after cleanup. A zero-value
Engine is not usable: always use `New`.

Public engine operations can run concurrently after construction. Do not mutate
configuration while `New` copies it. Each channel is a single stream: multiple
readers compete, rather than subscribe independently. `Peers` is a snapshot,
not an authoritative membership list; event loss/disconnection can race any call.
Provide non-nil contexts. Operations can return underlying filesystem, TCP,
handshake and DHT errors; use `errors.Is` for exported sentinel/context errors.
No callbacks are invoked by the API.

## Connections and messages

- `Connect(ctx, address)` waits for Noise, identity, version, overlay and both
  admission checks. Cancellation aborts a stalled attempt. The context bounds
  only the attempt: a successful session belongs to the Start context. Existing
  matching connections succeed; simultaneous duplicate attempts may be rejected.
  Failure may release its budget asynchronously as its session exits.
- `Peers`, `PeerID`, `Address` inspect local state. `Disconnect(PeerID)` removes
  the currently connected instance; it does not permanently ban automatic
  rediscovery/reconnection. `Events` reports bounded, best-effort `EventPeerConnected` /
  `EventPeerDisconnected` events; query `Peers` to resynchronize.
- `Send(ctx, peer, topic, data)` requires a direct connection. It signs the
  recipient into the envelope and never broadcasts that message. `Publish`
  sends signed gossip to current peers for best-effort relay. Neither confirms
  remote delivery, retries, persists, or echoes locally. Publish can partially
  fan out before returning an error. Empty networks can publish successfully.
- Topics are 1–64 bytes; data is at most `MaxMessageSize` (64 KiB). Payloads are
  opaque bytes copied during encoding, not protocol/control-message entry points.
  Outbound messages retain frame/envelope caps, signature/binding, network domain,
  origin admission, rate limiting and replay checks. Receiving applications get
  only verified gossip in their application namespace via `Messages`.
- `Message.SigningKey` is the authenticated Ed25519 origin. `ClaimedPeerID` is
  the signed Noise-ID claim, not standalone proof of its private-key possession
  for an unknown relayed origin. Restricted approvals pin the pair; messages
  contradicting a currently connected peer's signing identity are rejected.
  Authorize by the trusted signing key, not an unverified transport-ID claim.
  `Data` / `SigningKey` belong to the consumer. Validate data, authority and
  idempotency separately. Topics cannot invoke core control handlers.
- Message/event streams each buffer 128 entries and drop on saturation; internal
  incoming queues are also bounded. Per-peer output queues hold 16 envelopes;
  saturation disconnects the slow peer and returns an error to direct callers.
  Replay-cache saturation fails closed. Default connection/time limits remain
  unchanged. There is no reliable-delivery or hard process-memory guarantee.

Errors include `ErrNotStarted`, `ErrAlreadyStarted`, `ErrClosed`,
`ErrInvalidMessage`, `ErrPeerClosed`, `ErrResourceLimit` and context errors.
Local admission may return `ErrAdmissionDenied`; remote rejection often appears
as EOF/handshake failure without disclosure of its policy. `Send`/`Publish`
contexts are checked before bounded synchronous encoding/enqueue; they do not
cancel an already queued message.

## DHT and trust boundaries

`PutImmutable(ctx, value, ttl)` derives a content key. `PutMutable(ctx, name,
value, seq, ttl)` signs with the engine identity and prefixes the name with
`Application + "/"`; preserve monotonic sequence numbers yourself across restarts.
`Get(ctx, Key)` returns `(Record, found, error)` with copied byte fields. The
underlying record-size, expiry, signature, sequence and propagation limits apply;
TTL does not guarantee retention or publication to other peers. Publication
can update the local cache before cancellation or a propagation error; an error
does not roll back local state. Check `Record.Type`,
signer/name and your own authority rules. Get is untyped; namespaces are shared.
DHT record authors are not necessarily current approved members: admission
controls sessions and gossip origins, not a separate record-author certificate.
No quorum, freshness, durable replay state or application consensus is provided.

Application labels isolate API delivery, not confidentiality or hostile apps in
the same process. Gossip relays can read payloads after transport decryption.
Restricted membership is a static local allowlist, not enterprise authorization,
revocation, human identity or Sybil resistance. Network IDs and recipient fields
are signed, but open members can generate arbitrary new valid messages. Use
application encryption/authorization where needed, with a separate design review.
Park's shared-key channel UI remains in the game, outside this initial API.

See [architecture and extraction decisions](../docs/ENGINE_ARCHITECTURE.md) and
[SECURITY.md](../SECURITY.md). This protocol is experimental and has not been
independently security-audited.
