# Go SDK preview checklist

The canonical root module is `github.com/TylerPetri/p2p-networking-project`.
Applications import `github.com/TylerPetri/p2p-networking-project/engine`.
Park (`cmd/park-node`) remains a small chat/shared-key-channel reference CLI;
echo and netcheck are independent public-API examples. No nested module,
separate repository, new wire format or new cryptography is introduced.

Proposed first tag: **`v0.1.0-preview.1`**. This is a proposal, not a published tag.
A tag requires explicit owner approval. The existing reserved-rights evaluation
license and third-party notices stay in effect. Installing a Go module does not
grant production, commercial redistribution or general modification rights.

## Product and trust boundaries

The intended overlay stays public. Anyone with public discovery information may
attempt to join. A public stream's link/QR is shareable; streaming, media delivery,
moderation and creator permissions belong to its application. The engine does
not implement those features or make a QR code an authorization credential.

Private tools use that same public overlay and own device authorization,
revocation, key distribution/rotation and end-to-end payload encryption. Noise
protects each link; a gossip relay can see unencrypted application payloads.
Do not put private data into public DHT records. Network IDs separate message
domains, not business authority; neither invitation possession nor admission
makes a peer trustworthy. Existing engine admission controls remain available
and unchanged, but no enterprise identity/membership system is added.

## Install and verify before a tag

Use Go 1.24.1 or later, a C compiler for race checks, and Python 3 for repository
verification scripts. Replace `FULL_PUSHED_CANDIDATE_SHA` with the actual reviewed
40-character commit. Run from a fresh consumer directory:

```sh
go mod init example.com/sdk-evaluation
go get github.com/TylerPetri/p2p-networking-project/engine@FULL_PUSHED_CANDIDATE_SHA
```

Import only the public package:

```go
import "github.com/TylerPetri/p2p-networking-project/engine"
```

See [the API guide](../engine/README.md) for configuration, lifecycle, concurrency,
errors, identities, authenticated joining and message delivery. A successful Send
means enqueue, not receipt. Applications need acknowledgements where required.

To install Park separately for local evaluation:

```sh
go install github.com/TylerPetri/p2p-networking-project/cmd/park-node@FULL_PUSHED_CANDIDATE_SHA
```

The binary is installed into GOBIN or GOPATH/bin; add that directory to PATH to
run `park-node -h`. This command does not launch a public node. Use the bounded
loopback demos before any separately authorized real-network experiment.

From this repository the same external-consumer fixture has two distinct modes:

```sh
# Pre-commit/source check: fresh module, local replace.
python3 scripts/check_external_consumer.py
# Pre-tag remote check: fresh module AND module cache, no local replace.
GOTOOLCHAIN=local python3 scripts/check_external_consumer.py --version FULL_PUSHED_CANDIDATE_SHA
```

The remote mode resolves a commit to its Go pseudo-version using the normal
proxy/checksum policy. It prints the resolved path/version/checksum and confirms
no replacement. Both modes use only the public API, reject an incorrect bootstrap
signing identity, join with the expected Noise/signing identity pair, verify the
message's signer/payload and close/rebind both listeners. `GOWORK=off` prevents a
workspace replacement; `GOTOOLCHAIN=local` prevents a hidden toolchain upgrade.
Remote mode also installs the reference CLI at the resolved version into a
temporary GOBIN and runs `park-node -h`. This is loopback networking after remote
package download, not a cross-host test.

## Platform and compatibility policy

| Surface | Preview scope |
| --- | --- |
| Minimum Go | 1.24.1, tested with automatic toolchain switching disabled |
| Linux | Primary SDK/CLI target; local amd64 verification and hosted CI |
| macOS | SDK/CLI target; actual native CI outcomes must pass, not just cross-compilation |
| Native Windows | Unsupported by current Unix LAN socket dependencies; no native build promise |
| WSL2 | Linux environment; owner-reported device tests do not prove native Windows support |
| Other OS/architectures | Not claimed; no exhaustive build/runtime matrix |

CI runs minimum/current Go on Linux and macOS: vet, full ordinary/race suites,
boundaries, invitation vectors, external consumer, echo and Park. The primary
Linux job retains stress/fuzz/adversarial/process/history checks. A push-only
remote job tests the pushed commit from a fresh consumer at the minimum Go version.
Actual run URLs/results are recorded in the SDK PR; defining a matrix is not a
claim that its jobs passed.

The Go module changes from the former local `p2p-park` path; update consumer
imports. Exported API signatures are unchanged. Preview releases are v0
prereleases: source/behavior compatibility is not a stable-v1 promise, and changes
need release notes/migration instructions. Pin a reviewed version or commit.

Core wire remains exactly `p2p-engine/0.1.0`, invitation v1 canonical bytes and
identity-file v1 remain unchanged. Cryptographic `p2p-park/...` domain strings and
Park's `p2p-park.0.3.0` namespace intentionally remain. There is no protocol
negotiation or rolling-upgrade guarantee; an SDK tag does not change these wire
contracts. See [API_VERSION_POLICY.md](API_VERSION_POLICY.md).

## Evidence and release gates

The maintainer reports successful multi-host, multi-device connections. OSes,
network paths, counts and direct/relay details were not supplied; this is
**owner-reported evidence**, not an independently reproduced matrix or proof of
arbitrary Internet reachability. Automated evidence here is Linux/CI and
loopback; preserve [the cross-host plan](CROSS_HOST_TEST_PLAN.md) and record exact
future runs in its results template. There is no automatic NAT traversal.

Before approving a tag, the owner must:

1. Review/merge cleanup PR #8 first; retarget the stacked SDK PR to main and
   recheck its SDK-only diff and current CI. Preserve ancestry; do not tag a
   partial cleanup or stale candidate.
2. Select the exact final main commit, require passing Linux/macOS minimum/current
   jobs, stress/fuzz/security checks and clean remote resolution at that commit.
3. Confirm release notes accurately describe experimental API/platform/security
   limits and the unchanged evaluation license. Obtain professional legal advice
   if broader SDK distribution/use rights are wanted; no such grant is made here.
4. Explicitly approve creating and pushing the exact preview tag. No tag,
   deployment, license change or separate module publication is part of this PR.

Only AFTER that explicit approval, the owner can run the following in a clean
checkout. Substitute the exact reviewed main SHA, and stop on any failed check:

```sh
git fetch origin main --tags
P2P_RELEASE_COMMIT=FULL_APPROVED_MAIN_SHA
P2P_PREVIEW_TAG=v0.1.0-preview.1
test -z "$(git status --porcelain)"
test "$(git rev-parse origin/main)" = "$P2P_RELEASE_COMMIT"
# Must print no existing tag. If it exists, stop; never move/overwrite it.
git ls-remote --tags origin "refs/tags/$P2P_PREVIEW_TAG" "refs/tags/$P2P_PREVIEW_TAG^{}"
git switch --detach "$P2P_RELEASE_COMMIT"
GOTOOLCHAIN=local python3 scripts/check_external_consumer.py --version "$P2P_RELEASE_COMMIT"
# Reconfirm the release CI is green for this exact commit before continuing.
git tag -a "$P2P_PREVIEW_TAG" "$P2P_RELEASE_COMMIT" -m 'Experimental Go engine SDK preview; existing evaluation license'
git push origin "refs/tags/$P2P_PREVIEW_TAG"
```

Check command exits and tag absence manually before proceeding; do not blindly
paste past a failure. The tag belongs at the repository root (not `engine/v...`)
because engine is a package in the root module. Do not force-update a published
tag. If a release is defective, prepare a new reviewed preview version.

## Post-tag verification — separate, not executed by this work

After the approved tag is actually visible remotely:

```sh
git fetch origin --tags
test "$(git rev-parse "$P2P_PREVIEW_TAG^{commit}")" = "$P2P_RELEASE_COMMIT"
GOTOOLCHAIN=local python3 scripts/check_external_consumer.py --version "$P2P_PREVIEW_TAG"
# In a fresh external module:
go mod init example.com/tagged-sdk-evaluation
go get "github.com/TylerPetri/p2p-networking-project/engine@$P2P_PREVIEW_TAG"
go list -m -json github.com/TylerPetri/p2p-networking-project
go install "github.com/TylerPetri/p2p-networking-project/cmd/park-node@$P2P_PREVIEW_TAG"
```

Require the exact tagged version and no Replace field, successful signed delivery
and shutdown, and successful CLI installation. Record proxy/checksum availability,
resolved module sums, toolchain, OS and run URLs. A proxy propagation delay must
be reported/retried normally, not hidden with a local replace or disabled checksum
verification. Pre-tag pseudo-version success is not post-tag success.

Experimental preview only: no independent security audit, formal verification,
production-readiness guarantee or new license grant. Sybil/resource exhaustion,
unsigned invitation-source trust, volatile replay state, DHT freshness/authority,
application authorization and untrusted-output limitations remain documented in
[SECURITY.md](../SECURITY.md).
