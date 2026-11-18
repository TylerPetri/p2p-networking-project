# Public engine release readiness

The engine is a reusable Go networking library for public overlays. Park is a
reference CLI. A public streaming application and private company tools can
both consume the engine and define their own user and device policies.

| Responsibility | Owner |
| --- | --- |
| Peer identity, discovery, authenticated transport, frame and connection limits | Engine |
| Link and QR room join, public viewing, creator permissions, moderation and media | Streaming application |
| Device enrollment, permissions, revocation, encryption keys and audit trail | Private tool |

A public room link should open the application on a phone and allow anyone to
join as a viewer. The streaming application decides who can broadcast. The
engine's invitation format describes how to bootstrap a peer. The first mobile
prototype should exercise an HTTPS room link, engine-backed room events and a
separate media path.

Private tool traffic on the public overlay needs recipient authorization and
end-to-end encryption above the engine, including key rotation and revocation.
Do not publish sensitive state to the public DHT. Peer addresses, traffic
timing and other routing metadata can still be visible on a public network.

## Connectivity evidence

The maintainer reports successful connections across multiple hosts and devices.
The tested OSes, network paths, peer counts and direct versus relayed paths
were not supplied. Automated checks provide reproducible loopback evidence.
Record future runs using the [cross-host results template](CROSS_HOST_RESULTS_TEMPLATE.md).

## Release milestones

1. **Go SDK preview.** Canonical module: `github.com/TylerPetri/p2p-networking-project`;
   public package: `engine`; minimum Go 1.24.1; target platforms Linux and macOS.
   Follow [SDK_PREVIEW.md](SDK_PREVIEW.md) for clean pre-tag remote resolution,
   actual CI evidence and separately approved post-tag verification. No tag exists
   merely because a candidate commit resolves. Wire/invitation compatibility is
   preserved; [API_VERSION_POLICY.md](API_VERSION_POLICY.md) defines v0 scope.
2. **Public overlay beta.** Measure connect and join latency, reconnect behavior,
   steady-state resources and behavior under load. Use a reproducible matrix
   covering seed loss, mixed network paths and constrained peers. Record connection
   path and failure reasons. Test whether current admission and resource budgets
   leave room for legitimate peers during sustained churn.
3. **Two reference consumers.** Demonstrate a mobile browser joining a public
   stream from a QR code, and an authorized private tool exchanging encrypted
   commands on a public overlay. Exercise reconnect in both and verify that
   revoking a private device prevents it from reading newly sent data.
4. **Distribution and support.** Decide which rights a release grants before
   tagging it for outside use. The current [license](../LICENSE.txt) grants
   limited evaluation rights. A commercial release can center on hosted
   rendezvous or relay operations, integration support and maintenance. An
   open-source release needs an explicit license decision and corresponding
   notice updates. Public availability of the Git repository already exists
   independently of those decisions.

Treat the SDK preview, operation of a public overlay and a distribution license
as separate milestones. Review the [security model](../SECURITY.md) before
expanding the public beta.
