# Phase 1 engine extraction review

Historical Phase 1 record. See [Phase 2 migration](PHASE2_ENGINE_ADAPTER.md) for
the current Park consumer boundary and API additions.


## Scope and outcome

Based on merged main `b6cfb37`, retaining its security hardening and proprietary
license/notices. The public `engine` facade is a reusable experimental networking
API in the same module; no second repository/module was published. Generic wire
schemas moved out of game schemas; the old package retains aliases. The existing
Park game/CLI still uses the same core through its legacy adapter. Migrating that
adapter is explicitly Phase 2, not a completed separation of every application.
No game features, custom cryptography or independent security certification added.

Read [architecture/options](ENGINE_ARCHITECTURE.md),
[measured dependency map](PACKAGE_DEPENDENCIES.md), and
[API/configuration/lifecycle](../engine/README.md).

## Correctness and security changes

- Opaque, validated/copy-owned persistent identities; atomic private-file creation
  avoids partial-file observation and silent identity regeneration.
- Public overlay ID checked in Hello, signed gossip domain and recipient binding,
  plus mutual admission acknowledgement under `p2p-engine/0.1.0`. Static local
  restricted policy pins both Noise and Ed25519 IDs for sessions and relayed origins. Park stays `0.2.0`;
  byte-compatibility tests verify omitted engine fields preserve signed bytes.
- Public messages cannot select control-envelope types, sign another origin or
  skip framing/signatures/replay/admission/queue limits. Application topics are
  data, not core control commands. Direct messages are not broadcast.
- Contextual Connect waits for admission, bounds stalled setup, and retains good
  sessions after attempt context completion. Existing connection budgets cover
  these attempts. Send reports queue failures instead of hiding them.
- API DHT calls carry cancellation into the existing RPC sender path and register
  with engine lifecycle cleanup. Stored-peer bootstrap dials now run in a tracked
  worker so slow stored destinations do not block listening/startup.
- Bounded receive/event streams, snapshot access and idempotent concurrent Close;
  persistent keys, application state and replay durability remain separate concerns.

## Adversarial finding corrected before handoff

**High for a transport-ID-only restricted policy:** an approved relay could sign
another peer's public Noise ID using the relay's Ed25519 key, then claim that
approved Noise origin in gossip. Ed25519 verification and the signed binding would
succeed without evidence of possessing the claimed Noise private key. Restricted
approvals now pin both Noise and Ed25519 public identities. A regression test
constructs a mathematically valid forgery and confirms it is denied. Known direct
peer/signing-key contradictions are also rejected by the shared core.

Unknown relayed origins in open networks still authenticate their actual signing
key, not ownership of an arbitrary signed transport-ID claim. The public message
field is deliberately named `ClaimedPeerID`; authorization must use a separately
trusted `SigningKey`. This is explicit in the API and threat model. The example
verifies the expected signing key, rather than trusting the Noise-ID label alone.

## Verification

Local environment: managed Linux 6.18.57 x86_64, Go 1.27.3 linux/amd64,
Python 3.12.14; loopback TCP, isolated temporary state and optional real UDP tests.
The existing supported-platform caveats remain; no Windows, WAN/NAT traversal,
private business deployment or production-load campaign was tested.

Commands exercised:

```sh
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go test -race -count=10 ./engine
go test -race -count=20 ./internal/crypto/noiseconn ./internal/dht ./internal/crypto/channel
go test -race -count=10 -run 'TestSession|TestIdentify|TestStop|TestGossip|TestNAT|TestEncrypted|TestDHT|TestSigned|TestAdmission|TestSlow|TestOldSession|TestCorrectOutboundPin|TestEngine' ./internal/p2p
P2P_LAN_UDP_TEST=1 go test -race -count=1 ./internal/discovery
go test ./internal/p2p -run '^$' -fuzz '^FuzzEnvelopeDecoder$' -fuzztime=10s -parallel=2
go test ./internal/dht -run '^$' -fuzz '^FuzzRecordValidation$' -fuzztime=10s -parallel=2
go test ./internal/crypto/channel -run '^$' -fuzz '^FuzzChannelDecrypt$' -fuzztime=10s -parallel=2
python3 scripts/check_engine_boundary.py
go run ./examples/echo
python3 scripts/demo.py
gitleaks git --log-opts='--all --full-history' --redact .
gitleaks dir --redact .
```

Tests cover existing adversarial framing/identity/replay/DHT/channel cases plus
new engine repeated lifecycle/cancellation, concurrent Close, released listeners,
stalled handshake cancellation and connection-budget reuse, failed listening/dials,
concurrent peer traffic, direct-recipient isolation, application namespaces,
network-domain and relayed-origin admission (including validly signed transport-ID
impersonation), identity recreation/reconnection,
concurrent private-file creation, corrupt/permissive-file refusal, DHT access and
pending-RPC cancellation. A real Park app rejects an engine peer without admitting
it. Existing game tests and signed/encrypted three-process demo remain required.
Repeated testing also exposed shared routing-store interference in the existing
connection-capacity fixture. That fixture now disables persistence and asserts
that its two pending inbound clients occupy the budget before checking rejection;
the production limit was unchanged.
An external temporary consumer with a local go.mod replace successfully imported,
built and started the public engine without importing internal packages.

CI retains vet/full/race/repeated/fuzz/game-demo/history scanning and adds repeated
engine race tests, standalone demo and a production dependency-boundary check.
The PR records hosted run evidence for its final head. Fuzz runs are short smoke
coverage, not exhaustive campaigns; race and cleanup tests exercise selected
schedules, not proofs. Component budgets remain finite, not a hard RSS ceiling.

## Remaining limitations and Phase 2

No newly identified release-blocking issue remains for controlled experimental
use after the documented checks. These are consequential limitations:

| Risk | Severity / scope |
| --- | --- |
| Sybil/capacity exhaustion, allowed-range port probing | High if exposed as a trusted public service; controlled local demonstration only. Static restricted lists reduce entry but approved peers may still abuse capacity. |
| Initial identity trust and application authority | High for sensitive use. Pins/paired-key approvals require separate trusted distribution; unknown relayed Noise-ID claims do not prove transport-key possession; names, network IDs, topics and valid signatures do not establish human/business authority. |
| Volatile replay and DHT freshness/type/author trust | Medium. Restart/cache loss weakens deduplication/rollback protection; record validation is not newest-value consensus or membership certification. Applications must check record type/signers and persist their own sequence/idempotency state. |
| Plaintext persistent private keys | Medium on multiuser/shared hosts. Permission checks rely on trusted local paths; no encrypted keystore, rotation or secure-erasure promise. |
| Best-effort streams, queues and gossip | Medium for apps requiring reliability. Saturation drops notifications/messages or disconnects peers; no acknowledgement, durable queue or delivery guarantee. |
| Relays can read application payloads; untrusted display/content | Medium for sensitive data. Transport encryption is per connection. Labels are not payload secrecy or an authorization sandbox; sanitize terminal/UI output. |
| API/version and legacy adapter migration | Low operational limitation for a same-revision local demo. Engine/Park protocols intentionally differ; no negotiation or stable public-module compatibility promise. |

Phase 2 should migrate Park's signing/grant-sync integration onto a deliberate
application adapter consuming the facade, while keeping game schemas/storage out
of the engine. Evaluate optional LAN discovery and typed DHT access separately.
Dynamic membership/revocation, persistent replay, key rotation and application
payload encryption need explicit protocol/threat-model decisions, not speculative
enterprise features in this extraction. Existing reserved-rights licensing is
unchanged and does not automatically authorize third-party business deployment.
