# Phase 3 API stabilization and invitation review

## Repository baseline

Main `1c1d331` contains merged Phase 1. Phase 2 PR #3 was merged into
`engine/extraction-phase1` at `01e7117`, rather than main. This new branch starts
at that actual merged Phase 2 head and targets that branch to keep the invitation
diff focused. No merge, deployment, history rewrite, license change, module
publication or separate repository was performed. Main integration/retargeting
remains a maintainer decision.

Reviewed main plus the merged engine/adapter, ENGINE_ARCHITECTURE,
PACKAGE_DEPENDENCIES, engine/README, ENGINE_REVIEW, Phase 2 and security documents.
Existing boundaries and experimental classification remain applicable.

## API findings and choices before implementation

1. Park exposed a need to sign transferable application records without exporting
   private keys; Phase 2's standard crypto.Signer already covers that need.
   The echo consumer confirms applications need only public engine types.
2. Manual bootstrap addresses/pins are split across configuration fields, do not
   convey an overlay, and cannot be safely imported by mutating a running node's
   trust maps. A bounded invitation value and per-attempt paired-key dialing
   address this concrete gap without breaking manual configuration.
3. A successful TCP dial is insufficient. Join waits for both pinned identities,
   protocol/domain checks and admission. A pre-existing session must match both
   keys; duplicate-attempt reuse also checks the authenticated pair.
4. Join needs partial-failure reporting and a clear lifetime: try all bounded
   alternatives, return explicit Connected/Failures, keep successful sessions under
   Start, register Join for Close and preserve context cancellation semantics.
5. Application namespace/authorization and network identity/membership remain
   separate. Invitations carry no app roles, names, shared password, certificate
   authority, business metadata or implicit membership approvals.

Options considered: extend Config with opaque invitation strings and start hidden
retry workers; expose raw Node/pin mutation; or add explicit parse/generate/join
operations. **Chosen:** explicit public values and operations, no hidden retries.
Only the required internal per-call pin hook changes handshake integration.

For authenticity, a self-signed issuer field would not identify a trusted source
without a separate anchor and introduces issuer policy not needed for public open
bootstrapping. **Chosen:** unsigned public discovery data, direct endpoint key
proof and honest trusted-source requirements. This is not a signed invitation or
bearer token. An issuer-authenticated future format needs explicit trusted-anchor
semantics and a new version. No custom cryptographic primitive was invented.

For endpoints, DNS creates resolution/rebinding and reachability-policy ambiguity.
**Chosen for v1:** conservative canonical numeric addresses, no DNS, local approval
for private/loopback/CGNAT, non-overridable special-use exclusions. This trades
convenience/dynamic addressing for predictable policy. Global address scope does
not itself imply trust or Internet reachability.

For attempts, four parallel workers could reduce worst-case latency but complicate
partial completion and cancellation. **Chosen:** bounded sequential attempts with
five-second per-attempt caps and caller/expiry deadlines. No extra Join workers.
The global core connection budget still limits simultaneous calls.

## Changes and stability assessment

Added public Invitation, BootstrapPeer, JoinOptions, JoinResult, ParseInvitation,
Invitation.Encode, Engine.Invitation and Engine.Join, bounded format constants
and typed invitation/bootstrap errors. See [the specification](INVITATIONS.md).
No existing public API signature, core wire version or Park schema changes.
Manual bootstrap/Connect, optional LAN discovery, opaque signer identity, DHT,
admission, application messages and lifecycle continue through the same core.

The core now supports per-attempt Noise and Ed25519 pins without modifying its
configuration. Pin checks happen before admission. Closed/duplicate attempts
cannot bypass a signing pin; matching authenticated duplicate sessions can be
reused. The existing operation-registration helper now tracks Join as well as
DHT work. Existing context-cancellation cleanup remains intact.

The API is understandable for the two actual consumers and the new invitation
flow, but still experimental, not a stable published Go module. Before publishing:
choose a module/import path and compatibility policy; test declared minimum Go
and supported platform matrices; decide whether the public DTOs and canonical
v1 format require separate long-term version support; assess typed DHT access,
error consistency, address-specific pin aliases and drop/event observability with
real consumers. Do not add speculative interfaces or promise breaking-change-free
updates today. A future extraction can move engine and its generic internal
closure together; current dependency checks guard game isolation.

## Verification

New tests exercise canonical/strict parsing, version/extension rejection, unsafe
endpoints and explicit local policy, full-list validation before dialing,
configured pin conflicts, key substitution before admission (including reused
connections), two/three-node joining, multiple bootstraps, dead-bootstrap fallback,
continued messages and reconnection after bootstrap loss, concurrent joins,
restricted-policy denial, network mismatch, malicious authenticated referrals,
expiry during setup, cancellation, Close and released listeners. The parser fuzz
target performs no network I/O and requires exact successful round trips.

An external temporary Go consumer with a local module replacement also built,
started two nodes and joined through the public invitation API without internal
imports. No module was published or added to the repository.

The invitation-based echo demonstration uses public API types only. It preserves
temporary persistent identities, removes its files and closes all nodes. Park's
existing three-process signed/encrypted demonstration and integration tests remain
unchanged. Both dependency boundary checks still apply.

Required/reproduced commands:

```sh
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go test -race -count=10 ./engine ./internal/park-node
go test -race -count=20 ./internal/crypto/noiseconn ./internal/dht ./internal/crypto/channel
go test -race -count=10 -run 'TestSession|TestIdentify|TestStop|TestGossip|TestNAT|TestEncrypted|TestDHT|TestSigned|TestAdmission|TestSlow|TestOldSession|TestCorrectOutboundPin|TestEngine|TestRelayedTransport' ./internal/p2p
GOMAXPROCS=1 go test -race -count=50 -run '^TestStalledConnectCancellationReleasesBudget$' ./engine
P2P_LAN_UDP_TEST=1 go test -race -count=1 ./internal/discovery
go test ./engine -run '^$' -fuzz '^FuzzInvitationParse$' -fuzztime=10s -parallel=2
go test ./internal/p2p -run '^$' -fuzz '^FuzzEnvelopeDecoder$' -fuzztime=10s -parallel=2
go test ./internal/dht -run '^$' -fuzz '^FuzzRecordValidation$' -fuzztime=10s -parallel=2
go test ./internal/crypto/channel -run '^$' -fuzz '^FuzzChannelDecrypt$' -fuzztime=10s -parallel=2
python3 scripts/check_engine_boundary.py
go run ./examples/echo
python3 scripts/demo.py
gitleaks git --log-opts='--all --full-history' --redact .
gitleaks dir --redact .
```

Environment: managed Linux x86_64, Go 1.27.3, Python 3.12.14; loopback TCP and
isolated real UDP discovery. Hosted CI/run evidence is recorded in the PR. These
are exercised schedules and short fuzz smoke runs, not formal verification.
Windows, cross-host LAN/WAN, real NAT/firewall topologies, public endpoint
reachability and production loads were not tested; the environment has no general
outbound TCP grants. No successful localhost result establishes Internet-wide
connectivity or supports sensitive production communication.

## Remaining security risks and Phase 4

| Risk | Severity / scope |
| --- | --- |
| Replaced/untrusted invitation source, malicious pinned bootstrap, Sybil/eclipse and capacity exhaustion | High for exposed/sensitive service use. Endpoint proof is key possession, not honest community membership or issuer trust. |
| Approved-range/public port probes and malicious referrals | Medium. Numeric/scoped dialing, pins, four-record cap and deadlines bound some abuse; importing data is not universally safe SSRF filtering. |
| Volatile replay, DHT freshness/type/record-author trust, plaintext identity files | Medium; unchanged. No invitation credential, durable replay or automatic rotation introduced. |
| Best-effort streams/sync, stale endpoints, bootstrap availability | Medium for reliable applications. Partial Join, bounded queues, expiry and reconnection are explicit, not delivery guarantees or NAT traversal. |
| Terminal output and application authority/fairness | Medium for sensitive applications; remain application concerns. |
| API/format publication compatibility | Low for controlled same-revision evaluation; no published module/API stability promise yet. |

Phase 4 recommendation: exercise a small independently built consumer and
invitation interoperability fixtures, stabilize API/error/version policy, and
measure reachability requirements on controlled cross-host networks before choosing
any NAT/relay strategy. A future issuer-authenticated invitation format, durable
replay or dynamic restricted membership each needs its own scoped threat model.
Do not bundle new game features, enterprise roles, a website or module publication
into these conclusions. Experimental portfolio/prototype use only; no independent
security audit or production readiness is claimed.
