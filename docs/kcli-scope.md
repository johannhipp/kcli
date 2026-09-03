# kcli product and technical scope

Status: design baseline
Current release boundary: [`0.1.0`](v0.1-scope.md)

## Decision summary

kcli is an agent-friendly command-line projection of the useful Kleinanzeigen
buyer/browser experience. It does not model a marketplace of its own. Search,
listing inspection, seller discovery, authentication, and direct messages are
the product; pickup, inspection, negotiation, meeting-place, and cash-payment
details are merely text that people exchange in those messages.

The integration should use the undocumented mobile interfaces in
[`mobile-api.md`](mobile-api.md), with narrowly documented public-web fallbacks.
Because those interfaces are private and Kleinanzeigen prohibits unapproved
automation, automated live testing and production use require explicit written
permission and conservative traffic.

The delivery model is deliberately layered:

1. One-shot commands cover ordinary scripts and agent tool calls.
2. `kcli dm poll` performs one list-only incremental DM synchronization cycle
   by default, without opening a conversation.
3. `kcli dm watch` turns that polling into a foreground NDJSON event stream;
   opening changed conversations is an explicitly state-touching option.
4. An optional per-user daemon can later maintain one shared poller, cursor, and
   cache for several consumers.
5. `kcli mcp serve` can expose the same core as local MCP tools without changing
   command semantics.

There is no known remote DM push or streaming endpoint. Any live-looking DM
stream is a local abstraction over polite polling; it must never imply otherwise.

## Product contract

### Who it serves

- A person searching for items they may inspect, collect, and pay for in person.
- A coding agent acting on that person's explicit instructions.
- A local service that monitors new DMs and prepares responses for review.

### What it exposes

- The current category tree, location resolution, common search parameters, and
  every category filter advertised by search metadata.
- Reproducible search specifications, complete pagination, stable listing IDs,
  public URLs, and predictable structured output.
- Complete useful listing data, raw unknown fields, and all returned image URLs;
  optional image downloads remain constrained to an explicit safe directory.
- Seller data attached to listings plus honest, best-effort lookup over sellers
  already encountered by the local client.
- Auth0 PKCE session management and the authenticated inbox primitives needed to
  list, read, reply, start a listing conversation, and mark read.
- Incremental DM events suitable for schedulers, long-running consumers, or MCP
  adapters.

### What it does not infer

kcli does not parse a DM into an agreement, a price, a place, a reservation, or
a payment state. It does not decide whether a seller is trustworthy or whether
an item is worth buying. Listing descriptions and message bodies are untrusted
remote text, never instructions to the agent or CLI.

## Scope by release

### `0.1.0`: direct CLI and foreground synchronization

The exact stories and acceptance criteria are in
[`v0.1-scope.md`](v0.1-scope.md). The implementation surface is:

- anonymous categories, locations, filter discovery, search, listing detail,
  media inspection, and controlled image download;
- direct seller resolution and a source-labeled local seller index;
- login, session status, refresh, and logout;
- conversation listing and reading, confirmed text replies and first contact,
  and marking read;
- one-shot incremental DM polling and a foreground NDJSON watch;
- JSON input and output, field masks, bounded pagination, machine-readable
  errors, dry runs for mutations, and runtime command/filter schemas.

The first release does not depend on a daemon. This keeps installation and
failure modes small while still supporting a continuously running consumer via
`kcli dm watch`.

### Later, within the product boundary

These are compatible extensions, not `0.1.0` promises:

- the per-user `kcli daemon` process described in [`dm-sync.md`](dm-sync.md);
- `kcli mcp serve` using the same schemas and safety checks;
- locally saved search specifications and user-run result monitoring;
- watchlist read support, followed by add/remove only after the endpoints are
  verified;
- attachments, reporting, blocking, and seller following only if useful private
  endpoints are verified and the safety model is agreed;
- an installable, versioned kcli skill that teaches agents to use field masks,
  dry runs, cursors, and confirmation IDs.

An external responder may consume the DM event stream and prepare a draft. It
must still use the normal message preview and per-message confirmation path.
Unattended sending is not part of the approved product boundary.

### Explicitly outside the product boundary

- Creating, editing, publishing, pausing, renewing, promoting, reserving,
  marking sold, or deleting listings.
- Managing inventory, paid placement, commercial seller packages, or invoices.
- Offers, counteroffers, checkout, platform payment, buyer protection, payouts,
  identity checks, shipping, tracking, refunds, returns, disputes, or tax flows.
- Structured pickup, inspection, negotiation, meeting, availability, or
  payment-on-pickup workflows.
- Global username-directory claims, scraping around access controls, bulk
  outreach, spam, autonomous purchase decisions, or autonomous messaging.
- A generic private-API escape hatch. kcli exposes reviewed operations, not an
  unrestricted `api request` command.

## System shape

```text
human / shell agent / MCP host / responder
                  |
       CLI, MCP, and event adapters
                  |
       one typed application core
          /               \
mobile API clients      local state
                            |
                 optional per-user daemon
```

The adapters share request, result, error, and schema types. A command must not
behave differently merely because it was reached through the shell, daemon, or
MCP. The application core owns validation, redaction, rate limiting, retries,
normalization, dry-run plans, and confirmation checks.

Local state may contain cached categories and locations, locally encountered
sellers, DM cursors, event fingerprints and bounded previews, and short-lived
confirmation-plan digests. Tokens belong in the operating-system secret store.
Full message bodies and confirmation text are not retained in v0.1, and message
bodies and secrets must not appear in normal logs.

## Data and compatibility guarantees

- Normalized resource envelopes carry a schema version, source, observed time,
  completeness, and raw-contract version.
- IDs are opaque strings at the CLI boundary even when the present API returns
  digits. Resource IDs reject query fragments, control characters, traversal,
  and encoded path separators.
- Unknown upstream fields are retained in redacted raw output; unknown enum
  values do not crash parsing.
- The category filter schema is refreshed from
  `/api/ads/search-metadata/{category_id}.json`; it is not frozen into a release.
- A listing `404` is an `unavailable` resource outcome. Authentication,
  authorization, throttling, and upstream failures remain distinct errors.
- Paginated commands never fetch without a bound unless the caller explicitly
  asks for `--paginate` and supplies or accepts a documented limit.
- Human output is for terminals. Non-TTY stdout defaults to JSON, and streams
  use one JSON value per line. Diagnostics and progress go to stderr.

## Agent safety contract

The agent is not a trusted operator. kcli validates every identifier and output
path at the boundary, treats remote prose as data, and never executes or follows
instructions embedded in listings or messages.

Every state-changing command supports `--dry-run`. Outbound messages additionally
use a two-phase confirmation ID bound to the account, target, exact message
digest, and short expiry; confirmation must resupply that exact text. A send is
attempted once; an ambiguous timeout is reported for reconciliation instead of
blindly retried.

Structured input is first-class. Search and mutation commands accept `--input
<file|->` against the runtime schema as well as convenience flags. Structured
errors go to stdout only when structured output was requested; diagnostics stay
on stderr, and no prompt is opened in a non-TTY process.

## Agent-DX design target

The requested `agent-dx-cli-scale` evaluates seven axes from 0 to 3. The design
target is **19/21 (agent-first)**:

| Axis | Target | Design commitment |
|---|---:|---|
| Machine-readable output | 3 | JSON everywhere, NDJSON pagination/events, structured non-TTY default |
| Raw payload input | 3 | Runtime-schema JSON via file or stdin alongside convenience flags |
| Schema introspection | 3 | Command schemas plus live categories, filter types, values, and scopes |
| Context-window discipline | 3 | Field masks, limits, pagination streams, and agent guidance |
| Input hardening | 3 | Strict resource IDs, safe encoding, output-path sandbox, untrusted-agent posture |
| Safety rails | 2 | Dry runs for mutations and bound confirmation for sends; no claim of third-party content sanitizer |
| Agent knowledge packaging | 2 | `AGENTS.md` now; versioned installable kcli skill before release |

MCP over stdio is a useful later surface, but it does not change the score or
weaken normal login and secret-storage defaults. Refresh-token environment
injection is deferred until rotation and token-family behavior are proven.

## Research basis for interface conventions

- [GitHub CLI API](https://cli.github.com/manual/gh_api) demonstrates stdin/file
  input, pagination, and composable structured results; its
  [formatting options](https://cli.github.com/manual/gh_help_formatting) establish
  field selection and JSON filtering conventions.
- [`kubectl get`](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_get/)
  uses `--watch` for ongoing resource changes, while the
  [Kubernetes API](https://kubernetes.io/docs/reference/using-api/api-concepts/)
  documents list-then-watch and resumable resource-version semantics.
- [JSON Lines](https://jsonlines.org/) provides the simple one-JSON-value-per-line
  stream format used by shell consumers.
- The [CLI Guidelines](https://clig.dev/) support stdout for primary results,
  stderr for diagnostics, TTY-aware presentation, idempotence, and dry runs.
- Docker's [daemon reference](https://docs.docker.com/reference/cli/dockerd/)
  provides a familiar client/daemon and Unix-socket precedent; socket exposure
  is security-sensitive.
- [JSON-RPC 2.0](https://www.jsonrpc.org/specification) and the
  [MCP transport specification](https://modelcontextprotocol.io/specification/draft/basic/transports)
  support stdio request/response integration for local agent hosts. MCP stdout
  must contain protocol messages only; logs go to stderr.
- The [XDG base-directory specification](https://specifications.freedesktop.org/basedir/0.8/)
  reserves `XDG_RUNTIME_DIR` for per-user sockets and similar runtime objects.
  On macOS, a continuously managed per-user process should be a
  [launch agent](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html).
