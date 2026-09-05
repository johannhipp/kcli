# kcli implementation plan

Status: offline implementation complete and merged; release pending Phase 0 gates
Plan date: 3 September 2026
Target release: `0.1.0`
Authoritative product boundary: [`v0.1-scope.md`](v0.1-scope.md)

> **Current status (5 September 2026):** the offline code for phases 1–8 and
> hardening is implemented, tested against redacted fixtures and fake servers,
> and merged to `main`. The `0.1.0` release is still gated on the Phase 0
> external items in [release-blockers.md](release-blockers.md).

## Outcome and feasibility

Build `kcli` as a single Go binary. The first release will expose anonymous
Kleinanzeigen discovery and listing inspection plus explicitly authenticated,
human-confirmed direct messaging. The same typed application operations will
serve the CLI today and a possible daemon or MCP adapter later.

The scoped release is achievable, with four qualifications that must remain
visible:

1. The mobile API is private, unsupported, and protected by a distribution-level
   Basic credential. Compatibility and permission can change independently of
   kcli. The release must fail closed on access controls rather than treating
   them as an obstacle to route around.
2. The pinned Python reference uses browser TLS impersonation. A standard Go
   client probe from the current development network received an explicit
   temporary IP-range block on 2 September 2026, so it did not isolate whether
   Go's TLS fingerprint is accepted. Transport compatibility is therefore the
   first release gate, not an assumption buried in implementation.
3. Auth, inbox, and message calls are source-proven but have not been exercised
   here with a dedicated account. Their response shapes, Auth0 issuer, unread
   side effects, warning behavior, and timeout reconciliation must be proven
   before those stories can be marked complete.
4. Seller-name search is intentionally local and best-effort. There is no known
   global user directory. `seller get` by ID or profile URL can only return a
   seller already encountered through listing data until another public read
   endpoint is verified.

These qualifications do not require a different product. They determine
whether a build may be called `0.1.0`. If the fixed mobile transport cannot be
made reliable without bypassing a block or challenge, the correct outcome is to
stop the release and revisit the integration—not to add evasion.

Kleinanzeigen's current [terms of
use](https://themen.kleinanzeigen.de/nutzungsbedingungen/) explicitly require
written consent for crawlers, scrapers, and other automated collection, and its
[IP-block guidance](https://themen.kleinanzeigen.de/ip-eingeschraenkt/) tells a
blocked user to wait. Accordingly, distribution and automated live testing are
release-blocked until written permission covers the intended use. A local risk
acknowledgement prevents accidental tests but is not a substitute for that
permission.

## Decisions fixed by this plan

- Language: Go, with Go 1.26 as the minimum toolchain and Go 1.27 also tested.
- Deliverable: one `kcli` executable; no required Python, browser runtime,
  Node.js, JVM, CGO, or resident service.
- CLI parser: [`alecthomas/kong`](https://github.com/alecthomas/kong), using
  nested Go structs, tags, validation hooks, and `Run` methods.
- API model: reviewed operations only; no generic private-API request command.
- State: one SQLite database per named profile, with embedded migrations and
  generated typed queries.
- Secrets: OS keyring by default; refresh-token environment injection is not a
  v0.1 feature because rotation behavior is not yet proven.
- Output: TTY-aware tables, JSON, NDJSON, redacted raw JSON, and field
  projection. External `jq` remains composable and avoids an embedded evaluator.
- Messaging: exact dry-run plan, short-lived confirmation ID, one claimed
  execution, one send attempt, and an explicit ambiguous-outcome state.
- DM activity: finite polling and foreground watching over polling; no claim of
  remote push.
- Later integration: daemon and MCP reuse the core but are not on the `0.1.0`
  critical path.

## Why Go rather than Rust

Both languages can produce a portable single binary. Rust has excellent
building blocks—[`clap` derive](https://docs.rs/clap/latest/clap/_derive/),
[`oauth2`](https://docs.rs/oauth2/latest/oauth2/), `reqwest`, `rusqlite`, and
Serde—but Go is the smaller implementation for this particular product.

| Criterion | Go plan | Rust alternative | Decision |
|---|---|---|---|
| Command definitions | Kong maps nested structs and tags directly to commands | clap derive maps structs/enums similarly | Tie |
| Private mobile transport | Maintained `tls-client` offers a `net/http`-like client and fixed browser profiles | Ordinary reqwest is ergonomic, but a separately maintained impersonating client would still be needed | Go has the more direct known candidate |
| Poll/watch concurrency | Contexts, goroutines, channels, and timers are in the standard library | Tokio is capable but adds an async runtime and async/sync boundary decisions | Go is less code |
| SQLite distribution | `modernc.org/sqlite` is pure Go and cross-compiles without CGO | bundled rusqlite is mature but compiles native SQLite | Go simplifies release builds |
| Schema reuse | Reflection over the same Go request structs can produce JSON Schema; the official MCP Go SDK uses the same schema package | Rust schema derives are good but add another derive layer | Slight Go advantage |
| Cross-platform binary | Straightforward `GOOS`/`GOARCH` matrix | Cargo cross builds are workable but need more target setup | Go advantage |
| Team maintenance | Small interfaces and explicit structs; no lifetime or async trait concerns | Stronger compile-time modeling, with more glue for this I/O-heavy CLI | Go advantage for minimum code |

If standard Go TLS is accepted, use it. Otherwise, kcli may use exactly one
fixed profile shown by phase-0 evidence to be compatible with the reference
client. That is an explicit product/policy decision, not a fingerprint-rotation
strategy. If neither is acceptable, stop; changing to Rust would not change the
permission or anti-automation constraint.

## Version and dependency policy

Use `go 1.26` in `go.mod`; test the latest patch releases of Go 1.26 and 1.27.
Go supports the two most recent major releases, so the minimum must be reviewed
whenever a new Go release lands. Commit `go.mod` and `go.sum`. Pin tools using the
Go tool directive where supported, and let dependency update automation open
reviewable pull requests rather than tracking `latest` at build time.

The versions below were resolved on the plan date. They are starting pins, not
permission to upgrade without tests.

### Runtime dependencies

| Dependency | Candidate pin | Purpose | Why it reduces project code | Exit condition |
|---|---:|---|---|---|
| [`alecthomas/kong`](https://github.com/alecthomas/kong) | `v1.16.1` | Nested commands, flags, enums, validation, contextual help, dependency injection | One tagged struct is both command declaration and parsed value | Replace only if schema/completion integration needs a parallel command model |
| [`jotaen/kong-completion`](https://pkg.go.dev/github.com/jotaen/kong-completion) | `v0.0.14` | Bash, zsh, and fish completion derived from Kong | Avoids three handwritten completion trees | Completion must take a fast path before config/state initialization |
| [`bogdanfinn/tls-client`](https://github.com/bogdanfinn/tls-client) | `v1.16.0` | Fixed Chrome-like TLS, HTTP/2, and header profile for mobile API calls | Provides the compatibility layer used by the reference client without a browser process | Keep only if the phase-0 matrix proves standard Go transport is insufficient and this fixed profile is permitted and stable |
| *(none for OAuth/OIDC)* | — | PKCE, the two `/oauth/token` grants, and ID-token claim checks are written against the standard library | The verified contract is one JSON `POST` per grant; `x/oauth2` sends form bodies, auto-detects auth style with a second request, and its `TokenSource` cannot persist a rotated refresh token under the lease. The ID token arrives over the TLS back channel, so OIDC Core §3.1.3.7 rule 6 permits TLS validation instead of a JWKS signature check | Re-add a library only if phase 0 shows a grant or claim shape the ~150 hand-written lines cannot express |
| [`zalando/go-keyring`](https://github.com/zalando/go-keyring) | `v0.2.8` | macOS Keychain, Linux Secret Service, and Windows Credential Manager | One cross-platform `SecretStore` backend | Never silently fall back to plaintext tokens |
| [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) | `v1.58.0` | CGO-free SQLite driver | Keeps state transactional while preserving cross-compilation | Check binary size, startup time, and all six release targets in phase 1 |
| [`pressly/goose/v3`](https://github.com/pressly/goose) | `v3.28.0` | Embedded transactional SQL migrations | Avoids a custom migration ledger and partial-upgrade logic | Use the library API only; do not ship its CLI |
| [`google/jsonschema-go`](https://github.com/google/jsonschema-go) | `v0.4.3` | Infer and validate JSON Schema from Go structs | One schema implementation can back `--input`, `schema show`, and later MCP tools | Add golden schemas because it is still pre-1.0 |
| [`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term) | `v0.45.0` | Reliable TTY detection and non-echoed redirect paste | Avoids platform-specific terminal code | No command may prompt when stdin or stdout is non-TTY |

Use the standard library for configuration JSON, logging (`log/slog`), table
formatting (`text/tabwriter`), cryptography, URL parsing, HTTP test servers,
signals, browser opening, field projection, and filesystem paths. A 20-line `openURL` adapter for
`open`, `xdg-open`, and `rundll32` is preferable to adding an effectively
unmaintained browser-opening dependency.

The TLS dependency is intentionally conditional and is the largest transitive
cost. Use one fixed, documented profile. Do not rotate fingerprints, ship proxy
support, solve challenges, or expose the library's low-level knobs through the
CLI. A `403`, bot challenge, or explicit network block is terminal for that
operation.

### Build and test tools

| Tool/library | Candidate pin | Role |
|---|---:|---|
| [`sqlc`](https://docs.sqlc.dev/en/latest/tutorials/getting-started-sqlite.html) | `v1.31.1` | Generate typed SQLite query methods from reviewed SQL |
| [`rogpeppe/go-internal/testscript`](https://github.com/rogpeppe/go-internal/tree/master/testscript) | `v1.16.0` | Exercise the compiled CLI, streams, files, exit codes, and environment in txtar fixtures |
| [`google/go-cmp`](https://github.com/google/go-cmp) | `v0.7.0` | Readable structural diffs in parser and normalization tests |
| [`GoReleaser`](https://goreleaser.com/) | pin in CI | Reproducible archives, checksums, SBOMs, Homebrew metadata, and release snapshots |
| [`govulncheck`](https://go.dev/security/vuln/) | pin in CI | Reachability-aware Go vulnerability scanning |
| `staticcheck` | pin in CI | Static analysis beyond `go vet` |

Generated sqlc files are committed so source releases build without installing
sqlc. CI regenerates them and fails on a diff. Do not introduce Viper, a DI
container, an ORM, a logging framework, an HTTP framework, or a second schema
library unless a measured requirement appears.

## Architecture

Review command (the quoted review brief is abbreviated here):

```text
argv / JSON stdin
       |
       v
Kong CLI adapter ---- schema/catalog adapter ---- later MCP adapter
       |                         |
       +----------- typed operation inputs
                               |
                               v
                       application services
              / discovery  / listings / auth / messages / sync
             /             |                    |
mobile transport      SQLite state         OS secret store
  main/gateway/          and cache          OAuth entries
  login/CDN hosts
```

The application layer receives typed inputs and returns typed results. It does
not know whether the caller is a terminal, a JSON stdin document, or a future
MCP tool. The CLI adapter owns argument parsing and output selection. The API
adapter owns private wire shapes. The state package owns SQL transactions. Only
three boundaries need interfaces for deterministic tests:

- `Transport.Do(Request) (Response, error)`, using kcli-owned request/response
  types so standard `net/http` and `tls-client`/`fhttp` do not leak incompatible
  types into the application;
- `SecretStore.Get/Set/Delete`;
- `Clock.Now/Sleep`;

Use concrete types everywhere else. Do not create one interface per repository
or one package per struct. This keeps the system testable without turning it
into an abstraction exercise.

### Proposed repository layout

```text
cmd/kcli/main.go                         tiny process entry point
internal/cli/root.go                     Kong tree and global flags
internal/cli/search.go                   command structs and Run methods
internal/cli/listing.go
internal/cli/seller.go
internal/cli/auth.go
internal/cli/dm.go
internal/cli/schema.go
internal/cli/config.go
internal/cli/completion.go
internal/app/app.go                      dependency assembly and use cases
internal/app/catalog.go                  operation metadata for schema/MCP reuse
internal/domain/input.go                 named operation input structs
internal/domain/error.go                 typed errors and exit mapping
internal/domain/model.go                 stable public inputs, outputs, and events
internal/domain/event.go                 event, cursor, and sync types
internal/kleinanzeigen/client.go          host routing and operation classes
internal/kleinanzeigen/transport.go       fixed-profile HTTP adapter
internal/kleinanzeigen/headers.go         app/user headers and redaction
internal/kleinanzeigen/decode.go          namespace/wrapper normalization
internal/kleinanzeigen/search.go
internal/kleinanzeigen/listing.go
internal/kleinanzeigen/auth.go
internal/kleinanzeigen/messages.go
internal/state/db.go                     connection pragmas and transactions
internal/state/migrations/*.sql           embedded goose migrations
internal/state/queries/*.sql              sqlc source
internal/state/sqlc/*.go                  committed generated code
internal/secret/keyring.go                keyring implementation; build-tagged test fake
internal/output/encoder.go                JSON/NDJSON/table/raw and --fields
internal/schema/catalog.go                JSON schemas and live filter overlay
internal/dmsync/poll.go                   reconciliation and event creation
internal/dmsync/watch.go                  loop, backoff, signals
internal/media/download.go                constrained image downloading
internal/platform/open.go                 browser opening
internal/platform/paths.go                config/state/cache locations
internal/buildinfo/buildinfo.go            version, commit, date, app config
testdata/api/*.json                       minimized redacted wire fixtures
testdata/script/*.txtar                    end-to-end CLI scenarios
docs/                                    contracts and this plan
scripts/check_docs.py                     documentation traceability
sqlc.yaml
.goreleaser.yaml
go.mod
go.sum
```

Files should remain cohesive and normally below roughly 400 lines. Split by
upstream resource or behavior, not by arbitrary `util` buckets.

## Public type and schema model

Every operation has a named input and output Go type. JSON field names are the
canonical external contract; Kong tags map convenience flags into the same
input types. Each leaf command implements `Describe() OperationMeta`; a small
walker over the Kong model builds the operation catalog. The metadata records:

- command path and one-sentence purpose;
- input and output Go types;
- schema name and version;
- auth requirement;
- side-effect class (`none`, `local`, `account-state`, `external-message`);
- confirmation requirement;
- evidence (`live`, `source`, `provisional`, `local`, or `gap`);
- safe default limit and hard maximum;
- examples and story IDs.

`kcli schema list/show` reads this derived catalog. Phase 1 must prove that the
Kong node exposes each command target reliably; if it does not, generate the
catalog from the CLI structs at build time rather than maintain a handwritten
third command tree.

### Common envelopes

Use operation-specific schemas with a consistent envelope shape:

```json
{
  "schema": "kcli.search-results/v1",
  "request_id": "req_…",
  "source": "mobile-api",
  "observed_at": "2026-09-02T12:00:00Z",
  "completeness": "complete",
  "data": [],
  "page": {"number": 0, "size": 25, "fetched": 25, "returned": 20},
  "next": null,
  "warnings": []
}
```

Rules:

- `schema`, `request_id`, `source`, and `observed_at` are always present.
- `request_id` is generated locally by kcli for one invocation; it is not an
  upstream correlation identifier.
- Resource IDs are strings even if the service currently emits numbers.
- Omitted and empty are distinct when the upstream distinction matters.
- Amounts use the exact upstream decimal string in the public contract; an
  optional integer-cent field is added only when that conversion is exact.
  Never use binary floating point for money.
- Times normalize to RFC 3339 while retaining the original value in raw output.
- Unknown enums are represented as strings plus a warning, not rejected while
  decoding.
- `raw` is present only when requested and is a redacted `json.RawMessage` or
  normalized JSON value. Request/response headers are never part of raw output.
- Lists expose fetched, excluded, deduplicated, returned, total-if-known, and
  truncation fields so client-side behavior is observable.

Structured failures use `kcli.error/v1` with `code`, `message`, `retryable`,
`request_id`, optional `retry_after`, and safe typed `details`. Listing and
message prose never becomes the error message.

### Input precedence

JSON is the canonical bulk input and structured output format. YAML is deferred;
an external converter can consume JSON without expanding the v0.1 dependency
or schema surface.

- Command `Validate()` methods reject `--input FILE|-` when any convenience
  builder field was explicitly set. Do not rely on a large Kong `xor` tag group;
  presence tracking is clearer for defaulted fields.
- Unknown JSON fields fail validation.
- A versioned `schema` field is required for stored/replayed search specs and
  mutation plans, but an interactive flag invocation gets the current version.
- `-` may be consumed by only one flag. For example, `--input -` and
  `--message-file -` cannot coexist.
- Read stdin to a bounded buffer: 1 MiB for command documents and a separate,
  much smaller message limit after the authenticated contract test establishes
  the browser-compatible maximum.
- Environment variables supply secrets and path/config overrides only. They do
  not silently override explicit operation fields.

## Configuration, profiles, and local state

### Paths

Use platform conventions with standard-library helpers and a small platform
adapter:

- configuration: user configuration directory, `kcli/config.json`;
- state: XDG state home on Linux and the platform application-support location
  elsewhere, `kcli/profiles/<safe-name>/state.db`;
- cache: user cache directory, `kcli/profiles/<safe-name>/`;
- runtime socket: only for the later daemon, never v0.1.

Profile names match `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`. Canonicalize and join
paths only after validation. Config writes use a sibling temporary file,
`fsync`, and atomic rename. Directories are user-only; state/config files are
mode `0600` where permissions exist.

Precedence is explicit flags, operation-specific environment, profile config,
global config, then compiled defaults. `config list` redacts secrets and reports
the source of each effective value.

### SQLite schema

Keep one database per profile to make account and cursor isolation obvious.
The initial migrations create:

| Table | Important columns | Purpose |
|---|---|---|
| `meta` | `key`, `value` | profile UUID, cursor generation, schema versions |
| `rate_slots` | `host`, `next_eligible_at_ms` | atomic cross-process request reservations |
| `category_snapshots` | `id`, `path`, `label`, `parent_id`, `raw_json`, `observed_at` | refreshed category tree |
| `filter_snapshots` | `category_id`, `key`, `type`, `search_param`, `search_style`, `raw_json`, `observed_at`, `proof` | live dynamic-filter definitions |
| `location_cache` | `query_key`, `result_json`, `observed_at`, `expires_at` | bounded location resolution cache |
| `sellers` | `seller_id`, `folded_name`, public fields, `source`, `completeness`, `observed_at` | locally encountered seller index |
| `seller_listings` | `seller_id`, `listing_id`, title/status/url snapshot, `observed_at` | known public seller listings |
| `conversations` | account hash, conversation/listing/counterparty summary, remote fingerprint, timestamps | DM change detection and context |
| `messages` | account/conversation, remote ID or fingerprint, direction, received time, content digest, observed time | deduplication without retaining full message bodies |
| `events` | monotonic sequence, stable event ID, type, resource IDs, bounded conversation preview, observed time | restart-safe poll/watch spool |
| `cursor_heads` | account hash, generation, acknowledged sequence | one default local resume head per profile |
| `confirmation_plans` | random ID, account/target/message digest, state/stage, expiry, outcome | two-phase mutation safety without retaining the body |
| `leases` | name, owner, expiry | refresh and synchronization mutual exclusion |

Use foreign keys, a 5-second busy timeout, and explicit transactions. Phase 1
tests WAL and `synchronous=FULL` under abrupt termination and simultaneous CLI
processes. Enable WAL only if those tests pass on macOS, Linux, and Windows;
otherwise retain the default journal and accept more serialization in v0.1.

Migrations are forward-only for released databases. v0.1 creates a fresh
database and needs no pre-migration backup path. Add a bounded backup procedure
before the first post-release migration. On corruption, preserve the damaged
file, create a new generation, and emit `system.resync_required`; never silently
discard continuity.

### Sensitive local data

Store refresh token, access token, email, and expiry metadata as separate
keyring entries rather than one JSON blob. Never persist the ID token. Windows
Credential Manager caps one credential blob at 2,560 bytes
(`CRED_MAX_CREDENTIAL_BLOB_SIZE`); a value above that cap is split into
numbered sibling entries and rejoined on read, so a long Auth0 JWT cannot make
login impossible. Tests cover a synthetic 4 KiB token round-trip and a partial
chunk set, which reads as missing rather than as a truncated token. Only the
refresh token is required across a restart; a missing/expired access token is
minted under the refresh lease.

v0.1 deliberately does not add application-level database encryption. The
private mode-`0600` database retains public seller snapshots, DM summary
previews, hashes, and context, but not complete message bodies or confirmation
text. `dm get` reads full history from the service for that invocation. The
confirming command must supply the exact message again and the stored digest
must match. This threat model and limitation must be visible in privacy docs;
full encrypted offline DM archives are not a v0.1 goal.

Default event/preview retention is seven days with a bounded maximum count per
profile. Encountered sellers/listings default to 30 days and 10,000 sellers,
whichever bound is reached first; output reports the index horizon. Context
snapshots needed to understand an active conversation can outlive individual
events but are removed on logout unless the user explicitly keeps local state.
A later purge command requires its own documented dry run; it is not necessary
for the first functional slice.

Refresh-token environment injection is excluded until phase 0 proves rotation
and token-family behavior. Testscript uses a build-tagged file-backed fake
secret store that is absent from release binaries; production never falls back
from keyring to plaintext.

## Mobile transport and request policy

### Host separation

Create four clients with separately allowed base URLs, timeouts, auth/header
policies, response caps, and redirect rules:

| Client | Hosts | Credentials | Redirects |
|---|---|---|---|
| Main API | `api.kleinanzeigen.de` | distribution Basic; optional user headers | none |
| Gateway | `gateway.kleinanzeigen.de` | user Bearer token | none |
| Login | `login.kleinanzeigen.de` only | OAuth client configuration; no Basic | only exact allowlisted OAuth redirects |
| Media | exact HTTPS URLs returned by a listing, constrained to observed CDN policy | none | each hop revalidated |

Login and media use standard `net/http`. Main API and gateway use standard
`net/http` if phase 0 proves it works, otherwise the one accepted fixed
`tls-client` profile. Phase 0 must separately prove that the login host and
the observed image CDN accept standard Go TLS; the OAuth code cannot be
silently wired through `tls-client`'s incompatible `fhttp` types.

Base URLs are injectable only through unexported test constructors or an
explicit development build tag. Production configuration cannot redirect bearer
tokens to arbitrary hosts.

The distribution Basic values and OAuth client configuration are public-client
material but still rotate and must not be committed under repository policy.
Official builds receive them through release secrets and `-ldflags`; development
can use `KLEINANZEIGEN_BASIC_USER` and `KLEINANZEIGEN_BASIC_PW`. `doctor` reports
only present/missing and source. No log, crash, schema, or raw output contains
them.

Generate one stable install value per profile and store it as non-secret state.
The observed format is `<uuid><creation-unix-ms>`; both parts remain fixed for
that profile unless fresh evidence shows the official client rotates them.
Headers match the verified mobile contract. The app/user version is build
metadata, not a user-controlled arbitrary header.

### Request classes

Every endpoint is assigned a compile-time operation class:

| Class | Examples | Automatic retry |
|---|---|---|
| `stable-read` | categories, locations, metadata, search | At most two retries for connection-before-response, `429`, `500`, or `503`; honor `Retry-After` |
| `volatile-read` | listing detail | Same transient policy, but never retry `404` |
| `state-touching-read` | conversation `PUT` | No automatic retry; the caller can explicitly read again |
| `account-mutation` | mark read, logout local state | No blind retry; command-specific result |
| `external-create` | create conversation | Exactly one request, then reconciliation after ambiguity |
| `external-send` | reply/first text | Exactly one request; never automatically retry |

Use context deadlines: 25 seconds for JSON API requests, 30 seconds for OAuth,
and configurable media timeout with a hard 60-second ceiling. Cap JSON bodies at
10 MiB and diagnostic error bodies at a redacted 4 KiB. Stream image bodies with
a default 25 MiB maximum rather than buffering them. Global `--timeout` may
lower an operation deadline but never raise these ceilings.

### Rate limiting across processes

An in-memory limiter is insufficient because agents invoke many short-lived CLI
processes. Reserve a request time atomically in SQLite:

1. Start a short transaction on the host's `rate_slots` row.
2. Set the reservation to `max(now, next_eligible) + 2.5s + random jitter` and
   return the prior eligible time.
3. Commit immediately, then sleep until the reserved time.
4. If the queue is already more than 30 seconds ahead for a one-shot command,
   fail with `rate_limited_local` (exit `6`, `retryable: true`, `retry_after`
   set to the queue delay) instead of silently hanging.

A process that dies after reservation only creates a harmless gap. Each retry
must obtain another reservation. `Retry-After` can move the host slot forward.
Polling cycles have their separate 30-second minimum.

Do not retry `401` or `403` with changed fingerprints or credentials. Do not use
proxies, IP rotation, CAPTCHA services, or randomized ClientHello profiles.

### Response normalization and raw retention

Decode the response once into both a bounded raw JSON buffer and typed wire
structs. Central helpers must:

- locate known namespace keys by exact URI/local name rather than position;
- recursively unwrap `{ "value": ... }` containers;
- normalize object-or-array singleton variants;
- parse numeric strings without losing the original value;
- HTML-unescape documented text fields after JSON decoding;
- retain unknown fields in requested raw output and fixture captures;
- reject duplicate JSON keys in security-sensitive OAuth/confirmation input;
- turn contract drift into a partial result plus warning when safe, or a typed
  `upstream_contract` error when identity/side effects are ambiguous.

Fuzz the unwrap, singleton, ID, URL, cursor, and redaction functions. Never log
full request URLs containing account email, query text, or message content.

## Feature implementation details

### Categories, locations, and filter metadata

`category list/get/search` refreshes the server tree only with `--refresh`, on an
empty cache, or after the documented 24-hour TTL. Persist numeric ID, localized
label, parent, and full slash-separated path. Lookup is exact ID first, then
case-folded exact path, then a bounded ambiguity error listing candidates.

`location resolve` calls the mobile location endpoint, flattens parent-before-
child, and returns all candidates up to `--limit`. Cache the exact normalized
query for seven days. The public website autocomplete used by the reference
client as a fallback is not part of v0.1: it is a second host with a second
fingerprint tried after the first failed, which is exactly the identity-hopping
the transport policy forbids. A mobile location failure is reported as such.

The metadata object key is the query parameter name. Its `search-param` field is
a capability marker such as `optional`, `required`, or `unsupported`—it is not
itself the parameter name. Preserve at least `localized-label`, `type`,
`search-param`, `search-style`, supported values, deprecation/version markers,
and raw JSON.

`filter list/get` classifies each definition:

- `accepted`: serialization style has a passing live contract fixture;
- `provisional`: the service advertises it but its multi-value/range encoding is
  not yet proven;
- `unsupported-upstream`: `search-param` says unsupported;
- `unsupported-client`: a new type/style is preserved but no serializer exists.

The generic `--filter KEY=VALUE` can represent scalar string, decimal, integer,
boolean, enum, date/time, and repeated values. Validate known enums and scalar
types locally. Use repeated query keys for a proven `in` style; do not guess a
comma, pipe, or `MIN..MAX` encoding. Separate advertised min/max keys already fit
`KEY=VALUE`; a single range-valued field needs a structured form only after its
wire encoding is proven. Unknown types fail before the search and point to
`filter get --output raw`.

At the `0.1.0` evidence snapshot, every advertised filter whose `search-param`
is not `unsupported` must be either serialized correctly or explicitly shown to
be unusable by the service. `unsupported-client` is a safe post-release response
to newly introduced metadata, not a way to ship with a known searchable type
missing. This is the enforceable meaning of “all available filters.”

Two different procedures satisfy it. Phase 0 samples a small category set to
prove the wire encoding of each *kind* of filter (type × `search-style`); the
set of kinds is small even though the set of categories is not. The release
snapshot is then a single bounded walk over every category ID in the cached
tree, run once at the request floor with its request count and duration
recorded, whose only purpose is to find a kind the sample did not contain. A
newly found kind either gets a proven serializer before release or the release
waits; the walk never sends searches, and it is not repeated on ordinary use.

### Search

`SearchInputV1` contains query, category reference, location reference, radius,
minimum/maximum exact decimal-euro strings, ad type, picture requirement, sort, ordered repeated
dynamic filters, exclusions, page, page size, pagination mode, and limit.

Validation rules include:

- `page >= 0`, `1 <= page_size <= 25`;
- default one page and 25 returned results;
- `--paginate` defaults to a 100-result bound and rejects limits above 1,000;
- minimum price must not exceed maximum;
- radius and distance sort require a resolved location;
- common flags cannot also appear as dynamic-filter keys;
- dynamic filters require a resolved category and current metadata;
- empty exclusion terms and invalid UTF-8/control characters are rejected.

The price parser accepts the exact decimal grammar proven by phase 0 and never
rounds through binary floating point. The default ad type is `offered`, and the
client sends the observed `OFFERED` wire value explicitly rather than relying on
an undocumented server default.

For pagination, fetch pages serially to respect rate policy, deduplicate by
listing ID, then apply case-folded title/description exclusions. `--limit`
applies after exclusions, so another page may be fetched to fill the bound.
Stop on the bound, known total, an empty page, or two pages with no new IDs.
Expose each stop reason.

Return the canonical resolved `SearchInputV1` in metadata so a caller can store
and rerun it byte-for-byte. Preserve server ordering unless the user chose an
explicit local exclusion; never re-sort silently. Index each encountered seller
and seller/listing link transactionally after a successful decode.

### Listing and media

Accept only a digit-like opaque listing ID or an HTTPS public Kleinanzeigen URL
whose path matches a tested listing form. Strip no arbitrary query path into an
ID; reject user info, fragments used as data, encoded separators, control
characters, and unrelated hosts.

The normalized `ListingV1` includes every field promised in the scope:

- ID, title, description, official URL, category ID/path, ad type, status,
  labels, dates, view count, and contract warnings;
- exact original decimal amount, optional integer cents only when conversion is
  exact, and price type;
- location label, postcode, approximate coordinates, distance, and explicit
  pickup/shipping indicators when present;
- typed known attributes plus an ordered collection of every unknown attribute;
- every picture and media relation, URL, dimensions/size label when returned;
- seller/contact name, user ID, account/poster type, account age, rating, badges,
  and company fields;
- `availability` set to `available`, `reserved`, `expired`, `deleted`,
  `unavailable`, or an unknown upstream value.

A detail `404` returns a structured unavailable resource with exit 4 and no
transient retry. A reserved/expired status returned in a `200` is a successful
partial state with exit 0.

`listing images` lists variants without fetching them. Download accepts only an
index, exact relation, or `all`; never an arbitrary URL. The downloader:

1. re-fetches or accepts the same invocation's listing result;
2. selects exact returned HTTPS URLs;
3. revalidates every redirect against the CDN allow policy and rejects private
   addresses/IP literals;
4. streams through a byte limit, validates image content type and magic prefix,
   and writes to a sibling temporary file;
5. creates predictable `<listing>-<index>-<relation>.<ext>` names with `O_EXCL`;
6. atomically renames on success and removes only its own temporary file on
   failure.

The default output directory is `./kcli-downloads`. An absolute or parent-
escaping path requires the exact path again through
`--allow-outside-cwd PATH`; mismatches fail. Existing files are never replaced
unless `--overwrite` names the exact destination, and symlink parents are
rejected. `listing open` opens only the normalized official URL and returns it
even if desktop opening is unavailable.

### Sellers

Every listing normalization upserts a seller snapshot and link to that listing.
Normalize names for lookup with Unicode NFKC and language-neutral case folding,
but retain the exact public spelling for output.

- `seller get --listing` always performs/uses listing detail and returns source
  `listing`.
- `seller get ID_OR_URL` parses a known seller ID or only profile/company URL
  forms captured in redacted fixtures (including the observed `self-user`
  relation) and looks only in the local index. Every accepted URL grammar is
  listed in help; other Kleinanzeigen URLs are exit 2. URL support remains
  provisional until phase 0 captures at least one real form. A miss says
  `not_in_local_index`; it does not imply the seller does not exist globally.
- `seller search NAME --match exact|contains` searches the folded local index,
  returns a bounded result set, and always includes index size/last observation.
- `seller listings` returns locally linked public listing snapshots and labels
  them `known-only`; it does not call the authenticated “my ads” endpoint for
  another person.

All seller results include source, completeness, observed time, and the command
that can refresh their source listing. Never add a web scrape that claims to
turn this into global username search without changing the scope and evidence.
Expire seller snapshots after the documented 30-day/count bound and expose the
index horizon so results never look fresher or broader than they are.

### Authentication

The CLI never accepts a Kleinanzeigen password. Login is Authorization Code with
PKCE S256 against the verified mobile client configuration and fixed HTTPS
redirect. Because the provider does not allow localhost, the browser cannot
return directly to the process.

TTY flow:

1. Generate a 32-byte verifier, S256 challenge, 32-byte state, and nonce in
   memory; always include the nonce in the authorization request.
2. Print and attempt to open the authorization URL.
3. Explain that the final browser/app page may fail visually and ask the user to
   paste the complete redirect URL.
4. Read it without terminal echo, validate exact scheme/host/path, compare state
   in constant time, reject OAuth errors, and extract one code.
5. Exchange the code once: one JSON `POST` to `/oauth/token` mirroring the
   verified mobile contract, with retries disabled, so nothing can send a
   single-use code twice.
6. Decode the ID token's claims and require exact issuer (including its
   trailing slash), audience equal to the client ID, unexpired `exp`, and a
   nonce equal to the one sent. Obtain email only from those claims. No JWKS
   fetch or signature check is performed: the token was received directly from
   the token endpoint over TLS, which OIDC Core §3.1.3.7 rule 6 accepts in
   place of signature validation, and the email is only used to address
   requests that the service itself validates against the access token. Record
   the observed issuer and audience in `mobile-api.md` after the live test.
7. Resolve the numeric account ID through the authenticated profile endpoint.
8. Store refresh/access/email/expiry in separate OS-keyring entries and only a
   hash/ID in SQLite. Do not persist the ID token.

For a headless terminal, `auth login --no-open --redirect-file -` reads the full
redirect from stdin. Never accept the authorization code in an argv flag because
arguments and shell history leak. A non-TTY login without `--redirect-file`
fails with `interactive_required` rather than hanging.

Refresh 60 seconds before expiry. A SQLite lease ensures only one process uses a
potentially rotating refresh token; contenders re-read the keyring after the
lease holder finishes. Refresh once after a `401`, never after a `403`. On
`invalid_grant`, mark the profile login-required and retain no usable access
token. Implement the refresh exchange directly through the lease so the rotated
token is persisted in the same step that observed it. Phase 0 records whether
rotation/reuse detection is enabled.
`auth status` is local by default; `--check` performs one remote check.

`auth logout --dry-run` previews the local keyring and state effects. Invoking
logout without `--dry-run` deletes local token entries and advances the DM cursor
generation; it needs no external-message confirmation ID. Add Auth0
`/oauth/revoke` to the endpoint gap list and test it separately. Until verified,
logout claims only to clear the local session, not revoke it server-side.

### DM reads and account-state changes

`dm list` pages newest-first with a default 50 and hard 500 conversation bound.
It returns conversation ID, listing context, role, counterparty, unread state,
count, last activity, and preview. Preserve wrapper variants in fixtures.

`dm get` calls the documented state-touching `PUT`, returns oldest-first messages
with raw kind/direction/received time, and stores only listing/counterparty
context and message fingerprints—not bodies. It must not claim to be
side-effect-free. Phase 0 measures whether the call changes unread state.

`dm mark-read` validates all conversation IDs against the signed-in account,
shows a dry run, and makes one bounded call. A definite response updates local
summary state. An ambiguous outcome is reconciled with `dm list`; it is never
blindly repeated.

Listing context is snapshotted beside each conversation when first observed and
updated without erasing prior known values. A later unavailable listing therefore
does not make the conversation unintelligible.

### Message planning, confirmation, and send-once behavior

There is no `--yes`, force, unattended, bulk, template, or scheduled-send path.
`dm reply` and `dm start` use the same state machine.

Dry run performs read-only resolution and returns:

- profile/account pseudonym, conversation and listing IDs;
- counterparty and listing title/status;
- exact message text and UTF-8 byte/rune counts;
- static context warnings and any previously observed platform-warning codes;
- SHA-256 message digest, creation/expiry time, and random confirmation ID;
- exact operation stages that confirmation will attempt.

Store only the digest, never the body. Bind the plan to profile UUID, verified
account subject, target IDs, exact message digest, operation kind, and a
ten-minute expiry. Confirmation supplies the opaque ID **and the same message
source again**; kcli hashes it and rejects any mismatch before network access.
In one transaction, compare all bindings and atomically change `planned` to the
first executing state. A second process can never claim the same plan.

Reply states are:

```text
planned -> executing_send -> sent
                         \-> failed_definite
                         \-> warning_blocked
                         \-> outcome_unknown
planned -> expired
```

Start-conversation states are:

```text
planned -> executing_create -> conversation_created -> executing_send -> sent
                          |                          |                  \-> outcome_unknown
                          |                          \-> failed_definite
                          +-> reconcile_create -> conversation_created
                          |                    \-> outcome_unknown
                          \-> failed_definite
```

Before creating, inspect existing conversations for the same listing. If one
exists, fail with a conflict and instruct the caller to create a reply plan.
The dry-run preview states that creating a conversation may itself become
visible to the seller until phase 0 proves otherwise, and it requires the exact
contact-name value/default proven by the two-account fixture.
After an ambiguous create response, run one bounded inbox reconciliation. If one
new conversation for the listing is uniquely identified and no matching message
exists, record its ID and attempt the text once. If creation remains ambiguous,
stop. After an ambiguous message response, fetch the thread once and compare a
recent outgoing message using NFC normalization, surrounding-whitespace trim,
whitespace collapse, and a narrow timestamp window. Report `sent_reconciled`
only on an unambiguous match; any other result remains `outcome_unknown` for a
human to resolve.

Never reuse an expired, failed, sent, warning-blocked, or unknown plan. A new dry
run for the same target/body uses only inbox summary data and refuses while an
unresolved outcome exists. After inspecting `dm get`, a human can explicitly
reference the old plan with `--acknowledge-possible-duplicate PLAN_ID` to create
a new confirmation. The wire client must have retries disabled for both
creation and send.

On opening the database, any `executing_*` plan older than its request deadline
becomes `outcome_unknown`; it is never re-claimable. This covers process death
before, during, or after a request write.

Do not copy the reference client's warning-suppression query parameters
blindly. The two-account send test first establishes whether omitted/true flags
send normally or return a non-sending warning response. If a warning blocks the
send, persist only its code, expose it in a new dry run, and require
`--acknowledge-warning CODE` before creating a fresh confirmation that uses the
verified browser-equivalent acknowledgement. If the service offers no safely
testable acknowledgement, leave that message blocked. kcli does not independently
parse pickup, price, meeting, inspection, or payment meaning from the text.

Add exit code `8` for `ambiguous_external_state`. It is non-retryable until the
caller performs the stated reconciliation; mapping it to a generic transient
network error would invite duplicate messages.

### DM poll and watch

Use one synchronization implementation for both commands.

The default cycle is list-only so it does not call the state-touching
conversation `PUT`. It observes conversation-level activity from ID, unread
count/state, timestamp, listing context, and `textShortTrimmed`. It may emit a
preview, but not claim that preview is a complete message. `--open-changed` is
an explicit state-touching mode that opens changed threads to identify message
events; even then the event points callers to `dm get` and does not retain the
full body.

One cycle:

1. Load/validate the local cursor against the same database or require an
   explicit first baseline.
2. Acquire a short per-profile sync lease; do not run overlapping cycles.
3. Fetch bounded conversation pages newest-first.
4. Compare conversation ID, unread state, timestamp, count, listing, and preview
   against the stored remote fingerprint.
5. By default, create conversation-level events only. With `--open-changed`,
   open new/changed conversations after displaying that this is state-touching.
6. In open mode, prefer an upstream message ID. Otherwise derive a versioned
   SHA-256 fingerprint over account, conversation, direction, timestamp, kind,
   and content digest.
7. In one transaction upsert snapshots/messages, append ordered events, and
   create the next cursor sequence.
8. Emit complete UTF-8 JSON lines. Advance the named stored cursor only after the
   batch was committed and stdout flushed.

Cursors have prefix `cur_` and a base64url payload containing format version,
profile UUID, account subject hash, generation, and sequence. The values are
opaque rather than secret. Validate all components by database lookup; a cursor
from another account/store, an unknown sequence, or an old generation returns
`resync_required`, never an empty success.

Event IDs are deterministic for one observed upstream fact. Delivery is at
least once at kcli's stdout boundary; it cannot prove an external consumer
processed bytes after the OS accepted them. Consumers must deduplicate by
`event_id`. `--no-advance` plus explicit `--after` is the robust mode for a
consumer that owns acknowledgement. The stored head is monotonic: an explicit
`--after` older than the head replays from that point but, even with
`--advance`, never moves the head backwards.

First use requires `--since now` or a bounded RFC 3339 time. `since now` records
a baseline without emitting historical messages. Time backfill is limited by
available remote history and clearly marked partial. Periodically do a bounded
full reconciliation until ordering and watermarks have authenticated evidence.
Watch normally scans at most two 100-conversation pages per cycle and performs a
five-page reconciliation every 120 cycles; both remain below the global hard
limit and become configurable only toward slower/more conservative behavior.

`dm poll` runs once. `dm watch` repeats with a default 30-second interval plus
roughly 10% jitter; the default is also the floor, so `--interval` can only
slow it down. It emits NDJSON only, stays silent while idle unless heartbeats
were requested, honors `Retry-After`, and caps transient backoff at 15 minutes.
On `401`, it attempts the normal refresh once; on persistent auth failure,
`403`, challenge, or cursor corruption it emits/returns a terminal typed error.
A rejected or discontinuous cursor is `resync_required`: watch emits the
`system.resync_required` event first, and both commands exit `2`.

On SIGINT/SIGTERM, cancel the active request, finish no partial line, commit only
complete observed batches, and exit 130 for SIGINT or 143 for SIGTERM. Logs
contain event counts and IDs, never message bodies.

### Output, help, schemas, and agent ergonomics

TTY defaults to a compact table or readable object. Non-TTY finite commands,
including paginated search/list operations, default to one JSON envelope so
canonical specs and stop/count metadata are not lost. Watch events use NDJSON.
When a caller explicitly selects NDJSON for a finite list, emit data rows
followed by one `kcli.summary/v1` line. Data is stdout,
diagnostics/progress are stderr. `--quiet` suppresses diagnostics, never primary results. `--debug`
prints safe timing, operation, host, status, request ID, retry decision, and
schema information—not URLs with personal query values, headers, bodies, or
tokens.

Projection order is normalize, apply `--fields`, then encode. A field projection
that names an unknown path is exit 2 rather than silently empty. Users who need
arbitrary expressions compose the JSON output with their installed `jq`.

Kong supplies contextual `--help`, enum errors, suggestions, and examples.
`schema show` returns JSON Schema plus side-effect/evidence annotations. Dynamic
filter schemas overlay cached live metadata on the static `SearchInputV1` schema.
Golden tests ensure deterministic property and command ordering.

`kong-completion` supplies bash/zsh/fish hooks and invokes a fast completion path
in the binary. `kcli completion SHELL` prints the appropriate initialization
hook. Main must dispatch completion before opening config, SQLite, or keyring so
tab completion remains fast and side-effect-free. PowerShell completion is
deferred because the chosen library does not support it; do not clone the
command tree for a non-story convenience feature.

`doctor` performs bounded checks and returns one result per check:

- binary/build metadata and platform support;
- config and state path permissions;
- migration/schema health and free disk space;
- keyring availability without reading secret values;
- distribution and OAuth config present/missing;
- one optional public endpoint compatibility request only with `--network`;
- local account/token expiry and optional remote auth check with `--auth`;
- current API evidence/contract version and any blocked feature gates.

Plain `doctor` is local-only. It never sends a message, marks a conversation
read, opens a listing in a browser, or performs multiple probing retries.

## Delivery sequence

Each phase ends in a working, reviewable, green slice. Do not stack all command
stubs first or postpone tests until the end.

Phase 0's external gates—written permission, block expiry, and two authorized
test accounts—are waiting time, not engineering time, and they are the critical
path. Phase 1 and the fixture-driven parts of phases 2–8 touch no network: they
can proceed against the redacted anonymous fixtures already captured and fake
servers while those gates are open. Three rules keep that honest: no live
request runs before permission is recorded; no assumption stands in for a
phase-0 measurement (a story stays incomplete until its named evidence exists);
and transport-dependent code stays behind the kcli-owned `Transport` types so
the phase-0 choice between standard `net/http` and the one fixed profile is a
swap, not a rewrite. `0.1.0` cannot be tagged until phase 0's exit gate passes.

### Phase 0 — feasibility and permission gates (2–5 engineering days)

Deliverables:

- Obtain and record explicit written Kleinanzeigen permission for the intended
  automated access, collection, authenticated testing, and distribution. A
  local acknowledgement alone does not pass this gate.
- Wait for the current block to expire on the same development network. First
  run one request with the pinned Python reference's known profile. Record what
  requests preceded any block so volume and fingerprint cannot be conflated.
  Only after the baseline succeeds, compare standard `net/http` and the single
  fixed Go profile matching that reference. Stop on any block/challenge and
  retain only status/timing/redacted diagnostics. Do not move networks to evade
  the block.
- With the simplest passing fixed transport, prove categories, one location,
  one small search, one volatile detail, and a ranged image read at 2.5-second
  spacing. Separately prove standard `net/http` against the login host (during
  the authorized login test below) and the observed image CDN.
- Sample metadata from a small representative category set and capture redacted
  fixtures covering scalar, enum, boolean, numeric/range, repeated/in, and one
  unknown type if available. Prove the exact query encoding with one result
  difference or server acceptance per style.
- With two dedicated test accounts (one owning a disposable test listing) and
  explicit authorization, prove Auth0
  copy/paste PKCE, verified issuer/audience, refresh rotation behavior, account
  profile lookup, inbox list, conversation read, and before/after unread state.
- Record the exact seller/profile URL forms, contact-name default, whether
  conversation creation alone is visible, warning-flag semantics, and whether
  `/oauth/revoke` is supported. The only Phase-0 mutations are an explicitly
  authorized first contact and reply between the two test accounts, with
  harmless warning-triggering text where needed; a dry run cannot prove these
  contracts.
- Cross-compile an empty binary importing the chosen transport, keyring, and
  SQLite stack for darwin/linux/windows on amd64/arm64.

Exit gate:

- written permission has been obtained for the tested/released use, and one
  fixed Go transport passes without
  changing networks, identities, or fingerprints after a block;
- conversation read side effects are measured and documented; list-only polling
  remains valid regardless of the result;
- ID-token issuer, audience, and nonce match the values kcli sends and expects;
- filter serialization rules are documented with fixture evidence;
- dependency licenses and binary distribution are acceptable.

If any condition fails, update scope/evidence docs and stop. A language change
cannot repair a permission or anti-automation failure.

Suggested commits:

- `test(transport): add fixed-profile compatibility spike`
- `docs(api): record authenticated contract evidence`

### Phase 1 — executable foundation (3–4 days)

Deliverables:

- Initialize module, main package, build info, Kong root, context cancellation,
  and typed exit mapping.
- Implement profile/path/config loading and atomic writes.
- Open per-profile SQLite, embed/run first goose migration, generate sqlc code,
  and test corruption behavior.
- Implement split-entry keyring storage and the build-tagged test-only secret
  backend.
- Implement JSON/NDJSON/table/error encoders, TTY selection, field masks, and
  request IDs.
- Derive the operation catalog from command `Describe()` methods, add JSON
  Schema generation/validation, `schema`,
  `config`, `version`, initial `doctor`, and completion commands.
- Add testscript harness and CI skeleton.

Verification:

- Every command works with `--help` and has catalog/schema entries.
- Golden output proves stdout/stderr separation and exits 0/2/3/4/5/6/7/8/130.
- Concurrent database and keyring tests do not leak values.
- Six target binaries cross-compile; release snapshot runs.

Suggested commit: `feat(core): establish typed CLI and state foundation`

### Phase 2 — anonymous metadata and API boundary (4–5 days)

Deliverables:

- Implement fixed transport, host allowlists, app headers, redaction, limits,
  operation classes, cross-process rate reservations, and retry policy.
- Implement namespace/value/singleton decoding from fixtures.
- Ship category, location, and filter list/get/search commands and caches.
- Expose raw and normalized evidence fields.

Verification:

- Unit/fuzz tests cover malformed JSON, wrapper drift, IDs, redirects, 404,
  401/403, 429/Retry-After, 5xx, timeouts, and cancellation.
- Fixture tests cover all phase-0 metadata styles.
- One manually invoked anonymous contract smoke passes without exceeding the
  request budget.

Suggested commits:

- `feat(api): add bounded mobile transport and normalization`
- `feat(discovery): add category location and filter metadata`

### Phase 3 — search (5–7 days)

Deliverables:

- Implement `SearchInputV1`, flag/input exclusivity, static validation, and
  category/location resolution.
- Implement common and metadata-driven filter serialization.
- Implement bounded pagination, deduplication, client-side exclusions, stop
  reasons, canonical specs, structured/table outputs, and seller indexing.

Verification:

- Table-driven tests cover every common filter and sort.
- Golden request tests cover every proven metadata type/style and reject unknown
  or unsupported values before network access.
- The release-snapshot audit (one bounded walk of the category tree, see the
  filter metadata section) has no advertised searchable type/style left in
  `unsupported-client`.
- Multi-page fake-server tests cover duplicate IDs, disappearing results,
  exclusions, totals that drift, empty pages, bounds, and broken pipes.
- Live smoke demonstrates one common and one category-specific filter.

Suggested commit: `feat(search): add reproducible filtered listing search`

### Phase 4 — listing, media, and local seller discovery (4–6 days)

Deliverables:

- Implement detail normalization, availability states, raw preservation, and
  complete seller/media/attribute extraction.
- Implement image enumeration, safe streaming download, and official URL open.
- Implement direct listing seller resolution, known-ID/profile lookup, Unicode
  local name search, and known seller listings.

Verification:

- A field-coverage test fails when a known fixture field is neither normalized
  nor present in raw output.
- Media tests cover redirect, private host, size, MIME/magic mismatch, traversal,
  symlink, collision, partial write, overwrite, and interruption.
- Seller tests prove every output is source/completeness labeled and a local miss
  never claims global absence.
- Live smoke reads one available detail and one image prefix; a vanished listing
  is a successful expected test branch.

Suggested commits:

- `feat(listing): expose complete listing and media details`
- `feat(seller): add source-labeled local discovery`

### Phase 5 — authentication (4–7 days)

Deliverables:

- Implement PKCE URL/state/verifier, TTY and stdin redirect capture, strict
  redirect parsing, explicit in-params token exchange, OIDC/nonce verification,
  account-ID resolution, split keyring persistence, refresh lease, status/check,
  and local logout.
- Ensure authenticated headers differ correctly between main and gateway hosts.

Verification:

- Fake token-endpoint tests cover bad issuer, audience, nonce, expiry, state,
  redirect host/path, OAuth errors, refresh rotation, `invalid_grant` reuse,
  keyring chunking bounds, and races.
- Redaction snapshots contain no access token, refresh token, code, verifier,
  account email, Authorization value, or secret environment value.
- Dedicated-account smoke proves login, restart, refresh, status, profile ID,
  and logout/relogin without checking credentials into any file.

Suggested commit: `feat(auth): add verified PKCE sessions and keyring storage`

### Phase 6 — inbox reads and account state (3–5 days)

Deliverables:

- Implement DM list/get/mark-read, pagination, wrapper normalization, context
  snapshots, body-free fingerprints, and account isolation.
- Classify conversation `PUT` side effects in help/schema output.

Verification:

- Fixtures cover buyer and seller roles, unread counts, singleton/list wrappers,
  message kinds, missing IDs, unknown direction, and unavailable listings.
- Dedicated-account read smoke verifies history ordering and preserved context.
- One authorized mark-read smoke verifies before/after state and no retry after
  an injected ambiguous response.

Suggested commit: `feat(dm): add inbox reading and read-state control`

### Phase 7 — confirmed messaging (5–7 days)

Deliverables:

- Implement digest-only confirmation plans and atomic state machines; confirm
  must receive the exact message again.
- Implement reply and start dry runs, exact preview, confirmation, one-attempt
  transport, verified content-warning acknowledgement, duplicate checks, and
  reconciliation.
- Add exit 8 and outcome audit records without message bodies in logs.

Verification:

- Race tests prove two confirmers yield exactly one network attempt.
- Fault-injection tests cover expiry, tamper, wrong profile/account/target,
  definite 4xx, connect failure, timeout before/after write, lost response,
  process death before/after write, stale `executing_*` recovery,
  create-success/send-failure, and history reconciliation.
- No retry middleware is reachable from an external-send operation.
- With explicit per-attempt authorization, the sender account sends one unique
  reply and one first contact to the receiver account's disposable listing; the
  receiver validates the text and warning behavior exactly.

Suggested commit: `feat(dm): add preview-bound send-once messaging`

### Phase 8 — resumable polling and watch (5–7 days)

Deliverables:

- Implement conversation-summary fingerprints, event spool, database-validated
  cursors, baseline/backfill, list-only reconciliation, opt-in changed-thread
  opening, poll, watch, heartbeats, leases, retention, and signals.
- Document stdout-boundary delivery semantics and consumer deduplication.

Verification:

- Deterministic clock/transport tests cover no-change, new thread, new message,
  read change, unknown change, edits/collisions, ordering, pagination, restart,
  stale/wrong cursor, lease contention, retention, 401 refresh, 429 backoff,
  broken pipe, SIGINT, and corrupted state.
- Kill/restart tests demonstrate no event loss before acknowledged cursor and
  permitted duplicates retain the same event ID.
- Two-account smoke observes a manually generated incoming conversation change
  in default list-only mode without marking it read; opt-in open mode is checked
  against the measured side effect.

Suggested commit: `feat(sync): add resumable DM poll and watch`

### Phase 9 — hardening and `0.1.0` release candidate (4–6 days)

Deliverables:

- Finish doctor, completion on all promised shells, man/help examples, and an
  installable agent skill.
- Freeze v1 schemas and golden examples; run compatibility review against all
  docs.
- Add release archives, checksums, SBOMs, signatures/attestations, and install
  instructions.
- Perform security/privacy review, dependency/license review, and agent-DX
  scoring.

Verification:

- All commands pass Linux/macOS/Windows smoke; amd64/arm64 artifacts build.
- `go test -race ./...`, fuzz seed corpus, `go vet`, staticcheck, govulncheck,
  `go mod verify`, sqlc regeneration, docs checks, and GoReleaser snapshot pass.
- Manual anonymous and dedicated-account acceptance runs cover every scoped
  story exactly once at conservative volume.
- No deferred command is reachable; docs/changelog/version agree; the release
  tag is created only after evidence is recorded.

Suggested commit: `chore(release): prepare 0.1.0`

## Story-to-delivery traceability

No story becomes “done” from code review alone. Its named test/evidence must
also pass.

| Story | Phase | Primary command | Required acceptance evidence |
|---|---:|---|---|
| `V01-SEARCH-01` | 3 | `search` | Empty and keyword fake/live searches return bounded results |
| `V01-SEARCH-02` | 2 | `category list/get/search` | Refreshed tree resolves ID and unambiguous path |
| `V01-SEARCH-03` | 2–3 | `location resolve`, `search` | Place/postcode candidates and radius/all-Germany request fixtures |
| `V01-SEARCH-04` | 3 | `search` | Price, ad type, and picture-required golden requests |
| `V01-SEARCH-05` | 0, 2–3 | `filter list`, `search --filter` | Every release-snapshot searchable type/style implemented; upstream-unsupported and later drift explicit |
| `V01-SEARCH-06` | 2–3 | `filter get`, `search` | Canonical spec exposes combined/removed filters and validation |
| `V01-SEARCH-07` | 3 | `search --sort` | Four supported sort values map exactly; distance precondition enforced |
| `V01-SEARCH-08` | 3 | `search --paginate` | Drift/duplicate/bound tests and multi-page smoke |
| `V01-SEARCH-09` | 3 | `search --exclude` | Unicode case-folded title/description exclusion with observable counts |
| `V01-SEARCH-10` | 1, 3 | `search --input`, `schema show` | Stored versioned spec round-trips and rejects unknown fields |
| `V01-LISTING-01` | 4 | `listing get` | Strict ID and official URL resolve the same listing |
| `V01-LISTING-02` | 4 | `listing get` | Known-field coverage fixture plus raw unknown preservation |
| `V01-LISTING-03` | 4 | `listing images` | Every returned media relation/variant appears in stable order |
| `V01-LISTING-04` | 4 | `listing images --download` | Safe path/redirect/size tests and one ranged live image |
| `V01-LISTING-05` | 4 | `listing get` | Metadata pickup/shipping/seller signals remain separate from prose |
| `V01-LISTING-06` | 1, 4 | `listing get --raw` | Normalized and redacted raw fixture with no auth material |
| `V01-LISTING-07` | 4 | `listing get` | 404 and returned reserved/expired states are distinct |
| `V01-LISTING-08` | 4 | search/listing outputs | Every result contains a verified official HTTPS public URL |
| `V01-USER-01` | 4 | `seller get --listing` | Listing seller normalized and source-labeled |
| `V01-USER-02` | 0, 4 | `seller get` | Known local ID and fixture-proven profile URL resolve; miss says local-only |
| `V01-USER-03` | 4 | `seller search` | Exact/contains Unicode matching over encountered sellers |
| `V01-USER-04` | 4 | seller commands | Full observed public metadata and known listings retained |
| `V01-USER-05` | 4 | all seller outputs | Source, completeness, index scope, and observed time always present |
| `V01-DM-01` | 0, 5 | `auth login/status/logout` | Verified PKCE, refresh/restart, account ID, and logout smoke |
| `V01-DM-02` | 6 | `dm list` | Auth fixture/live list with unread/listing/counterparty context |
| `V01-DM-03` | 0, 6 | `dm get` | Complete available oldest-first history and measured read side effect |
| `V01-DM-04` | 7 | `dm reply` | Exact body supplied for dry run and confirm; two-account reply receipt verified |
| `V01-DM-05` | 7 | `dm start` | Existing-thread conflict, contact/create semantics, warning acknowledgement, and two-account receipt verified |
| `V01-DM-06` | 6 | `dm mark-read` | Dry run and verified before/after account state |
| `V01-DM-07` | 6 | `dm get` | Conversation retains listing/counterparty snapshot after detail 404 fixture |
| `V01-DM-08` | 5, 7 | auth/send errors | 401/403/429/timeout mapping and zero blind send retries |
| `V01-DM-09` | 8 | `dm poll` | List-only activity, restart/cursor/at-least-once fault suite |
| `V01-DM-10` | 8 | `dm watch` | List-only NDJSON activity, opt-in open mode, jitter/backoff, and SIGINT suite |

## Test strategy

### Test layers

1. Pure unit tests: validation, decoding, normalization, redaction,
   projection, cursor, fingerprint, confirmation transitions, and path rules.
2. Package contract tests: `httptest` hosts with injected transport/base URLs,
   exact requests, response variants, retries, time, keyring, and SQLite.
3. Binary tests: testscript fixtures execute the compiled CLI and inspect stdout,
   stderr, files, schemas, exit codes, signals, and multiple processes.
4. Cross-platform integration: real SQLite, filesystem permissions, keyring
   availability/mocks, terminal behavior, completion loading, and cross-builds.
5. Manual anonymous contract smoke: covered by written permission, explicitly
   invoked, low-volume, and free of mutations.
6. Manual authenticated acceptance: two dedicated accounts, separately
   authorized, smallest possible reads, and exactly the release-gating sends.

Never record tokens, account email, live message bodies, private seller data, or
full raw personal payloads in fixtures. Write a minimizer that selects relevant
keys and replaces IDs/text with shape-preserving values before a fixture can be
committed. Review fixture diffs like source code.

### Required fault cases

- DNS/connect/TLS error before request write;
- timeout before headers and after the server may have committed;
- malformed/truncated/oversized JSON;
- namespace or singleton/list drift;
- 200 with unknown enum/field;
- 400 validation, 401 expired, 403 blocked, 404 volatile listing, 409 conflict,
  429 with valid/invalid `Retry-After`, and 500/503;
- refresh-token rotation and concurrent refresh;
- `invalid_grant` after refresh-token reuse or an interrupted rotation;
- concurrent confirmation and sync commands;
- disk full, read-only config/state, database busy/corrupt, keyring unavailable;
- broken stdout pipe and process interruption;
- symlink/path traversal and hostile media redirects;
- hostile listing/message strings containing terminal escapes, newlines, prompt
  injection, JSON-like content, and invalid Unicode bytes.

Human table output strips or visibly escapes terminal control sequences.
Structured output preserves safe Unicode data exactly. Remote prose is never
rendered to stderr as an instruction.

### CI lanes

On every pull request:

```text
python3 scripts/check_docs.py
gofmt check
go mod verify
go generate ./... && git diff --exit-code
go vet ./...
staticcheck ./...
go test -race ./...
govulncheck ./...
goreleaser check
goreleaser release --snapshot --clean
```

Run the full race lane on Linux and targeted unit/integration lanes on current
macOS and Windows. Cross-compile darwin/linux/windows for amd64/arm64 on every
release-candidate change. Network tests are excluded from ordinary CI. A manual
workflow may use release secrets only when recorded written permission covers
that workflow and an operator explicitly starts it; logs and artifacts pass
redaction checks.

Fuzz targets run their committed seed corpus on each pull request and a bounded
time budget manually before release. Do not aim a fuzzer at the live service.

## Release and maintenance

GoReleaser produces `.tar.gz`/`.zip` archives, SHA-256 checksums, SBOMs, and
signed provenance for the six targets. Embed version, commit, build time, Go
version, API contract version, and fixed transport profile name. Do not embed
user secrets. Official release jobs inject distribution-level mobile client
configuration without exposing it in workflow logs.

The initial release process is:

1. Freeze command and JSON schema goldens.
2. Complete all phase-9 checks and manual acceptance evidence.
3. Update all docs, `CHANGELOG.md`, and comparison links.
4. Commit with Conventional Commits; use `chore(release): prepare 0.1.0` for the
   preparation change.
5. Create the signed `v0.1.0` tag only after review.
6. Build artifacts from the tag in CI, verify checksums/signatures on a clean
   machine, then publish.
7. Install and run `kcli doctor`, one anonymous read, and help/schema checks from
   each OS artifact.

Dependency updates require unit/contract tests, a transport smoke when the TLS
stack changes, a database migration/recovery run when SQLite/goose changes, and
schema golden review when Kong/jsonschema changes. A fixed transport profile is
versioned with the API contract and changes only from evidence, never as an
automatic reaction to a `403`.

## Security and privacy review checklist

- No password, token, OAuth code/verifier/state, app Basic value, account email,
  message text, or raw authenticated payload enters logs or crash output.
- User tokens never reach main/gateway/login/media hosts outside their explicit
  policy; redirects cannot change that decision.
- Authenticated operations verify that path user ID matches the active profile.
- IDs and URLs are parsed, allowlisted, length-bounded, and encoded once.
- Raw output is redacted and opt-in; headers are never included.
- Secret-store failure is terminal for persistence.
- Complete message bodies and confirmation text are not persisted in v0.1;
  private file permissions and short preview retention match the documented
  local threat model.
- Confirmation is account/target/body-digest/expiry bound, requires the exact
  body again, and is atomically single-use.
- Create/send have no automatic retry and have explicit unknown outcomes.
- Platform warnings and access controls are not disabled or bypassed.
- Search/poll/watch are bounded; rate reservations work across processes.
- Downloads cannot choose arbitrary remote URLs or escape the authorized path.
- Listing and message content is untrusted data and never evaluated as commands.
- The binary has no telemetry and opens no listener in v0.1.

## Code-size and complexity budget

Target at most roughly 7,500 hand-written non-test Go lines for v0.1, excluding
generated sqlc code. Treat this as a pressure against unnecessary frameworks,
not as a reason to omit safety tests. Expected distribution:

| Area | Approximate hand-written Go |
|---|---:|
| CLI/catalog/schema/output | 1,100–1,500 |
| Mobile client and normalization | 1,200–1,600 |
| Search/listing/seller/media | 1,200–1,600 |
| Auth/secrets | 600–900 |
| DM confirmation/sync | 1,200–1,700 |
| State/platform/build glue | 600–900 |

Generated queries, embedded SQL, fixtures, and tests are additional. If the core
exceeds the budget by more than 20%, review duplicated command/schema models,
wire normalization, and output adapters before adding abstractions.

For one experienced Go engineer, the evidence-heavy plan is approximately 39–59
engineering days, with external waiting for permission/test-account access not
included. Agent assistance can reduce mechanical work, but authenticated safety
and contract gates still require human review.

## Risks and explicit responses

| Risk | Likelihood / impact | Response |
|---|---|---|
| Private API or app credentials change | High / release-blocking | Version contract, overrides, doctor, small adapter, no invented endpoints |
| TLS/client fingerprint rejected | Medium-high / release-blocking | Phase-0 transport matrix; fixed profile only; stop on blocks |
| Terms prohibit intended automation | High / release-blocking for distribution | Obtain explicit written permission covering the intended automated use before live testing or release; no scheduled live CI |
| Auth response/issuer differs | Medium / auth-blocking | Dedicated-account spike; exact issuer/audience/nonce checks; no silent fallback |
| Conversation read changes unread state | Medium / open-mode impact | Measure and label; default poll/watch remains list-only |
| Dynamic filter encoding varies | High / partial search | Metadata proof states, fixtures by style, no guessed serialization |
| Duplicate message after timeout | High / user harm | Plan state machine, no retry, reconciliation, exit 8 |
| Local seller results mistaken as global | Medium / misleading | Source/completeness/index metadata in every result |
| Cross-process request bursts | Medium / account risk | SQLite reservation scheduler and sync leases |
| Sensitive DM data at rest | Medium / privacy harm | Do not retain full bodies; private state, bounded previews, redacted logs |
| SQLite or transport dependency size | Medium / maintenance | Phase-1 size/build benchmark; retain only proven dependencies |
| Schema library pre-1.0 drift | Medium / compatibility | Pin, golden schemas, review before upgrade |

## Later daemon and MCP plan (not `0.1.0`)

Only begin this after v0.1 commands and schemas are stable.

The daemon is the same binary in `kcli daemon run`, foreground under the user's
service manager. It owns the sync lease and serves versioned HTTP/1.1 JSON plus
NDJSON over a user-only Unix-domain socket; Windows uses a named pipe. The CLI
falls back to direct core execution unless `--require-daemon` is supplied. No TCP
listener is enabled by default.

MCP uses the official
[`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
over stdio. Its typed `AddTool` path already uses `google/jsonschema-go`, so the
same input/output structs can be registered with little glue. Expose only finite
tools: search, listing detail, seller local lookup, DM list/get/poll, reply/start
dry run, and confirmed execution. Keep continuous watch in the daemon. Use
simple object input structs and server-side XOR validation rather than root
schema compositions that may be interpreted differently by MCP clients.

Before that later release, add daemon ownership/crash/upgrade tests, socket
permission tests, client-version negotiation, MCP Inspector tests, and a new
threat review. Daemon/MCP work must not alter direct CLI semantics or weaken
message confirmation.

## Fable plan review

The complete plan was reviewed on 3 September 2026 with the `omp` `fbl` model
preset at high reasoning. The review included this plan, the v0.1 scope, command
syntax, mobile API reference, and DM synchronization design. Its findings were
worked through as follows.

```text
omp --model fbl --thinking high --no-session --no-tools --max-time 10m -p \
  @docs/implementation-plan.md @docs/v0.1-scope.md \
  @docs/command-syntax.md @docs/mobile-api.md @docs/dm-sync.md \
  "Act as a skeptical principal engineer ..."
```

Accepted release-blocking changes:

- made explicit written permission—not a local risk acknowledgement—a gate for
  automated live testing and distribution, and required the first transport
  comparison to run from the same network only after the existing block clears;
- defined one evidence-backed fixed transport profile and a stop condition,
  rather than an open-ended browser-impersonation or language-switch strategy;
- introduced kcli-owned transport request/response types because `tls-client`
  uses `fhttp`, while OAuth, OIDC, login, and media stay on standard `net/http`;
- required two separately authorized accounts to prove replies, first contact,
  warning behavior, message receipt, read side effects, and ambiguous-outcome
  reconciliation;
- changed `dm poll` and `dm watch` to conversation-list-only operation by
  default, with state-touching conversation opens behind `--open-changed`.

Accepted correctness and safety changes:

- stale `executing_*` confirmation plans become `outcome_unknown` and can never
  be claimed again; confirms must resupply the exact message body whose digest
  was previewed;
- refresh-token environment injection was removed from v0.1 until rotation and
  reuse behavior is proven; OAuth uses an explicit token auth style and manual,
  leased refresh rather than an automatically retrying token source;
- login always sends and verifies a nonce, verifies the exact captured issuer,
  and stores access, refresh, email, and expiry values as separate keyring
  entries without persisting an ID token;
- metadata option keys, `search-param` capability markers, and `search-style`
  are modeled separately; every searchable type/style in the release snapshot
  must work, while newly observed drift fails visibly;
- seller/event retention is bounded, test secret storage is excluded from
  release builds, local logout is not described as remote revocation, and exit
  code `8` represents an ambiguous mutation outcome;
- local cursors are validated directly against the profile database rather than
  signed for a nonexistent external consumer; fallback message matching is
  normalized and remains unknown when evidence is insufficient;
- command metadata is derived from each Kong command's `Describe()` method,
  while input mutual exclusion uses explicit validation rather than relying on
  parser tag groups;
- completion has a pre-initialization fast path and v0.1 promises bash, zsh, and
  fish only; finite pagination defaults to a JSON envelope, while explicitly
  requested NDJSON ends with a summary record;
- YAML, an embedded jq evaluator, application-level DM database encryption, a
  custom entropy interface, and logout confirmation IDs were removed from the
  first-release design to reduce code without weakening its stated guarantees.

Two suggested simplifications were deliberately not applied. Embedded Goose
migrations remain because released local cursors and confirmation state will
need forward-only, transactional upgrades; phase 1 will remove it if its binary
or maintenance cost is not justified. The Go-versus-Rust comparison remains
because language selection was an explicit planning requirement.

One factual correction from the review was rejected based on the pinned
upstream source: the complete `X-EBAYK-APP` value, including its creation-time
suffix, is created once for a client installation rather than regenerated for
each request. Phase 0 will record observed behavior and may supersede that
source-backed conclusion. The plan also does not invent a `KEY=MIN..MAX` range
syntax or warning-acknowledgement behavior; both stay behind live contract
gates. Dependency versions were independently resolved with the Go module
proxy on the plan date before being pinned above.

## Second review pass

The plan set was re-read end to end on 3 September 2026 after the Fable review
had been incorporated, this time looking for internal contradictions, hidden
release blockers, and unspecified behavior. Adjustments made:

- OAuth/OIDC libraries were removed. The verified contract is one JSON `POST`
  per grant; `x/oauth2` sends form bodies and its auth-style auto-detection
  could re-send a single-use code, while its `TokenSource` was already
  bypassed for the leased refresh. `go-oidc`'s discovery and JWKS signature
  check guarded an ID token that arrives over the TLS back channel, which OIDC
  Core §3.1.3.7 rule 6 explicitly allows to be validated by TLS instead; the
  email claim is only used to address requests that the service validates
  against the access token. Issuer, audience, expiry, and nonce checks remain.
  This also removes one host from the login client and one phase-0 unknown.
- The 2 KiB keyring ceiling that "failed closed" would have made login
  impossible on every platform for a long Auth0 JWT. Windows' real cap is
  2,560 bytes per credential blob; values above it are chunked into numbered
  entries and a partial chunk set reads as missing.
- `dm get --mark-read` was removed. It combined a state-touching read with an
  account mutation that had no dry run, contradicting the rule that every
  state-changing command supports `--dry-run`; `dm mark-read` already exists.
- The public-website location autocomplete fallback was dropped from v0.1. It
  was a second host and fingerprint tried after the mobile endpoint failed—the
  identity-hopping the transport policy forbids—and no scoped story needs it.
- Unspecified behavior was fixed in the contract: `resync_required` exits `2`,
  the local cross-process rate reservation refusing a request is
  `rate_limited_local` under exit `6`, SIGTERM exits `143`, the watch interval
  floor equals its 30-second default, an explicit older `--after` never rewinds
  the stored head, and `dm poll` follows the finite-command output rules.
- "Every searchable type/style in the release snapshot" now has a procedure:
  phase 0 proves each filter *kind* on a sampled category set; the release
  snapshot is one bounded, recorded walk of the cached category tree whose
  only purpose is to detect a kind the sample missed.
- Phase 0's external gates are identified as the critical path, and the
  offline phases are explicitly allowed to proceed against fixtures while those
  gates are open, without weakening any evidence requirement.
- Text defects: the architecture named "four" interface boundaries and listed
  three; the layout still described an env-backed secret store that v0.1 had
  already removed.

Reconsidered and left unchanged: Goose and sqlc (already gated on phase-1
cost), the SQLite cross-process rate reservation (an in-memory limiter cannot
see sibling CLI processes), `listing open` (harmless, and the URL opener is
needed for login anyway), and the two-page/five-page polling bounds (explicitly
provisional until authenticated evidence exists).

## Plan definition of done

Implementation is complete only when all of the following are true:

- Phase 0 passed without bypassing an access control.
- Every command in [`command-syntax.md`](command-syntax.md) marked v0.1 exists,
  has help, JSON Schema, tests, and an operation-catalog entry.
- Every scoped story in the traceability table has its named automated and,
  where required, authorized live evidence.
- Every acceptance criterion in [`v0.1-scope.md`](v0.1-scope.md) passes.
- Anonymous commands require no user account and authenticated commands cannot
  accidentally run under the wrong profile.
- No selling, offer, shipping, payment, transaction, or structured pickup
  command is present.
- Static checks, race tests, fault tests, documentation checks, and release
  snapshots are green on the supported platform matrix.
- Security/privacy and dependency/license reviews have no unresolved
  release-blocking finding.
- `CHANGELOG.md`, schemas, docs, examples, agent skill, binaries, and the SemVer
  tag all agree on `0.1.0`.
