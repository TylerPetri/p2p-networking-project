# Package dependency map (Phases 2–4)

Phase 4 adds a public-API diagnostic consumer; the engine adds no project
dependency edges or external libraries. See
[the invitation/API review](PHASE3_API_REVIEW.md).

Production imports measured with `go list -f`; external/standard-library imports
are omitted. [Phase 2 review](PHASE2_ENGINE_ADAPTER.md) records the before/after.

| Package | Direct project dependencies |
| --- | --- |
| `engine` | `internal/discovery`, `internal/netx`, `internal/p2p`, `internal/wire` |
| `internal/p2p` | `internal/crypto/noiseconn`, `internal/dht`, `internal/netx`, `internal/telemetry`, `internal/wire` |
| `internal/dht` | `internal/appdata`, `internal/wire` |
| `internal/wire` |  |
| `internal/proto` | `internal/gameproto`, `internal/wire` |
| `internal/gameproto` |  |
| `internal/park-node` | `engine`, `internal/appdata`, `internal/crypto/channel`, `internal/gameproto`, `internal/telemetry`, `internal/uiutil` |
| `internal/discovery` | `internal/appdata`, `internal/netx`, `internal/p2p` |
| `internal/bootstrap` | `internal/discovery`, `internal/netx`, `internal/p2p` |
| `cmd/park-node` | `internal/park-node` |
| `examples/echo` | `engine` |
| `examples/netcheck` | `engine` |

The Park/CLI/application dependency graph reaches networking **only through
`engine`**. `internal/gameproto` contains game DTOs and canonical encoders without
wire dependencies. Legacy `internal/proto` re-exports these types for compatibility
with core security tests; it is not imported by production game code.

The engine uses the same existing core and optional discovery implementation.
Its dependency closure contains no game schemas/logic, game channel UI or BoltDB.
`internal/appdata` is a local path helper, not a networking capability. Park keeps
its application channel cryptography and data-directory helpers on its own side.

`scripts/check_engine_boundary.py` enforces both directions in CI: the engine's
complete production closure excludes game packages; Park's complete application
closure stops at `engine` and rejects all other routes to networking internals.
No separate module/repository is needed to prove this boundary. A future extraction
can move the public package and its generic internal closure together, with an
explicit import-path/API migration plan; no module is published in this phase.

SDK preview: all project import paths are rooted at `github.com/TylerPetri/p2p-networking-project`.
The root module contains the public `engine` package; no nested module is created.
The Park scoring/grant/BoltDB cleanup is reviewed separately in PR #8.
