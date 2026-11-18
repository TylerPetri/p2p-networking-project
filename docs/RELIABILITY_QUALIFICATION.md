# Reliability behavior tests and release qualification

This pass adds tests, application-layer fixtures and qualification tooling. It
changes no production Go source, public acknowledgement API, wire protocol,
invitation bytes, identity format, resource limits, license or deployment.
“Production qualification” is the question being measured, not a readiness claim.
The experimental engine has not been independently security-audited.

## Integration snapshot

Main `348eef58250ec4b1977ced5156d9b2dcaea3509d` contains cleanup PR #8 but still
uses `module p2p-park`. SDK PR #9 was merged into the former feature branch at
`36acdcecc4f840e73339a0f84132a6454f756af8`, not main. That feature tree is exactly
equal to SDK commit `6aae960fa9f3fd7ceb97af60a77fe63c90cbbc1e`. The owner reports
the reviewed ZIP matched that SDK commit; this pass verifies Git trees, not an
unprovided ZIP or an independent security audit.

[Integration PR #10](https://github.com/TylerPetri/p2p-networking-project/pull/10)
contains only the already-reviewed SDK delta. This reliability branch is stacked
on its source `codex/engine-first-public-readiness` so the tests are a separate
diff. Owner sequence: review/merge #10 preserving ancestry, retarget the test PR
to main, verify its test/tool/documentation-only diff and rerun checks. No merges,
force-pushes, tag creation, history rewrites or deployments are performed here.
Recheck live refs before following this snapshot if branches have advanced.

## Commands and result semantics

The ordinary CI suite automatically includes the short new tests:

```sh
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
python3 scripts/check_engine_boundary.py
python3 scripts/check_invitation_vectors.py
python3 scripts/check_external_consumer.py
go run ./examples/echo
python3 scripts/demo.py
python3 scripts/check_netcheck.py
```

Existing invitation, admission, identity, adversarial, concurrency, fuzz and
shutdown tests remain. No new always-failing acceptance check is added to merge
CI. Subprocess helpers skip when not invoked as helpers; the explicit delivery
acceptance test skips unless P2P_QUALIFY=1. Those skips are not qualification PASS.

Run explicit qualification from the repository root (Go, C compiler, Python 3):

```sh
# Every release gate: currently exits 1, with delivery FAIL and media NOT_RUN.
python3 scripts/qualify.py --gate all --output /tmp/p2p-qualification.json
# Current behavior only; PASS means the documented loss was reproduced.
python3 scripts/qualify.py --gate behavior --output /tmp/p2p-behavior.json
# Stronger semantics demonstrated by the application TEST MODEL only.
python3 scripts/qualify.py --gate room --output /tmp/p2p-room.json
# Explicit receipt/receiver-pressure acceptance requirement: currently FAIL.
python3 scripts/qualify.py --gate delivery --output /tmp/p2p-delivery.json
# Strict concurrent discovery/manual join settlement, no hidden retry:
python3 scripts/qualify.py --gate reconnect --soak-rounds 100 --output /tmp/p2p-reconnect.json
# Manual real-device/media evidence absent: NOT_RUN, exit 1.
python3 scripts/qualify.py --gate media --output /tmp/p2p-media.json
# Explicit longer run. Default capacity test in CI uses only 3 rounds.
python3 scripts/qualify.py --gate capacity --soak-rounds 100 --output /tmp/p2p-soak.json
```

The runner uses `go test -race -count=1` (or explicit pressure repetitions),
bounded timeouts and exact required test names. A skipped/missing required test,
compile failure, timeout, assertion failure, or media NOT_RUN never yields PASS.
It records commit, dirty state, actual toolchain/platform, commands, exit codes,
full logs and scoped outcomes. Run on a clean commit for candidate evidence.
A selected subset PASS does not waive the other release gates. The current media
gate does not ingest a manually edited PASS: its integration needs a real app and
an independently reviewable evidence adapter later.

No capacity latency SLO is invented here. Register target hardware, workload,
connection/message limits, duration, minimum throughput and p95/p99 budgets before
a production load campaign. This bounded smoke/soak measures completion, timing
and resources; it does not qualify production load or internet reachability.

## Delivery findings and deterministic schedules

| Observation | Current behavior | Qualification implication |
| --- | --- | --- |
| Full SDK Messages queue | Sender Send returns nil; new receive message omitted; later message still arrives | FAIL if a release requires receipt or receiver-pressure feedback |
| Full outbound peer queue | Resource-limit error, slow peer disconnected; healthy fan-out still enqueues | Partial fan-out; healthy enqueue is not a receipt |
| Publish with zero peers | nil, no recipient | nil is not a delivery count |
| Disconnect after enqueue | First enqueue nil; fixture withholds writer and delivers nothing; later Send reports peer closed | Outcome after enqueue is unknown |

`engine/reliability_test.go` primes the actual SDK queue (currently 128) with
**synthetic sentinels**, holds back only the SDK pump, and sends one real signed,
Noise-encrypted message through Send. It waits for that envelope in core Incoming,
starts the real pump, and uses a newly connected peer event as a pump barrier
before draining. This proves processing finished while the queue was full without
racing its output select or relying on a sleep. It then sends/receives a new
message on the surviving connection. Primed sentinels are not counted as network
messages. No production test hook, queue resize or security bypass is added.

The core fan-out/disconnect fixtures hold back a writer and fill real bounded
peer channels. They isolate enqueue semantics from OS TCP buffer sizing. They
are not a real WAN blackhole or slow TCP reader benchmark. Existing stalled-dial
and session tests provide separate real-socket cancellation coverage.

There is no public receiver-drop counter/receipt API. The capacity report marks
public queue drops unobservable, while the controlled white-box probe records
one known omitted overflow. No new counters or delivery guarantee are invented.

## Room-control model: stronger application semantics

`qualification/room_test.go` is test-only and imports only the public engine.
It models one room authority, one creator signing key, and public viewers:

- Actions carry a stable ID, expected sequence and small replacement state.
  The authority serializes them; wrong order or reusing an ID with a different
  signer/payload is rejected. Transport enqueue alone never confirms an action.
- Authority decisions use `Message.SigningKey`, not ClaimedPeerID or an actor
  string in a request. Replies route through the verified signing-key/peer map.
  A publicly joined viewer claiming the creator identity is denied. The client
  checks the separately expected authority signing key before accepting responses.
- Apply and stable-ID record are saved together before acknowledgement. A lost
  acknowledgement leaves the client outcome unknown, with its pending ID saved.
  Retrying that ID returns its original result without a second application.
- Faults occur before enqueue (cancelled context), after enqueue/before apply,
  and after persisted apply/before acknowledgement. Actor/payload substitution,
  required ordering, wrong authority, stale snapshots, gapped deltas and stale
  acknowledgements are rejected. A stale duplicate ack requires reconciliation.
- Restarted clients load last-confirmed sequence and pending ID but start stale.
  They reconcile a contiguous delta from their checkpoint or reload an
  authoritative snapshot. Each request has a fresh random correlation ID, echoed
  in the signed response and checked against the outstanding attempt; a previous
  process/attempt response cannot make a resumed stale cache appear current.
  This correlation is distinct from the persisted stable action ID. “Current”
  means confirmed at the completed exchange,
  not a lease guaranteeing no future changes or instantaneous disconnection
  detection. A real UI must invalidate/reconcile on connection and state events.
- A real authority child process is killed after persisted apply, before ack.
  Authority and client are then started as new OS processes using persisted
  identities/state. The same pending ID is retried; both confirmation and room
  remain at sequence 1 with one application. Graceful helper shutdown is bounded.

The filesystem model uses a single writer, file Sync and atomic rename. It does
not claim directory/power-loss durability, malicious-filesystem resistance,
multi-authority consensus, durable engine queues, automatic retries, revocation,
a production transaction store, secure key custody or infinite dedupe retention.
The tiny ledger is intentionally unpruned for bounded tests; it is not a scalable
production design. No production application protocol is shipped by these tests.

## Capacity, compatibility and cleanup evidence

`TestBoundedChurnRecovery` keeps nodes on numeric loopback endpoints and measures
join/fallback/recovery times, authoritatively applied/acknowledged actions,
explicit bootstrap failures and resource samples. Canonical-first bootstrap is
a TCP service deliberately returning EOF (unusable for authenticated bootstrap),
then a healthy pinned endpoint. This is not an unreachable WAN timeout test.
Seed loss starts only after both peer snapshots and a fresh application sync
prove the referral-created direct path; it is followed by fresh sync with that
surviving peer. An initial CI run exposed a fixture race: redundantly dialing
while discovery established the same peer could leave a disconnected session.
The fixture now observes discovery instead of issuing that competing dial; no
retry is hidden inside the seed-loss assertion. Concurrent duplicate dial
settlement and continuous liveness after Join are not newly guaranteed by this
pass. An explicit `--gate reconnect --soak-rounds 100` acceptance probe retains
that competing-dial schedule and reports every failed Join, enqueue or signed
recovery instead of retrying failures into success. Its zero-failed-trials
requirement is stricter than the current API contract and runs outside merge CI.
A PASS covers only the observed bounded schedule, not all interleavings. The
existing triangle gossip/DHT setup helper also forms a settled star before
closing the mesh through discovery, following an observed minimum-Go triangle
setup failure. Its original delivery/deduplication assertions remain intact;
competing manual/discovery dialing is tested in the explicit acceptance probe. An active authority
engine restarts from its ledger/identity; other rounds disconnect and rejoin.
Application state must converge after every round.

Default: 3 rounds; explicit capacity run: 1..1000. It records sampled goroutines,
Go heap and Linux current RSS before, during and after shutdown. RSS is null on
other platforms. Samples are not peak RSS or hard memory bounds; runtime arenas
and fixture-held objects may remain. Listener rebinds, peer-removal conditions,
child exit and tracked Close are checked. Goroutine settling has a bounded
before+8 tolerance for runtime activity; this can catch accumulating leaks in a
long run but is not a proof of no leaks. No arbitrary wait is used as a success
condition; timers bound failures and tickers observe explicit state predicates.

The wire matrix connects real core nodes using exact `p2p-engine/0.1.0` and
rejects empty, legacy `park-p2p/0.2.0` and future `p2p-engine/0.2.0` before admission.
Connection errors are visible in evidence. No negotiation/rolling upgrade is
promised. Existing 21 invitation vectors, pin/network isolation, admission,
malicious referrals and shutdown cancellation regressions remain enforced.

## First implementation work driven by these results

1. Decide and implement a receiver-pressure/receipt/error observability contract
   in a separate reviewed implementation PR. The current sender cannot tell
   successful delivery from a remote SDK queue drop. Preserve finite budgets;
   making queues unbounded or merely sleeping longer would not solve reliability.
2. Define the actual application's stable IDs, authoritative state, persistent
   idempotency/sequence rules, retry/reconciliation and stale-UI behavior. The
   model proves a pattern, not production delivery. Any engine acknowledgement
   API or wire change needs separate design/version review and adversarial tests.
3. Investigate any failing concurrent discovery/manual join acceptance trials:
   session arbitration/liveness after simultaneous dials needs its own lifecycle
   implementation review; the ordinary capacity test does not hide retries.
4. Build and instrument the separately scoped media consumer before executing
   [streaming qualification](STREAMING_QUALIFICATION.md). Real-device/media rows
   remain NOT RUN; owner-reported historical multi-device connectivity is not
   playback evidence. NAT/relay, application authority and end-to-end payload
   encryption limitations remain in SECURITY.md and the SDK release checklist.

See the reliability PR for actual final-SHA commands, toolchains/platforms, JSON
results, CI status and bounded-run measurements. Owner approval remains required
for tagging and deployment; green ordinary CI does not override failed gates.
