# kcli command syntax

Status: implemented (offline); automated live testing and the `0.1.0` release remain gated
Current release: commands marked **v0.1**

This document fixes command names and argument shapes before implementation.
Commands are singular resource nouns followed by verbs. `search` is the one
intentional top-level verb because it is the product's primary action.

## Global contract

```text
kcli [--profile NAME] [--output FORMAT] [--fields LIST]
     [--quiet] [--debug] [--timeout DURATION]
     COMMAND
```

`FORMAT` is `table`, `json`, `ndjson`, or `raw` where meaningful. Terminals
default to a compact table or readable object. Non-TTY finite commands default
to one JSON envelope, including during pagination; event streams default to
NDJSON. Explicit finite NDJSON ends with a `kcli.summary/v1` line so stop and
count metadata is not lost. Diagnostics always go to stderr. `--fields` accepts
comma-separated schema paths and is available on every read command. Arbitrary
projection remains composable through an external `jq` process.

All durations use explicit suffixes such as `30s`, `5m`, or `1h`. `--timeout`
may lower but never raise the documented operation ceiling. Every command
supports `--help`; `kcli schema show` provides the machine-readable equivalent.
The later daemon layer adds mutually exclusive global `--no-daemon` and
`--require-daemon` controls; they are not needed in v0.1.

## Discovery commands

### Search listings — **v0.1**

```text
kcli search [QUERY]
  [--category ID_OR_PATH]
  [--location ID_OR_TEXT] [--radius KM]
  [--min-price EUR] [--max-price EUR]
  [--ad-type offered|wanted]
  [--picture-required]
  [--sort date-desc|price-asc|price-desc|distance-asc]
  [--filter KEY=VALUE]...
  [--exclude TEXT]...
  [--page NUMBER] [--page-size NUMBER]
  [--paginate] [--limit NUMBER]
  [--input FILE|-]
```

`--input` accepts a complete versioned search specification. It is mutually
exclusive with search-building flags so an agent cannot accidentally mix two
sources of truth. `--paginate` requires a bounded `--limit` unless a documented
safe default applies.

### Categories, locations, and filters — **v0.1**

```text
kcli category list [--refresh]
kcli category get ID_OR_PATH
kcli category search TEXT

kcli location resolve TEXT [--limit NUMBER]

kcli filter list --category ID_OR_PATH [--refresh]
kcli filter get --category ID_OR_PATH KEY
```

Filter output includes the metadata option key, upstream `search-param`
capability marker, `search-style`, type, legal values, labels, and whether
serialization has been live-proven.

## Listing commands

```text
kcli listing get ID_OR_URL [--raw]                         # v0.1
kcli listing images ID_OR_URL                             # v0.1
kcli listing images ID_OR_URL --download SELECTOR         # v0.1
  [--output-dir PATH] [--allow-outside-cwd EXACT_PATH]
  [--max-bytes BYTES] [--overwrite]
kcli listing open ID_OR_URL                               # v0.1
```

`listing images` lists every returned variant by default. `SELECTOR` is a
returned image index, exact relation name, or `all`, never an arbitrary URL.
Downloads stay under the current working directory unless the user explicitly
permits an exact outside path. `listing open` opens the official public URL and
has no machine-side marketplace effect.

## Seller commands

```text
kcli seller get ID_OR_URL                                  # v0.1
kcli seller get --listing LISTING_ID_OR_URL                # v0.1
kcli seller search NAME [--match exact|contains]           # v0.1
kcli seller listings ID_OR_URL [--limit NUMBER]            # v0.1
```

Every result includes `source` (`listing`, `profile-link`, or `local-index`) and
`completeness` (`direct`, `known-only`, or `best-effort`). `seller search` never
claims to search every Kleinanzeigen user. `seller listings` returns locally
known or explicitly linked listings until an anonymous seller-inventory endpoint
is proven.

## Authentication commands

```text
kcli auth login [--profile NAME] [--no-open]
  [--redirect-file FILE|-]                                 # v0.1
kcli auth status [--check]                                 # v0.1
kcli auth logout [--dry-run]                               # v0.1
```

Interactive login opens the system browser and completes Auth0 PKCE by accepting
the provider's final HTTPS redirect URL. `--redirect-file -` supports a
non-echoed stdin handoff without placing the single-use code in argv or shell
history. Refresh-token environment injection is deferred until rotation and
token-family behavior are verified.

## DM commands

### Read and synchronize — **v0.1**

```text
kcli dm list [--unread] [--page NUMBER] [--page-size NUMBER]
  [--paginate] [--limit NUMBER]
kcli dm get CONVERSATION_ID
kcli dm mark-read CONVERSATION_ID... [--dry-run]

kcli dm poll [--after CURSOR | --since TIME_OR_NOW]
  [--limit NUMBER] [--advance | --no-advance] [--open-changed]
kcli dm watch [--after CURSOR | --since TIME_OR_NOW]
  [--interval DURATION] [--limit NUMBER]
  [--include-heartbeats] [--open-changed]
```

`poll` runs once. Without an explicit cursor it uses the named profile's stored
cursor; first use requires `--since`. It advances durable state only after a
complete stored batch and successful stdout flush. `--no-advance` is available
for replay and diagnostics. The stored cursor head only moves forward: an
explicit older `--after` replays from that point but never rewinds the head.
`poll` is a finite command, so its output follows the global rules: one JSON
envelope containing the events and the resulting cursor, or, with explicit
NDJSON, event lines followed by a `kcli.summary/v1` line. Both commands observe
conversation-summary changes by default. `--open-changed` additionally calls
the state-touching conversation `PUT` to identify changed messages; help and
schemas label that side effect. `watch` repeats the same operation until
interrupted and emits `kcli.event/v1` NDJSON. Its `--interval` floor is the
30-second default; the flag can only slow polling down.

### Communicate — **v0.1**

```text
kcli dm reply CONVERSATION_ID
  (--message TEXT | --message-file FILE|- | --input FILE|-)
  --dry-run
  [--acknowledge-warning CODE]
  [--acknowledge-possible-duplicate PREVIOUS_CONFIRMATION_ID]
kcli dm reply CONVERSATION_ID
  (--message TEXT | --message-file FILE|- | --input FILE|-)
  --confirm CONFIRMATION_ID

kcli dm start LISTING_ID_OR_URL
  (--message TEXT | --message-file FILE|- | --input FILE|-)
  [--contact-name NAME]
  --dry-run
  [--acknowledge-warning CODE]
  [--acknowledge-possible-duplicate PREVIOUS_CONFIRMATION_ID]
kcli dm start LISTING_ID_OR_URL
  (--message TEXT | --message-file FILE|- | --input FILE|-)
  [--contact-name NAME]
  --confirm CONFIRMATION_ID
```

A dry run returns the exact account, recipient/conversation, listing context,
message preview, warnings, and a short-lived `confirmation_id`. The plan stores
the message digest, not the body. Confirmation must receive the exact message
and contact name again; changing any bound value invalidates it. No TTY or
non-TTY path bypasses the two commands. A platform warning requires a new dry
run with the exact warning code, and a prior ambiguous send requires explicit
acknowledgement of its confirmation ID after the user inspects the thread.

There are deliberately no `pickup`, `offer`, `negotiate`, `meeting`, `pay`, or
`transaction` commands. Those subjects are ordinary text passed to `dm reply` or
`dm start`.

## Schemas, configuration, and diagnostics

```text
kcli schema list                                            # v0.1
kcli schema show COMMAND...                                # v0.1
kcli schema filters --category ID_OR_PATH                  # v0.1

kcli config list                                           # v0.1
kcli config get KEY                                        # v0.1
kcli config set KEY VALUE [--dry-run]                      # v0.1
kcli config path                                           # v0.1

kcli doctor [--network] [--auth]                           # v0.1
kcli completion bash|fish|zsh                              # v0.1
kcli version                                               # v0.1
```

Schemas describe flags, JSON input, output fields, enums, required auth scope,
side effects, confirmation requirements, and whether an upstream contract is
live-proven. `doctor` checks configuration, secret-store access, public endpoint
compatibility, state health, and daemon compatibility without exposing secrets
or sending messages.

## Daemon and MCP commands — after v0.1

```text
kcli daemon run [--poll-interval DURATION]
kcli daemon install [--dry-run]
kcli daemon start
kcli daemon stop
kcli daemon restart
kcli daemon status
kcli daemon logs [--follow]
kcli daemon uninstall [--dry-run]

kcli mcp serve [--stdio]
```

`daemon run` is the actual foreground process; the other lifecycle commands
delegate to the per-user OS service manager. `mcp serve` uses stdio by default.
Neither opens a network listener unless a future, separately reviewed option
explicitly requests it.

## Later buyer-side browser parity

Names are reserved but not promised until useful endpoints are verified:

```text
kcli watchlist list
kcli watchlist add LISTING_ID_OR_URL
kcli watchlist remove LISTING_ID_OR_URL

kcli saved-search list|get|create|update|delete
kcli saved-search watch ID
```

Reporting, blocking, following, and attachments do not receive command names
until their endpoint and safety contracts are known. Selling and transaction
families will not be reserved.

## Stable exit behavior

| Exit | Meaning |
|---:|---|
| `0` | Complete success; partial/resource state is represented in output |
| `1` | Unclassified failure |
| `2` | Invalid command, identifier, input, schema, or a cursor that does not belong to this profile/store (`resync_required`) |
| `3` | Login required, expired, or revoked |
| `4` | Resource unavailable or not found |
| `5` | Retryable upstream or connectivity failure |
| `6` | Rate limited, by the service (`rate_limited`) or by the local cross-process reservation queue (`rate_limited_local`); retry metadata is in the structured error |
| `7` | Confirmation missing, expired, mismatched, warning-blocked, or unresolved-duplicate acknowledgement required |
| `8` | An external mutation may have succeeded; reconcile before any retry |
| `130` | Interrupted with `SIGINT` |
| `143` | Terminated with `SIGTERM` |

Every nonzero structured result has a stable error `code`, human `message`,
`retryable` boolean, and optional `retry_after`, `details`, and `request_id`.
Remote listing and message prose never appears in the error message field.
