# Third-party software and redistribution notices

Tyler Petri retains the rights asserted in [LICENSE.txt](LICENSE.txt) for original
P2P-Park code. That proprietary notice does not replace third-party copyright,
license, patent or trademark terms. The notices copied below apply to their
respective upstream components, not to the original project as a whole. No
open-source license is being applied to P2P-Park.

## Components used by the application

Checked with Go 1.27.3 on Linux/amd64 using:

```sh
go list -m all
go list -deps -f '{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}' ./cmd/park-node
```

| Component | Version checked | Upstream terms / notice copies |
| --- | --- | --- |
| `github.com/flynn/noise` | v1.1.0 | BSD 3-Clause; Prime Directive copyright and Flynn trademark notice; [LICENSE](third_party/licenses/flynn-noise.LICENSE) |
| `go.etcd.io/bbolt` | v1.3.10 | MIT; Ben Johnson copyright; [LICENSE](third_party/licenses/bbolt.LICENSE) |
| `golang.org/x/crypto` | v0.45.0 | BSD 3-Clause; [LICENSE](third_party/licenses/golang-x-crypto.LICENSE), [PATENTS](third_party/licenses/golang-x-crypto.PATENTS) |
| `golang.org/x/sys` | v0.38.0 | BSD 3-Clause; [LICENSE](third_party/licenses/golang-x-sys.LICENSE), [PATENTS](third_party/licenses/golang-x-sys.PATENTS) |
| Go standard library/runtime | Go 1.27.3 | BSD 3-Clause; [LICENSE](third_party/licenses/go.LICENSE), [PATENTS](third_party/licenses/go.PATENTS) |

License/notice texts were copied unchanged from the pinned module cache and the
Go distribution used for verification. Runtime modules are imported, not vendored
here. `go list -m all` also lists upstream test/development dependencies; that list
is not the set of components linked into `park-node`. The application's own tests
currently import no additional third-party modules beyond the runtime list.
GitHub Actions and the separately installed Gitleaks executable are external
verification tools, not code included in the application distribution. Recheck
this inventory if dependencies, vendoring, tool bundles or build options change.

## What these terms require

- **BSD 3-Clause:** retain the copyright, conditions and disclaimer when
  redistributing upstream source. For binaries containing the component, reproduce
  them in accompanying documentation/materials. Do not use upstream names or
  contributor names to imply endorsement without permission. Flynn also identifies
  its trademark separately.
- **MIT:** include the copyright and permission notice in copies or substantial
  portions of the upstream software, including distributions containing it.
- The Go patent grants have their own scope and patent-litigation termination
  conditions; their texts are preserved alongside the licenses.
- These permissive terms allow combination with proprietary application code.
  They do not require applying an open-source license to the original P2P-Park
  source or transferring its ownership. They also do not let P2P-Park's proprietary
  license remove recipients' existing upstream rights.

Publishing original source that only imports these dependencies is different
from copying their source or distributing a linked executable. The repository
currently ships no vendored dependency source or executable release. The notice
files prepare for attribution; they are not a certification of every future
platform/build's distribution compliance.

## Before distributing a binary or dependency bundle

Include these notices with the release, preserve any additional upstream file
notices when vendoring source, and verify the actual build/platform inventory.
The tested Linux build has CGO enabled and `ldd` reports `libc.so.6` and the system
loader. glibc has LGPL terms (plus per-file exceptions/notices); a binary release
must assess applicable notice/license-copy, replaceable-library/relinking and any
bundled-library source obligations. This repository does not bundle glibc. A
static or different-platform build may have a different inventory and obligations.

The tested build did not enable `GOEXPERIMENT=boringcrypto`. Enabling that option
requires reviewing Go's `src/crypto/internal/boring/LICENSE` and its additional
BoringSSL/OpenSSL/ISC notices; the ordinary Go notice alone is not a complete
inventory for that build. No Go distribution or verification-tool bundle is
being released here.

## Original-code evaluation permission

[LICENSE.txt](LICENSE.txt) now expressly permits viewing, cloning, building,
testing and local execution for personal technical evaluation or recruitment/
hiring review. A commercial employer may conduct that review, but receives no
automatic permission for commercial redistribution, production use, product/
service integration or general source modification. Original-code ownership
and all ungranted rights remain reserved.

These conditions govern the Original Materials defined in that notice, not the
upstream components. All upstream license and patent texts listed above remain
unchanged. The experimental protocol is not independently security-audited.

## Custom-license review notes

This is a proposed custom proprietary evaluation permission in PR #1, not a
standard open-source license or a professional legal opinion. Qualified counsel
should review it before reliance for enforcement or commercial licensing,
particularly:

- The boundary between permitted personal/recruitment evaluation and prohibited
  production use, product integration or external redistribution, including
  organizational review and local build outputs.
- Compatibility with mandatory legal rights and GitHub's applicable hosting/fork
  terms; the notice preserves rights recipients independently have.
- The scope of original-code ownership versus third-party rights, and the
  warranty/liability wording under the applicable jurisdiction.
- Any future binary release, bundled-library obligations or separately negotiated
  commercial permissions. No such distribution right is granted by this change.

No jurisdiction, assignment of ownership, broad modification permission or
open-source grant was selected. The rights reservation is limited to original
materials; upstream MIT/BSD permissions continue independently.
