# Package dependency map (Phase 1)

Direct project-package imports, obtained with `go list -f` on this branch.
Standard-library and external imports are omitted for readability. Tests may
import legacy wire aliases to exercise compatibility; this map is production code.

| Package | Direct project dependencies |
| --- | --- |
| `engine` | `internal/netx`, `internal/p2p`, `internal/wire` |
| `internal/p2p` | `internal/crypto/noiseconn`, `internal/dht`, `internal/netx`, `internal/telemetry`, `internal/wire` |
| `internal/dht` | `internal/appdata`, `internal/wire` |
| `internal/wire` |  |
| `internal/proto` | `internal/wire` |
| `internal/park-node` | `internal/app/grants`, `internal/app/points`, `internal/app/quiz`, `internal/crypto/channel`, `internal/discovery`, `internal/netx`, `internal/p2p`, `internal/paths`, `internal/proto`, `internal/storage/grantsbolt`, `internal/telemetry`, `internal/uiutil` |
| `internal/discovery` | `internal/appdata`, `internal/netx`, `internal/p2p` |
| `internal/bootstrap` | `internal/discovery`, `internal/netx`, `internal/p2p` |
| `cmd/park-node` | `internal/netx`, `internal/park-node` |
| `examples/echo` | `engine` |

The production engine closure additionally contains `internal/crypto/noiseconn`,
`internal/telemetry` and `internal/appdata` (only the optional DHT routing-store
path helper). It contains no game schemas, quiz/points/grants, CLI, channel UI or
BoltDB. `scripts/check_engine_boundary.py` enforces that boundary in CI for both
engine and standalone example. The internal `appdata` default remains for legacy
Park; the engine disables implicit persistence and uses an explicit store path.
No second module is necessary while the facade and legacy adapter share the core.
See [architecture decisions](ENGINE_ARCHITECTURE.md) for future module extraction.
