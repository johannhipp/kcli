# Release verification

Target: anonymous-only `0.1.0`, macOS/Linux amd64/arm64. Run: 18 September 2026.
The prior mobile-backed acceptance notes are superseded by these release checks.
The operator-confirmed permission recorded in the September 6 research run
covers the anonymous testing context. No login or message transmission occurred.

## Offline and build checks

| Check | Evidence |
|---|---|
| Full suite | `make check`: formatting, vet, race/shuffle tests across all packages, staticcheck, govulncheck and documentation passed; no reported vulnerabilities |
| Dependency integrity | `go mod verify` passed |
| Real CLI/web boundary | `TestPublicWebCLIJourney` exercises real argv, production web transport and reopened state through intercepted HTTP; verifies deadlines, public hosts and no authorization headers |
| CLI smoke | All 23 exposed commands have working help and schemas; bash/zsh/fish completion, local doctor and structured invalid-input errors passed |
| Supported platforms | macOS/Linux amd64/arm64 cross-builds and GoReleaser snapshot archives passed; SHA-256 checksums generated |
| Local execution | macOS arm64 binary exercised; Linux runtime suite is run by GitHub CI before publication |
| Release automation | Release unit tests, actionlint and GoReleaser config validation passed; temporary bare-remote tests covered atomic push races and immutable tag reuse |
| Website parsing breadth | All 159 prior category captures parsed offline (573 filter definitions); captures remain ignored and are not test fixtures |

Permanent tests use synthetic fixtures. CI never runs live Kleinanzeigen probes.

## Paced live CLI walkthrough

Requests ran serially, with at least 2.5 seconds plus jitter between HTTP request
starts in shared state and three-second pauses between walkthrough commands.
Each CLI call had a deadline and a 40-second outer timeout. Captures and downloads
remain in ignored `.tmp/`; no personal listing data is committed.

- Category browsing: 162 entries; location suggestions: 10 Berlin matches;
  postcode lookup: three matches.
- Combined keyword/category/location/radius/price search returned five results.
- Pagination returned 30 deduplicated listings over multiple pages.
- Bicycle metadata returned four filter definitions; car metadata returned 30.
- Combined bicycle type/condition enums and car range/navigation boolean filters
  returned results and passed selected-state verification.
- Date/default, ascending price and distance sorts, wanted ads, and a deliberate
  empty search returned valid structured responses.
- Listing details and raw extracted JSON, four gallery image URLs, one bounded
  actual image download, and official browser opening passed.
- Linked seller lookup, direct public-profile lookup, one-page seller inventory,
  persisted profile lookup and exact local seller-name search passed.
- `doctor --network` parsed a fresh public category response successfully.

These are representative journeys, not exhaustive combinations or proof that
all promoted results obey ordering/filter semantics. Descending-price mapping,
malformed contracts, missing resources, request timeouts, rate limits, cooldowns,
unsafe paths and unsupported inputs are covered offline. No access controls were
bypassed, no account was used, and missing resources are not blindly retried.

## Regressions found and fixed

- Numeric location IDs were incorrectly passed to text suggestions; numeric
  references now remain IDs, while postcodes use explicit location resolution.
- Kong split comma-separated range bounds; repeated filters now preserve values.
- Website condition metadata uses an explicit alias in selected state; verified
  values are accepted without weakening checks for missing or mismatched filters.
- Commercial listings expose seller IDs in a public data attribute even when
  no profile anchor exists; the parser recognizes that observed form.
- Direct profiles were not persisted and sparse inventory previews could erase
  richer seller names/metadata; the local index now retains that information.
- Relative image destinations were expanded prematurely into absolute paths;
  argument parsing now preserves the download authorization boundary.

## Known release limits

No authentication/DM commands. No global username directory. Seller inventory
covers one page. Picture-required filtering and custom website page sizes are
unsupported. Media output covers available gallery images, not every thumbnail
resolution. Unknown listing IDs require a full public URL until encountered.
Unsigned macOS binaries may require operating-system approval. Website markup
and inventory can change after this verification.
