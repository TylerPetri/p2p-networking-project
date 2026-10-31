# Phase 4 review and packaging handoff

## Outcome

This branch adds compatibility evidence and owner-run verification tooling; it
adds no game features, enterprise authorization, public relay or engine API.
The engine remains experimental, not independently audited or production-ready.

- [Exact branch/PR ancestry and safe owner integration sequence](PHASE4_INTEGRATION.md)
- [API/error/lifecycle findings and version policy](API_VERSION_POLICY.md)
- [Deterministic public interoperability corpus](../engine/testdata/invitations/README.md)
- [Reproducible owner cross-host matrix](CROSS_HOST_TEST_PLAN.md)
- [Results template: external rows intentionally NOT RUN](CROSS_HOST_RESULTS_TEMPLATE.md)

The current Phase 4 PR stacks on PR #4's head `d58f403`; PR #4 is still open.
Phase 2 was merged into its topic branch, not main. The integration assessment
contains the exact merge bases and owner sequence. No merge/retarget of existing
PRs, deployment, module publication, licensing change or history rewrite occurred.

## Compatibility work

The committed 21-vector corpus uses fixed public identities, JSON and URI bytes,
expected error categories and a reference clock. Go and independent Python codec
checks agree. Live Go scenarios substitute only ephemeral endpoints and verify
actual Noise/signing identity failures and overlay/scope isolation. Private clock
helpers enable deterministic expiry fixtures; production API and real-clock expiry
checks are unchanged. No caller can use the public API to disable expiry.

The external-consumer script creates a disposable Go module with local replace,
builds/runs under the race detector and tests invitation policy, authenticated
messages, stable sentinels and lifecycle without internal imports. It publishes
nothing and adds no nested module. Both existing application boundaries remain
checked in CI; the new netcheck diagnostic also has no game dependencies.

No new release-blocking API defect was found. Context cancellation and tracked
shutdown are exercised; a successful attempt's cancellation does not close its
accepted session, and a failed signing-pin check does not poison the existing
session/trust. Event/receive queues remain lossy/bounded; remote readiness and
continued liveness are not implied by admission. Existing error specificity,
address-specific pin aliases, Unix support and untyped DHT limitations are
explicit pre-publication decisions, not reasons to add speculative interfaces.

## Diagnostic evidence and limits

`examples/netcheck` is a small public-API consumer for the owner matrix. Non-loopback
binding requires a deliberate flag; wildcard binds are rejected. State/identity
are private files; public invitation files can be copied through trusted channels.
Expected-signer/nonce round trips distinguish enqueue from verified delivery.
The process lifetime, pending probes and input/payload sizes are bounded.

`python3 scripts/check_netcheck.py` builds it with the race detector and verifies
separate **loopback processes**, expected-signer denial, stale-bootstrap failure,
restart/fallback with the same keys/network, and process cleanup. This proves
neither cross-host networking, real firewall filtering nor partition recovery.
The owner plan uses exact source/port scopes, preserves security/firewall controls,
provides test-only DROP/cleanup rules, and distinguishes explicit rejoin from
automatic healing. No such network rules were applied by this work.

The managed executor has no VPN or external TCP grants and provides no second
independent host. Same-LAN, different-network, real partition/firewall and NAT tests
are therefore **NOT EXECUTED**. The template remains unfilled. The owner must run
and record those scenarios before any corresponding reachability claims.

## What a relay would mean

A directly reachable peer is enough to bootstrap where routing/firewalls allow
it; invitations do not create routes or port mappings. If both sides are behind
unreachable NAT/firewalls, an authorized routable VPN/path or a separately designed
relay may be necessary. The current seed role is not a deployed STUN/TURN service,
relay circuit, hole-punching implementation or permission to expose a public node.

Current gossip relays terminate per-link Noise connections and can read plaintext
application payloads, topics and origin metadata, plus observe endpoints, sizes,
timing and topology. Origin signatures protect integrity, not secrecy from those
relays. Park shared-key channel ciphertext adds a separate boundary, subject to its
existing key-holder/replay limitations. A future opaque byte-forwarding circuit
with end-to-end leaf Noise might hide payloads from a relay while still exposing
metadata; that is a hypothetical design, not a current guarantee or feature.

## Verification record

Environment: managed Linux/amd64, Go 1.27.3, Python 3.12.14. Current toolchain only;
declared minimum Go 1.24.1 and other OSes are not validated by this record.
Required commands (final results and hosted run link are recorded in the PR):

```sh
python3 scripts/check_engine_boundary.py
python3 scripts/check_invitation_vectors.py
python3 scripts/check_external_consumer.py
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
go run ./examples/echo
python3 scripts/demo.py
python3 scripts/check_netcheck.py
gitleaks git --log-opts='--all --full-history' --redact .
gitleaks dir --redact .
```

Race tests are evidence for exercised schedules, not proof. Short fuzz runs are
smoke coverage. Parser/corpus checks have no network I/O; live tests use loopback.
Existing adversarial authentication, admission, replay, unsafe-referral and frame
bounds remain required and unchanged. No controls were relaxed for verification.

## Readiness and next phase

**Approaching structural extraction readiness; NO-GO for publishing in this pass.**
Complete the reviewed integration sequence, choose module/import/version support,
run minimum/current Go and supported OS matrices, and preserve license/notice
policy before a separately authorized extraction. An external consumer working
with local replace is useful evidence, not a published-module compatibility claim.

Recommended Phase 5: owner-run the two-real-host LAN and authorized routed-network
matrix, record failures honestly, then assess a release-candidate API/import path
and platform matrix. Choose any NAT/relay scope only from those results with its
own threat model/authorization. No game feature, enterprise role system, public
hosting or module publication follows automatically from this handoff.

Remaining high risks for exposed/sensitive services: unsigned invitation source
replacement, malicious/Sybil/eclipse peers and finite-capacity exhaustion. Medium
limits: permitted-range/public port probes, volatile replay, DHT freshness/author
trust, plaintext identities, lossy delivery/sync, bootstrap/route availability,
relay-visible payloads and application authority/untrusted output. All remain in
SECURITY.md; this work does not certify sensitive production communications.
