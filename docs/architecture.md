# Architecture and verification

Go produces self-contained CLI binaries for macOS/Linux on amd64/arm64.
Kong defines commands and runtime schema discovery; SQLite stores metadata,
encountered sellers and shared request reservations. Public HTML/JSON reads use
bounded HTTP requests and verified website links. The web adapter normalizes
those responses for the application services; mobile-shaped internal request
identifiers are compatibility plumbing, not outgoing mobile API requests.

`cmd/kcli` owns process exit and signals; `internal/cli` owns arguments and
streams; `internal/app` owns use cases; `internal/kleinanzeigen` owns transport
and normalization; `internal/state` owns persistence; `internal/media` confines
downloads; `internal/output` owns envelopes and projection. Schemas derive from
the command model rather than a separately maintained command inventory.

Auth/DM internals are retained as tested historical implementation, but are not
registered in the command tree. The release does not initialize keyrings or
inject mobile credentials. Reintroducing authenticated features requires a
separate scope and acceptance change.

Run `make check` for formatting, vet, tests, staticcheck, vulnerability and docs
checks; `make cross` for all four targets. Tests use synthetic fixtures/fake
transports and never contact Kleinanzeigen automatically. Explicit live
acceptance must be paced, bounded, stop on challenges/429, and keep captures in
ignored `.tmp/`. See [test results](test-results.md) for the actual evidence.

CI and release automation are described in [CONTRIBUTING.md](../CONTRIBUTING.md).
User-story traceability lives in [endpoint coverage](endpoint-coverage.md).
