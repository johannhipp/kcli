# DM polling, watching, and local service design

Status: proposed contract
Current release target: one-shot polling and foreground watching in `0.1.0`

## The upstream reality

The known mobile interface can list conversations and open one conversation. It
does not expose a WebSocket, server-sent-event feed, webhook, long-poll, or
cursor-based change endpoint. kcli therefore cannot honestly provide remote
push. It can provide a reliable local change stream by periodically listing
conversations, fetching only those that appear changed, and comparing them with
durable local state.

Authenticated live verification is still required. Until a dedicated test
account proves identifiers, ordering, pagination, and side effects, the design
below is a contract target rather than a claim about production behavior.

## Access modes

| Mode | Command | Lifecycle | Best use |
|---|---|---|---|
| Snapshot | `kcli dm list` / `kcli dm get` | One request sequence | Humans and ordinary agent calls |
| Incremental poll | `kcli dm poll` | One synchronization cycle | Cron, job runners, MCP tools |
| Watch | `kcli dm watch` | Foreground until interrupted | Pipes and a single continuous consumer |
| Shared daemon | `kcli daemon run` | OS-managed per-user process | Multiple consumers and durable monitoring |

`watch` is the primary streaming word because these are changing resources, not
log files. `poll` always means one finite cycle. Neither spelling hides how the
remote side works.

## One synchronization cycle

1. Load the caller's local cursor and per-conversation fingerprints, or establish
   an explicit initial baseline.
2. Fetch conversation pages newest first, with a strict maximum. The initial
   default scans two pages; every 120 successful cycles it performs a bounded
   five-page reconciliation until authenticated evidence justifies other bounds.
3. Compare each conversation's ID, unread state, timestamp, count, and preview
   with the stored summary.
4. In the default list-only mode, persist and emit only conversation-level
   changes. Do not open changed threads because that `PUT` may mark them loaded
   or read.
5. With explicit `--open-changed`, open only a new or changed conversation to
   retrieve its available messages and label the operation as account-state
   touching. Prefer an upstream message ID for deduplication. If none exists,
   derive a versioned fingerprint from conversation ID, direction, normalized
   timestamp, message kind, and content digest. Collisions and edits remain
   possible and must be observable in diagnostics.
6. Persist new summaries/events and a monotonic local cursor transactionally.
7. Emit events in stored order. Flush stdout before reporting the final cursor.

The remote API supplies no resume token, so the kcli cursor is local. It is only
valid for the same account and state store. A cursor mismatch produces a clear
resync requirement, never an empty result that looks authoritative.

Delivery is **at least once**. Consumers deduplicate by `event_id`. Ordering is
guaranteed within one conversation after local observation, not across all
remote conversations. A fresh consumer normally uses `--since now` to establish
a baseline or `--since <timestamp>` for a bounded backfill.

## Event stream contract

`kcli dm watch --output ndjson` emits one UTF-8 JSON object per line. Stdout is
data only; progress, backoff, and authentication notices go to stderr.

```json
{"schema":"kcli.event/v1","event_id":"evt_…","cursor":"cur_…","type":"dm.conversation.updated","observed_at":"2026-09-03T12:00:00Z","account_id":"acct_…","conversation_id":"123","listing_id":"456","data":{"unread":true,"message_count":4,"preview":"Ist der Artikel noch …","complete":false}}
```

Initial event types are:

- `dm.conversation.created`
- `dm.message.created`
- `dm.conversation.read_changed`
- `dm.conversation.updated` when change is visible but cannot be represented
  more specifically
- `system.resync_required` when local continuity cannot be guaranteed

Events contain only bounded conversation previews and mark that data incomplete;
they never retain or emit full message bodies. `dm get` retrieves full history
live for that invocation. `--fields` can further reduce event fields. Logs never
contain message text. A heartbeat is emitted only with `--include-heartbeats`;
idle streams otherwise remain silent. `dm.message.created` is available only
when the caller explicitly enables `--open-changed`.

On `SIGINT` or `SIGTERM`, watch stops after the active request, commits any fully
observed event batch, writes no partial JSON line, and exits `130` for `SIGINT`.

## Polling policy

- Start with a 30-second interval plus approximately 10% random jitter.
- Allow a user to choose a slower interval; reject values below a conservative
  documented floor.
- Honor `Retry-After`. For `429`, `500`, and `503`, use exponential backoff with
  jitter and a 15-minute ceiling.
- On `401`, pause and attempt the normal token refresh once. If that fails, emit
  `auth_required` and stop polling until login succeeds.
- Do not retry conversation creation or message send after an ambiguous timeout.
- Cache stable category/location data separately; the DM loop does not refresh
  unrelated resources.
- Scan at most two conversation pages during an ordinary cycle and five pages
  during the bounded full reconciliation performed every 120 successful cycles.
  These conservative defaults remain provisional until authenticated tests
  prove remote ordering and density.

The older project-wide 2.5-second request spacing remains a per-request floor;
the 30-second value is the interval between normal DM synchronization cycles.

## Optional per-user daemon

The daemon is useful when several agent processes need the same inbox or when a
watch should survive a terminal closing. It is an optimization and reliability
layer, not a prerequisite for any core operation.

- `kcli daemon run` stays in the foreground. The operating-system service manager
  owns start, restart, and shutdown; kcli does not double-fork itself.
- Use a user launch agent on macOS, a systemd user service on Linux, and an
  equivalent per-user service on Windows.
- One daemon owns polling, the SQLite state database, deduplication, and token
  refresh for one local user. It can support multiple named Kleinanzeigen
  profiles without mixing their cursors.
- Local clients use versioned HTTP/1.1 JSON over a Unix-domain socket. The event
  response is NDJSON. This is easy to inspect and follows the familiar
  client/daemon-over-socket shape without exposing a TCP port.
- On Linux the socket lives beneath `XDG_RUNTIME_DIR`; on macOS and Windows use
  the platform's per-user runtime location. The directory is user-only and the
  socket is mode `0600` where modes exist.
- No TCP listener exists by default. Any future HTTP transport must bind to
  loopback, authenticate clients, validate origins, and document its larger
  exposure.

Minimum local service routes are versioned and private to kcli:

| Method and route | Purpose |
|---|---|
| `GET /v1/health` | Version, profile, last successful poll, and degraded state |
| `POST /v1/dm/poll` | Run or join one synchronization cycle |
| `GET /v1/dm/events?after=<cursor>` | Resume the NDJSON event stream |
| `POST /v1/command` | Execute a typed, schema-validated core operation |

The public CLI contract remains commands and JSON schemas, not these private
routes. A client with a missing or incompatible daemon falls back to the direct
core unless the caller supplied `--require-daemon`.

## MCP and agent integration

`kcli mcp serve` is the standard local agent surface: MCP JSON-RPC over stdio,
protocol messages on stdout, and logs on stderr. It reuses the command schemas,
returns bounded structured results, and connects to the daemon when available.
The first MCP surface should expose finite tools such as search, listing detail,
DM poll, DM read, reply dry-run, and confirmed reply. Continuous activity remains
in the daemon; an MCP client resumes it with a cursor rather than holding an
unbounded tool response open.

A response service should remain a separate consumer:

```text
kcli dm watch -> event consumer -> fetch context -> produce draft
                                              -> kcli dm reply --dry-run
                                              -> human confirmation -> send once
```

This keeps the CLI browser-equivalent and model-agnostic. The responder may
classify or draft, but listing text and DM text remain untrusted input. In
`0.1.0` it cannot bypass the confirmation ID or enable bulk/unattended sends.

## State, privacy, and recovery

- Store events, cursors, fingerprints, and confirmation plans in SQLite with
  transactional writes. Enable WAL only after testing platform and backup
  behavior.
- Keep tokens in the OS secret store and only opaque account/profile references
  in SQLite. Refresh-token environment injection is deferred until rotation and
  token-family behavior is proven.
- Do not persist complete message bodies or confirmation text. Retain bounded
  event previews for seven days by default and store only a digest for a pending
  confirmation. A later state purge is a deliberate local mutation with dry-run
  support.
- Redact email addresses in URLs, tokens, message bodies, and precise personal
  data from logs, crashes, and diagnostics.
- If corruption or a schema migration breaks continuity, preserve the old file,
  establish a new baseline, and emit `system.resync_required`.
- `kcli daemon status --output json` reports last success, backoff, cursor,
  queue depth, and schema version without returning message content.

## Why this is the conventional compromise

Foreground `watch` plus NDJSON follows established CLI composition practice and
needs no resident service. Incremental polling with an explicit cursor works in
schedulers and request/response agent protocols. A per-user, foreground daemon
managed by the OS follows the standard client/daemon pattern and prevents every
agent from polling independently. MCP stdio is the typed integration surface;
the Unix socket is an implementation detail for sharing state locally.

SSE would be reasonable for a future remote HTTP service, but offers no benefit
over NDJSON for local shell pipelines. A custom WebSocket or fake remote push
would add complexity without changing the polling limitation.
