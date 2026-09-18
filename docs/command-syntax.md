# kcli command syntax

Release contract: anonymous-only `0.1.0`. See [acceptance evidence](test-results.md).

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
No daemon or background polling is included.

## Discovery commands

### Search listings — **v0.1**

```text
kcli search [QUERY]
  [--category ID_OR_PATH]
  [--location ID_OR_TEXT] [--radius KM]
  [--min-price EUR] [--max-price EUR]
  [--ad-type offered|wanted]
  [--picture-required] # unsupported by the public website; explicit error
  [--sort date-desc|price-asc|price-desc|distance-asc]
  [--filter KEY=VALUE]...
  [--exclude TEXT]...
  [--page NUMBER] [--page-size 25] # website controls page size
  [--paginate] [--limit NUMBER]
  [--input FILE|-]
```

`--input` accepts a complete versioned search specification. It is mutually
exclusive with search-building flags so an agent cannot accidentally mix two
sources of truth. `--paginate` requires a bounded `--limit` unless a documented
safe default applies. Search `next` is a zero-based page number advertised by
returned website links, or null when there is no continuation. If `--limit`
truncates a page, `next` is null, completeness is partial, and a `page_truncated`
warning identifies the page to rerun with a larger limit before advancing.
Thus null plus partial completeness must not be interpreted as exhaustion.

Numeric `--location` values are location IDs. Resolve a postcode with
`kcli location resolve POSTCODE` first, then pass the returned ID. Text place
names must resolve unambiguously.

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
serialization is supported. `public-web-advertised` means the encoding was
observed in website controls, not that every value has been live-tested. Enum
values use advertised choices; range values use `MIN,MAX` (one bound may be
empty); booleans use `true` or `false`. Repeat `--filter` for multiple keys;
commas inside a range remain part of that value. Unknown types and multiple
values for one enum are rejected explicitly.

## Listing commands

```text
kcli listing get ID_OR_URL [--raw]                         # v0.1
kcli listing images ID_OR_URL                             # v0.1
kcli listing images ID_OR_URL --download SELECTOR         # v0.1
  [--output-dir PATH] [--allow-outside-cwd EXACT_PATH]
  [--max-bytes BYTES] [--overwrite]
kcli listing open ID_OR_URL                               # v0.1
```

`listing images` lists the returned full-size gallery image URLs by default.
Thumbnail/srcset resolution variants are not an exhaustive media inventory. `SELECTOR` is a
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

Every result includes `source` (`public-web`, `listing`, `profile-link`, or `local-index`) and
`completeness` (`direct`, `known-only`, or `best-effort`). `seller search` never
claims to search every Kleinanzeigen user. `seller listings` reads one public inventory page and labels it best-effort.

Accepted seller references share one validator in CLI and application code:
numeric IDs, observed API `self-user` links, `/s-anbieter/<slug>/<id>` links,
and `/s-bestandsliste.html?userId=<id>` on the official website. Additional query
parameters, other hosts, and malformed IDs are rejected. Previously encountered sellers use the local index. An unknown numeric ID or
profile URL fetches its public profile; `seller listings` reads one bounded public
inventory page. Neither result claims exhaustive inventory coverage.

## Schemas, configuration, and diagnostics

```text
kcli schema list                                            # v0.1
kcli schema show COMMAND...                                # v0.1
kcli schema filters --category ID_OR_PATH                  # v0.1

kcli config list                                           # v0.1
kcli config get KEY                                        # v0.1
kcli config set KEY VALUE [--dry-run]                      # v0.1
kcli config path                                           # v0.1

kcli doctor [--network]                           # v0.1
kcli completion bash|fish|zsh                              # v0.1
kcli version                                               # v0.1
```

Schemas describe flags, JSON input, output fields, enums, required auth scope,
side effects, confirmation requirements, and whether an upstream contract is
live-proven. `doctor` checks local configuration, disk and state health. `--network` makes
a paced public category request. No login, keyring, or credentials are needed. `doctor`, `version`, and `config
path` fall back to CLI defaults when the configuration cannot be read. Doctor
reports broken configuration and state as individual checks; `--network` is
skipped when state is unavailable because safe pacing requires it.

Raw-payload fields describe arbitrary JSON values in runtime schemas, matching
their actual output; they are not arrays of byte integers.

`schema filters` reads the profile cache without network access. It returns
`overlay: "cached"`, key/value alternatives, enum constraints, and all observed
definitions (including unsupported ones) under `x-kcli-filter-metadata`.
`x-kcli-observed-at` and `x-kcli-stale` expose cache age. Missing cache is an
actionable schema error (exit 2): run `filter list --category ID --refresh` first.
The schema is descriptive; search still validates scalar types, combinations,
and serialization proof before sending a request.

## Stable exit behavior

| Exit | Meaning |
|---:|---|
| `0` | Complete success; partial/resource state is represented in output |
| `1` | Unclassified failure |
| `2` | Invalid command, identifier, input, schema,  |
| `3` | Reserved for authentication errors; no login flow in this release |
| `4` | Resource unavailable or not found |
| `5` | Retryable upstream or connectivity failure |
| `6` | Rate limited, by the service (`rate_limited`) or by the local cross-process reservation queue (`rate_limited_local`); retry metadata is in the structured error |
| `7` | Reserved; no remote mutations in this release |
| `8` | Reserved; no remote mutations in this release |
| `130` | Interrupted with `SIGINT` |
| `143` | Terminated with `SIGTERM` |

Every nonzero structured result has a stable error `code`, human `message`,
`retryable` boolean, and optional `retry_after`, `details`, and `request_id`.
Remote listing and message prose never appears in the error message field.
