# Invitation v1 interoperability vectors

All data in `v1.json` is PUBLIC TEST data. No real account, private credential,
production identity or deployment endpoint is present. The reproducible key
material derived by Go tests must never be reused for real nodes.

- `reference_unix` is the fixed test clock, not a scheduled event or production
  clock override. Production ParseInvitation/Encode still use the real clock.
- `json` contains literal JSON bytes when applicable; `uri` contains expected
  bare custom-URI bytes. These were assembled independently of the Go codec.
- `parse_error` is empty for acceptance, otherwise invalid/expired/unsupported.
- `join_scenario` instructs a Go live contract test. The fixture identity and
  network stay fixed, but ephemeral loopback ports replace the literal test ports
  so tests do not collide or contact deployment endpoints.
- Wrong identities and wrong network IDs are syntactically valid discovery data;
  the mismatch must fail during the corresponding authenticated join/preflight.
  Parsing alone does not prove endpoint identity, issuer authenticity or permission.

Run `python3 scripts/check_invitation_vectors.py` and
`go test ./engine -run 'TestInvitationGoldenVectors|TestInvitationFixtureLiveContracts'`.
The Python check performs no network I/O and implements no cryptography. Live
fixtures use only loopback. Corpus compatibility is separate from owner-run
cross-host verification in `docs/CROSS_HOST_TEST_PLAN.md`.
