# Architecture and verification

Go produces self-contained CLI binaries for macOS/Linux on amd64/arm64.
Kong defines commands and runtime schema discovery; SQLite stores metadata,
encountered sellers and shared request reservations. All seller-write paths prune
observations older than 30 days and keep at most 10,000 sellers; deleting a seller
also deletes its indexed listing links. Public HTML/JSON reads use
bounded HTTP requests and verified website links. The web adapter normalizes
those responses for the application services; mobile-shaped internal request
identifiers are compatibility plumbing, not outgoing mobile API requests.

`cmd/kcli` owns process exit and signals; `internal/cli` owns arguments and
streams; `internal/app` owns use cases; `internal/kleinanzeigen` owns transport
and normalization; `internal/state` owns persistence; `internal/media` confines
downloads; `internal/output` owns envelopes and projection. Media directory handles are
opened component-by-component without following symlinks on macOS/Linux. Writes
and atomic installation stay relative to that handle across network waits;
non-overwrite installation never creates an intermediate destination placeholder. Schemas derive from
the command model rather than a separately maintained command inventory.

Auth/DM internals are retained as tested historical implementation, but are not
registered in the command tree. The release does not initialize keyrings or
inject mobile credentials. Reintroducing authenticated features requires a
separate scope and acceptance change.

Run `make check` for formatting, module integrity, build, vet, tests and docs;
`make lint` adds staticcheck and vulnerability checks; `make cross` builds all
four targets. Tests use synthetic fixtures/fake
transports and never contact Kleinanzeigen automatically. Explicit live
acceptance must be paced, bounded, stop on challenges/429, and keep captures in
ignored `.tmp/`. See [test results](test-results.md) for the actual evidence.

CI and release automation are described in [CONTRIBUTING.md](../CONTRIBUTING.md).
User-story traceability lives in [endpoint coverage](endpoint-coverage.md).
