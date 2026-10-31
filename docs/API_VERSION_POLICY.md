# Experimental API and compatibility policy

The public `engine` API is approaching a separable library boundary, not a stable
published module. Park, echo, the diagnostic consumer and a temporary external Go
module consume it without networking internals. No new public API is added in
Phase 4. Clock seams for deterministic fixtures are **private**; production parse
and encode still use the real clock and enforce expiry.

## Keep these versions distinct

| Surface | Current contract | Change policy |
| --- | --- | --- |
| Go API/module | Local `p2p-park/engine`, unpublished | Avoid unnecessary signature changes. Record source/behavior changes and consumer migration before publishing; no stable semver promise yet. |
| Core wire | `p2p-engine/0.1.0` | Exact Hello version required. No negotiation/rolling-upgrade guarantee. Changes to authenticated framing/handshake semantics require explicit version review and adversarial compatibility tests. |
| Invitation | Canonical v1 | Preserve committed canonical byte fixtures. Security-relevant extension fields require an explicit new version; old readers reject unknown fields/versions. |
| Application schema | Park `p2p-park.0.3.0`, echo, netcheck | Owned by the application. Joining an overlay does not negotiate application behavior or grant permissions. |
| Identity disk file | Version 1 | Private storage format, not invitation metadata. Never silently regenerate a corrupt identity or silently rotate its keys. |

A Go API addition does not automatically require a wire change; an invitation
version does not negotiate around a core version mismatch. V1's JSON ordering,
base64url rules and required identity fields are compatibility commitments for
this experimental format. New destination-policy security exclusions may reject
formerly accepted endpoints without changing serialized bytes: document such a
security correction and update its rejection vectors. Do not silently broaden
allowed destinations or ignore a future authorization field.

## Reviewed API contracts

- Construct with New, call Start once, Close concurrently/idempotently. A
  pre-cancelled Start is retryable; an actual startup failure closes the instance.
  A closed Engine cannot restart. Start's context owns sessions. Zero-value Engine
  is not usable. Inputs must not be concurrently mutated during copying.
- Connect/Join contexts bound their attempts, not an already accepted session.
  Cancellation closes stalled setup and preserves its context error when EOF
  races cancellation. Join validates all records/policy before dialing; partial
  successes remain live under Start. Close cancels/joins tracked Join/DHT/core work.
  A successful result is not proof of continued reachability or remote app readiness.
- Non-nil contexts are required. Start/Join explicitly reject nil; other methods
  follow the documented non-nil context precondition. This is not a new promise
  that every misuse yields the same typed error. Review consistency before freeze.
- Engine methods support concurrent use after construction. Messages/Events are
  single streams, not broadcast subscriptions. Receiver-owned message byte slices
  and copied snapshots/public keys avoid exposing mutable private engine state.
- Send means bounded enqueue, not remote receipt. Publish is best effort and may
  partially fan out. Queues/cache saturation may drop/disconnect/fail closed.
  Events can be lost; Peers is only a snapshot. No durable-delivery guarantee.
- Use errors.Is for documented engine/context sentinels. BootstrapUnavailable
  wraps endpoint errors; local policy rejection is InvalidInvitation. Remote
  rejection can be EOF or a handshake failure. Pin/network/remote-policy failures
  do not all have distinct public sentinels; do not parse error strings or assume
  exact wrapper order. Configuration and filesystem errors can be untyped.
- The overlay is immutable per Engine. Hello and signed gossip enforce its ID;
  invitation import never switches a running Engine's network. Names and public
  network IDs are not authorization. Restricted lists remain local paired-key policy.
- Bootstrap pins authenticate a Noise endpoint and its Ed25519 binding before
  admission. Invitations are unsigned public hints; issuer/community authenticity
  needs a trusted source/reference. Per-attempt pins do not become permanent
  membership policy or a certificate authority for referrals.
- DHT results remain untyped validated records, not membership certificates or
  freshest-state consensus. Caller checks type/signer/name/authority and retains
  its own durable sequence/idempotency state where required.

No release-blocking regression was found in these reviewed contracts. Existing
limits remain; tests exercise selected schedules, not a proof or an independent
security audit. Added compatibility tests verify attempt cancellation does not
kill an accepted session, a failed signing pin cannot poison an existing session,
and the external consumer can use stable sentinels without internal imports.

## Interoperability fixtures

`engine/testdata/invitations/v1.json` has 21 deterministic vectors with a fixed
reference Unix clock, literal URI and JSON bytes, public identities, expected parse
errors and selected live-join scenarios. It covers valid/no-expiry/expiry/multiple
endpoints, unsupported versions, malformed/unknown/duplicate/noncanonical input,
unsafe endpoints, substituted identities and network/scope isolation.

Go and an independent Python standard-library codec check the same committed
bytes. Go live tests substitute ephemeral endpoints solely for port isolation,
retain deterministic public identities, and verify real transport/identity/domain
checks. Parser fixtures do not perform network activity. Deterministic private
TEST material is derived at runtime in temporary files; none is a production
credential. Never reuse fixture identities as a deployment identity.

The Python check verifies this bounded fixture profile; it is not another
network implementation or exhaustive proof of compatibility with every possible
IP-policy registry/update. Add cases with any intentional format/policy change.
Do not regenerate expected bytes using the implementation under test just to
make a failing vector pass.

## Independent packaging assessment

**Structural assessment: approaching extraction readiness. Publication: NO-GO
for this pass.** The engine closure excludes game schemas/storage; the game graph
stops at engine. A temporary external module with local replace builds and runs
under the race detector in CI. No nested/published engine module is created.

Before a separately authorized extraction:

1. Complete the reviewed Git integration sequence; main must represent the tested
   API and adapter before selecting a release/tag.
2. Choose the canonical hosted module/import path and experimental version policy.
   Current `p2p-park` consumers use local replace; changing imports is an explicit
   migration, not a transparent `go get` publication.
3. Decide support for the public DTOs, error categories and canonical v1 bytes.
   Keep Clock helpers private. Expand API only for demonstrated consumer needs.
4. Run declared minimum Go 1.24.1 and selected current toolchains/OS matrices.
   Current evidence is Linux/amd64, Go 1.27.3. Existing Unix LAN socket imports
   prevent claiming Windows support even when optional LAN discovery is off.
5. Keep identity/peer-state path semantics, bounded streams, single-use lifecycle,
   diagnostics and DHT limitations explicit. Evaluate source-compatible remedies
   for error specificity/event observability only when a consumer needs them.
6. Preserve required third-party notices and obtain the owner's distribution/license
   decision if publication rights change. This pass leaves reserved rights intact;
   reusable architecture is not a general commercial deployment grant.
7. Finish the owner-run connectivity matrix before claiming LAN/WAN reachability.
   Lack of NAT traversal need not be hidden by packaging an experimental library.

Moving the public package plus its generic internal dependency closure can later
preserve encapsulation. Do not copy game packages or create a second networking
implementation. No separate repository, module publication, license change or
production-readiness claim is authorized by this assessment.
