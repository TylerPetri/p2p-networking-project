# Future browser/mobile streaming qualification — NOT RUN

No browser/mobile media application, media relay path, authorized device fleet or
cellular/WAN campaign is available in this pass. Every row below is **NOT RUN**.
Go loopback, room-fixture PASS and owner-reported multi-device engine connectivity
cannot qualify media playback. The engine remains experimental, not independently
security-audited. Do not expose development listeners or alter firewalls/routes
without owner authorization for the exact hosts/path.

## Prerequisites and reproducible record

Record the app build/commit, engine commit/wire/invitation version, date, physical
device model, OS/browser versions, hardware decoder/media settings, codec,
resolution/bitrate/frame rate, network provider/type, authorized endpoint scopes,
relay configuration and expected authority/creator identities. Use disposable
room content and independently trusted invitations. Keep private keys and personal
information out of evidence; redact unnecessary routing metadata.

Predeclare required success rate, trial count, join/first-frame/recovery budgets,
freeze definition and acceptable state convergence before running a release
campaign. Without thresholds or enough trials, record measurements rather than
claiming qualification. Failed, blocked, uninstrumented and unexecuted rows must
not be folded into successful trials or silently excluded from denominators.

## Manual matrix

| Scenario | Steps and required assertions | Status |
| --- | --- | --- |
| QR → public watch on home Wi-Fi | Scan a shared link on a fresh viewer device; validate invitation/pins; join correct room; receive real new video/audio; viewer cannot claim creator/broadcast role | NOT RUN |
| QR → watch on cellular | Repeat on a separate physical cellular device, not a laptop hotspot labelled cellular without topology evidence; record actual control/media paths | NOT RUN |
| Wi-Fi → cellular mid-watch | Maintain room identity, switch network; record disconnect/stale UI, freeze, resumed frames and recovered authoritative room sequence; no duplicate control action | NOT RUN |
| Cellular → Wi-Fi mid-watch | Reverse transition; verify identity/device authorization and stale cache handling, media recovery and bounded abandoned connections | NOT RUN |
| Media relay fallback | On an owner-approved restricted path, make direct media unavailable; prove the selected media path is actually relayed using application/RTC statistics; verify expected media encryption and room authority | NOT RUN |
| Broadcaster reconnect | Interrupt broadcaster link/app and restart with its authorized identity; viewers show honest offline/stale state; reconnect/snapshot converges without duplicate start/stop action | NOT RUN |
| Viewer reconnect/app restart | Restart viewer with last-confirmed room sequence; catch up or reload snapshot; first new frame belongs to current authorized broadcast, not stale cached UI | NOT RUN |
| Seed/active control peer lost | Remove the original seed/peer; verify pinned fallback/rejoin, room correctness and fresh media independently; record which path failed | NOT RUN |
| Unauthorized creator attempt | A public viewer joins via the same discovery information but attempts broadcast/moderation; reject by verified signing/device authorization, not display name or possession of the link | NOT RUN |
| Cleanup after churn | End watching/broadcasting; verify capture/decoder/transport/listener resources close and no media continues unexpectedly | NOT RUN |

Repeat on the actual supported iOS/Android/browser combinations, low-bandwidth or
lossy paths and home routers chosen by the owner. Browser backgrounding, permission
prompts and autoplay restrictions must be recorded as part of the actual flow.
No native mobile, browser embed or WAN support is inferred from Go SDK CI.

## Measurements: what counts

Use monotonic timestamps within each device. For cross-device timing, record
clock synchronization/uncertainty rather than subtracting unsynchronized clocks.
Give each trial and critical action a stable ID. Preserve raw event logs and
redacted application/RTC path statistics so another reviewer can reproduce values.

| Measurement | Definition and evidence |
| --- | --- |
| Join success | Number of attempts that authenticate the intended network/authority and load correct room state / all initiated attempts; record control join separately from media success |
| Control join latency | Join initiation → authenticated control session plus authoritative state loaded; retain each attempt and failure reason |
| Time to first frame | User scan/open or explicit watch request → first newly decoded/presented frame of the expected live stream; report which starting event, QR/app/permission delays and media-only delay separately |
| Freeze duration | Every interval without expected new presented frames while playback should advance; predeclare frame-cadence/threshold (for example 500 ms), include count, max and total; audio-only/cached-frame display is not video recovery |
| Recovery time | Network-change/failure event → sustained new frames and correct room state; predeclare “sustained” interval, record control, first-frame and state convergence separately |
| Room-state correctness | Expected authority signing key, last-confirmed and authoritative sequences, role/broadcast identity, critical action ID/apply count; stale/unknown state is visibly labelled until verified sync |
| Path and cleanup | Actual direct/relayed media candidate/path, endpoint scopes, control peers, resource samples and stopped capture/transports after exit |

Record per-attempt PASS/FAIL/BLOCKED/NOT RUN, raw durations and failure causes, then
compute percentiles and confidence appropriate to the declared trial count.
A timeout or successful enqueue cannot substitute for an observed frame/ack.
Never label an expected failure as meeting a release requirement.

Suggested result row:

```text
trial/build/device/OS/browser/network/topology:
authorized path and expected room/creator/authority IDs:
scan/watch start; control joined; authoritative seq loaded; first new frame:
network change; freeze start/end; control rejoined; media sustained; synced seq:
critical action ID; applied count; ack/reconcile outcome; stale UI interval:
media path evidence (direct/relay); resource cleanup evidence:
status; declared thresholds; raw logs/statistics; limitations:
```

## Reusing current cross-host tooling

Use `examples/netcheck` with the existing
[CROSS_HOST_TEST_PLAN.md](CROSS_HOST_TEST_PLAN.md) to isolate authenticated engine
reachability, peer/seed loss and explicit rejoin on two authorized real hosts.
Record those results in [the cross-host template](CROSS_HOST_RESULTS_TEMPLATE.md).
Use actual reachable bind/advertised addresses, trusted signing/Noise pins and
narrow operator-approved CIDRs/firewall rules. Do not disable security controls
or open a public node to obtain a PASS. Never copy private identity files between
physical devices to force reconnection to look successful.

Netcheck's nonce proves a control-message round trip, not frames, QR-to-watch,
a browser media connection or automatic network handover. The Go core does not
implement STUN/TURN, hole punching or an opaque media relay. Existing gossip
relays terminate Noise links and can observe unencrypted application payloads.
A future media relay/encryption design belongs to the streaming application and
needs its own threat model and privacy/traffic-metadata disclosure.

`python3 scripts/qualify.py --gate media` reports NOT_RUN and exits nonzero.
Manual rows remain NOT RUN until actual authorized trials and evidence exist;
filling this template alone does not make the automated release gate PASS.
