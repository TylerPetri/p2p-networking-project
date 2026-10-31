# Network invitation format v1

An invitation is **public discovery information**, not a password, membership
capability or application permission. No account or shared secret is required.
Get it from a trusted source or compare its network/key information using a
separate trusted channel. V1 is deliberately **not issuer-signed**: the engine
verifies the named peers' keys, not who distributed the invitation or whether
those peers are honest members of the intended community.

## Representation and canonical encoding

```text
p2p://invite/<unpadded-base64url(canonical-JSON)>
```

JSON field order is exactly the following, with no whitespace:

```text
{"version":1,"network_id":"<64 lowercase hex>","bootstraps":[{"address":"<numeric IP:port>","peer_id":"<64 lowercase hex>","signing_id":"<64 lowercase hex>"}],"expires_unix":<integer>}
```

The placeholders above are explanatory, not a parseable example. `expires_unix`
is omitted when zero. All other fields are required. Bootstrap records are sorted
by their canonical address using ascending byte order; duplicate endpoints are
rejected even if their identities differ. Multiple addresses for one identity are
allowed but do not create multiple independent peers. IDs are 32 bytes encoded as
64 lowercase hexadecimal characters. `peer_id` is the Noise/X25519 static public
key; `signing_id` is the Ed25519 public key verified in the direct Noise handshake.
`network_id` is a public overlay identifier, not an owner certificate or secret.
The application namespace is chosen by the consuming application, not this data.

Limits: **2,048 URI bytes**, **1,400 decoded JSON bytes**, **1–4 bootstrap records**,
**64 bytes per endpoint**. Decode limits apply before JSON parsing, including
invalid inputs. Fixed fields and values define the canonical bytes; this is not
a general-purpose JSON canonicalization or new cryptographic primitive. Encoding
copies and sorts the caller's peer list. Parsing accepts only the exact encoding:
no padding, CR/LF, duplicate/case-alternate/unknown keys, alternate integer/string
representations, unsorted records, extra JSON values or trailing whitespace.

Only version 1 is supported. Future versions/unknown fields fail closed; adding
security-relevant semantics requires an explicit new format version and reader
support. There is no permissive extensions map or silently ignored admission flag.
Invitation versions are separate from core wire protocol `p2p-engine/0.1.0`.
A version-valid invitation cannot negotiate around the core Hello version check.

The URI can be encoded directly as QR text or embedded by an application in a
link fragment. The engine accepts the bare URI only, never fetches a URL, follows
a redirect, resolves a hostname or launches a browser. QR encoding is a visual
representation of these bytes, not another networking protocol. Practical QR
capacity depends on size/error-correction level; prefer one or two endpoints.
No QR library, web frontend or mobile application is introduced.

## Address and receiver policy

V1 supports canonical numeric TCP endpoints: `127.0.0.1:43001` or
`[2606:4700::1111]:43001`, for example. The latter is syntax only, not a project
bootstrap service. Ports must be 1–65535. IPv6 must use canonical compressed
lowercase spelling. DNS names, IPv6 zones, IPv4-mapped IPv6, URI/userinfo/path forms,
leading-zero ports and wildcard listeners are rejected.

Parsing always rejects unspecified, multicast, broadcast, link-local, reserved,
documentation, benchmark and supported special-use exclusions. IPv4 exclusions:
`0/8`, `169.254/16`, `192.0.0/24`, `192.0.2/24`, `192.88.99/24`, `198.18/15`,
`198.51.100/24`, `203.0.113/24`, `224/4`, `240/4`, plus known platform endpoints
`168.63.129.16` and `100.100.100.200`. IPv6 public candidates must lie in
`2000::/3`; `2001::/23`, `2001:db8::/32`, `2002::/16` and `3fff::/20` are excluded.
ULA and loopback are structurally valid local candidates. This conservative
profile excludes DNS rebinding and common IPv4/IPv6 transition aliases; it is not
a complete or automatically updated registry of every provider's special service.

`JoinOptions{}` permits the remaining public candidates. Private, loopback and
CGNAT (`100.64/10`) addresses additionally require explicit receiving-operator
`AllowedCIDRs`. At most 16 canonical numeric prefixes are accepted. An invitation
cannot provide this policy or enable a prohibited address. Even a loopback-bound
engine requires explicit approval of local invitation destinations. Avoid broad
ranges: approval allows attempts against selected ports inside that range. IP
policy is not a firewall or protection against arbitrary routing/NAT configured
outside the process. Public endpoints also allow bounded port probes.

All invitation fields, network match, scope options and configured pin conflicts
are checked before **any Join dial**. Parse before constructing/starting a new
node if no network activity at all should occur for invalid imported data. An
already-running engine may independently have manual bootstrap/LAN/discovery work;
Join does not undo those explicitly configured activities.

## API and lifecycle

```go
// Creator is already started. Nil advertises creator.Address().
inv, err := creator.Invitation(nil, time.Now().Add(time.Hour))
if err != nil { return err }
uri, err := inv.Encode()
if err != nil { return err }

// Receiver: obtain uri through a trusted sharing channel, then parse first.
parsed, err := engine.ParseInvitation(uri)
if err != nil { return err }
receiver, err := engine.New(engine.Config{
    NetworkID: parsed.NetworkID,
    Application: "my-app", // the application's own namespace
})
if err != nil { return err }
defer receiver.Close()
if err := receiver.Start(lifetime); err != nil { return err }
result, err := receiver.Join(attempt, parsed, engine.JoinOptions{
    AllowedCIDRs: []string{"127.0.0.1/32"}, // local demo only, not from URI
})
// Inspect err and result.Failures even when some result.Connected entries exist.
```

`Engine.Invitation(addresses, expires)` advertises **only this node's own key pair**.
It never promotes a peer-list/DHT referral into an endorsed bootstrap. Empty
addresses uses Address(); wildcard binds require explicit reachable addresses.
There is no reachability probe. To name another bootstrap, merge its separately
trusted generated `Bootstraps` records into the same-network Invitation and Encode.
Public structs are editable for operator assembly; Join revalidates and copies
all records. Do not mutate inputs concurrently while an API call reads them.

`Join` requires a started engine whose immutable NetworkID matches the invitation.
It tries every record sequentially in canonical order, with a five-second timeout
per attempt, the caller's overall context, and any earlier invitation expiry.
Four attempts therefore have at most roughly 20 seconds of network wait unless
cancelled sooner. Ordering is canonical, not a latency priority. Sequential
attempts avoid extra fan-out workers; an unreachable early peer can delay later
ones. Existing connection/handshake budgets still apply across concurrent calls.

Each attempt checks the expected Noise **and** signing keys before peer admission,
plus normal protocol, overlay and local/remote membership policy. Existing manual
address pins cannot be overridden. Already connected matching peers may be reused;
simultaneous duplicate attempts may reuse a matching authenticated session. Normal
connection races/disconnects can still invalidate a snapshot immediately.

`JoinResult.Connected` records completed authenticated attempts, not continued
liveness or independent-peer counts. `Failures` contains bounded endpoint errors.
Nil Join error means at least one success; unavailable alternatives remain in
Failures. If none succeeds, `ErrBootstrapUnavailable` wraps the failures. Caller
cancellation/expiry returns an error even if an earlier attempt succeeded; earlier
connections remain owned by Start's lifetime. There is no rollback or hidden retry.
Repeated/concurrent Join is supported; global session limits may reject capacity.
Close cancels pending attempts and waits for registered Join work and core sessions.

Use `errors.Is` with `ErrInvalidInvitation`, `ErrUnsupportedInvitationVersion`,
`ErrInvitationExpired`, `ErrBootstrapUnavailable`, engine lifecycle/resource
sentinels and context errors. An expired invitation is rejected at parse/encode/
join time. During Join, expiry bounds pending setup; it does **not** disconnect
previous successes or revoke existing membership. Expiry is public advisory data,
not signed revocation: someone can repackage public discovery information.

## Trust, discovery and restricted networks

The choice of discovery records is trusted only as far as their sharing channel.
Tampering with one expected key prevents connection to the original endpoint.
Replacing the whole invitation with an attacker's keys/server can redirect a user
who lacks an independently trusted reference. No issuer proof, human identity,
network-owner proof, Sybil protection or honest participation is claimed.
A malicious bootstrap can withhold honest peers or try to eclipse a joiner.

Per-attempt pins do not mutate permanent trust, `ApprovedPeers`, address pins or
`DiscoveryCIDRs`. Automatic referrals still pass existing numeric scope checks,
claimed Noise-key verification, protocol/domain checks, budgets and replay rules;
referrers are not certificate authorities. Auto-reconnect outside Join retains its
existing policy; use configured paired-key Restricted admission where ongoing
membership pinning is required. An invitation never auto-approves the joiner or
bootstrap in Restricted lists. Both ends must independently permit a connection.

Application operations still need authorization using trusted verified signing
keys, never the public network ID, names or unverified relayed transport-ID claims.
Signed gossip remains overlay-bound; different overlays do not accept its messages.
Applications in a shared overlay have separate delivery namespaces, not a privacy
sandbox. Existing transport/replay, message-size and worker limits are unchanged.

## NAT, firewall and availability limits

A reachable participant must remain available for a disconnected/new peer to join.
Already connected peers can communicate after the original bootstrap stops; a
fully partitioned network cannot rediscover itself without reachable contact info.
Keep multiple current trusted endpoints and persistent identities if continuity
matters. Ephemeral identities change on restart and invalidate old pins.

V1 has no STUN/TURN, hole punching, UPnP, relay-circuit negotiation, DNS/mDNS resolver,
IPv6 transition support or automatic public-address detection. A seed role is not
an Internet relay service. Operators must provide routable endpoints and any
required firewall/port-forward rules. The core's advertised listen ports/referral
addresses do not automatically become NAT mappings. A stale, private or unroutable
endpoint may parse successfully and still be unreachable from another host.

`go run ./examples/echo` demonstrates three **localhost** nodes, invitation join,
bootstrap loss, continued authenticated messages and reconnection through another
peer. It does not prove Internet-wide connectivity or NAT traversal. Use disposable
data; this protocol remains experimental and not independently security-audited.
