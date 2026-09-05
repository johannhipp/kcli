# kcli

An agent-friendly command-line interface for the buyer/browser side of
Kleinanzeigen: search, listing and image inspection, best-effort seller lookup,
and explicitly confirmed direct messaging after login.

> [!IMPORTANT]
> kcli is under active implementation: the Go binary builds and its command,
> state, and transport logic are tested against redacted fixtures and fake
> servers. Automated live testing and release remain blocked until written
> Kleinanzeigen permission covers the intended use. It is unofficial and not
> endorsed by Kleinanzeigen.

**Status:** the offline implementation (phases 1–8 + hardening) is merged to
`main`; the `0.1.0` release is blocked on six external Phase 0 items. See
[Release status and blockers](docs/release-blockers.md) for the exact
done/blocked split.

## Planned `0.1.0`

- Anonymous listing discovery with all filters advertised by mobile metadata.
- Complete listing, image, public-link, and seller-context inspection.
- Local, clearly labeled seller-name lookup over previously encountered data.
- Auth0 PKCE login and inbox reading.
- Previewed, explicitly confirmed replies and first contact; no unattended
  sending.
- Bounded, list-only DM polling and foreground watching for agent workflows.

Selling, offers, checkout, shipping, payment, and structured pickup workflows
are outside the release boundary. Pickup and payment-on-collection details are
ordinary text exchanged by users in chat.

## Start here

| Document | Purpose |
|---|---|
| [v0.1 scope](docs/v0.1-scope.md) | Authoritative release boundary and 33 scoped user stories |
| [Implementation plan](docs/implementation-plan.md) | Go stack, architecture, phases, estimates, tests, and release gates |
| [Command syntax](docs/command-syntax.md) | Proposed command, flag, output, confirmation, and exit-code contract |
| [Mobile API reference](docs/mobile-api.md) | Useful undocumented endpoints and evidence status |
| [Documentation index](docs/README.md) | All product, API, synchronization, and research documents |
| [Contributing](CONTRIBUTING.md) | Repository workflow and required checks |

The repository is documentation-led: every contract is specified before code,
and the release gate requires live evidence (permission + two authorized test
accounts) that is external to the repository.

Build and test the Go implementation:

```bash
go build ./...
go test -tags testing -race ./...
```

Validate documentation contracts locally with:

```bash
python3 scripts/check_docs.py
```

Changes use [Semantic Versioning](https://semver.org/) and
[Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). Pending
user-visible changes belong under `Unreleased` in [CHANGELOG.md](CHANGELOG.md).

## Permission boundary

Kleinanzeigen's
[`Nutzungsbedingungen`, section 5](https://themen.kleinanzeigen.de/nutzungsbedingungen/?locale=de-DE)
prohibit unapproved automated access. Automated live testing and distribution
are blocked until explicit written permission covers the intended use. Do not
bypass access controls, change networks to evade a block, or treat a local risk
acknowledgement as permission.

> [!NOTE]
> Everything from **Research conclusion** down to the end of this file is the
> **historical technical investigation** (September 2026) that informed the
> design. It is **superseded by the implemented kcli Go CLI** and retained only
> as provenance. The `kcli` binary is the product; the Python research snippets
> and `scripts/smoke_*.py` below are **research-only** — do not use them for
> live testing without written permission, and do not treat them as part of the
> build.

## Research conclusion

Research snapshot: 2 September 2026; implementation plan updated 3 September
2026. This is a technical suitability review, not legal advice.

There is no single established project that currently provides a dependable,
agent-friendly version of the whole Kleinanzeigen product.

The best technical base I found is
[`monkrel/kleinanzeigen-api`](https://github.com/monkrel/kleinanzeigen-api). Its
anonymous search and detail API worked in a live test, and its code covers a
meaningful subset of account functions. It is nevertheless a very young,
single-maintainer client for undocumented mobile endpoints. The embedded app
credentials or endpoint shapes can change without warning, and the logged-in
surface has not been proven against a real account in this investigation.

For seller-side ad maintenance,
[`Second-Hand-Friends/kleinanzeigen-bot`](https://github.com/Second-Hand-Friends/kleinanzeigen-bot)
is much more mature, but it is browser automation rather than a buyer/search
CLI, has no chat support, and is AGPL-3.0.

Several projects that parse public HTML were broken in the live test by a
current site-markup migration. That is the clearest practical argument against
building the core on CSS selectors alone.

Most importantly, Kleinanzeigen's
[`Nutzungsbedingungen`, section 5](https://themen.kleinanzeigen.de/nutzungsbedingungen/?locale=de-DE)
prohibit crawlers, spiders, scrapers, other automated access, access-control
bypass, and excessive load without written permission. Technical feasibility
therefore does not equal safe operational use. An unattended production agent
should only be pursued with explicit permission from Kleinanzeigen.

## What was tested

- Anonymous use in a fresh browser session: search, filters, result cards,
  thumbnails, listing detail, description, attributes, seller summary, and the
  full image gallery all rendered without an account. The contact action was a
  login-gated control.
- `monkrel/kleinanzeigen-api` 0.4.0: a live Berlin search returned structured
  listings with descriptions, prices, location, timestamps, categories,
  attributes, and image URLs. Its 71 repository tests passed.
- [`its-me-prash/kleinanzeigen-reader`](https://github.com/its-me-prash/kleinanzeigen-reader):
  a live listing read returned its content and eight working JPEGs; 27 tests
  passed. Installation needed a clean virtual environment because its declared
  Python range conflicts with the optional MCP dependency.
- [`taneron/willmehr`](https://github.com/taneron/willmehr): build and typecheck
  passed, but its own live smoke test failed at search. The fetched page was a
  normal HTTP 200 containing 27 ads, but its legacy selectors found none.
- [`DanielWTE/ebay-kleinanzeigen-api`](https://github.com/DanielWTE/ebay-kleinanzeigen-api):
  the Playwright-backed listing-detail endpoint worked, including images and
  seller data; search returned zero because it uses the same legacy selectors.
- [`jonasehrlich/ek-scraper`](https://github.com/jonasehrlich/ek-scraper): its
  current parser found zero ads on the same live results page.
- [`orangecoding/fredy`](https://github.com/orangecoding/fredy): inspected rather
  than installed end to end. Its current Kleinanzeigen provider still targets
  the legacy result-list selectors, so it is very likely affected too. Fredy is
  also housing-specific rather than a general marketplace CLI.
- `Second-Hand-Friends/kleinanzeigen-bot`: after applying the repository's
  documented dependency patch, 1,532 tests passed, 5 skipped. Current issues
  show that Kleinanzeigen's redesigned detail page is already causing missing
  shipping and category attributes.
- Auth failure behavior in `monkrel/kleinanzeigen-api`: with an isolated empty
  token directory, account commands exited with status 2 and made no token
  file. Login URL construction, token handling, and authenticated operations
  were assessed from source and tests only; no real account was used.

Listings can disappear between search and detail. One sampled listing changed
to reserved/deleted during the test, so the smoke script treats that as normal
marketplace volatility and tries another result.

## Candidate comparison

Legend: **Yes** = live-verified here; **Code** = implemented/documented but not
verified live here; **Broken** = attempted live and failed; **No** = absent.

| Project | Search | Detail + images | Monitoring | Agent interface | Account features | Health / fit |
|---|---:|---:|---:|---:|---:|---|
| `monkrel/kleinanzeigen-api` | **Yes** | **Yes** | Code | CLI + Python | Code | Best overall starting point; young/private API risk |
| `kleinanzeigen-reader` | No | **Yes** | Limited price tracking | CLI + MCP + Python | Limited session counters | Good single-listing component, not a marketplace client |
| `willmehr` | **Broken** | Not relied on | No | MCP | No | Nice agent shape, but currently unusable for search |
| `ebay-kleinanzeigen-api` | **Broken** | **Yes** | No | HTTP API | No | Heavy browser dependency; awkward for a small CLI |
| `ek-scraper` | **Broken** | Not tested | Notifications | Python/cron | No | Focused monitor, but current parser is stale |
| `Fredy` | Likely broken for Kleinanzeigen | Not tested | Strong | Web UI + MCP | No | Mature housing monitor, not general marketplace |
| `kleinanzeigen-bot` | No | Own ads only | No | CLI/config | Seller ad management | Mature adjunct, not buyer/discovery foundation |

## Anonymous feature matrix

| Capability | Official public site | `monkrel` | Reader | `willmehr` | DanielWTE | `ek-scraper` | Fredy |
|---|---:|---:|---:|---:|---:|---:|---:|
| Keyword/category search | Yes | **Yes** | No | **Broken** | **Broken** | **Broken** | Likely broken |
| Location/radius/price filters | Yes | **Yes** | No | Broken with search | Broken with search | Broken with search | Housing filters, likely broken |
| Sort and pagination | Yes | **Yes** | No | Broken with search | Broken with search | Broken with search | Code |
| Listing title/price/description | Yes | **Yes** | **Yes** | Not relied on | **Yes** | Not tested | Not tested |
| Attributes/location/seller summary | Yes | **Yes** | **Yes** | Not relied on | **Yes** | Not tested | Not tested |
| Gallery/image URLs | Yes | **Yes** | **Yes** | Not relied on | **Yes** | Not tested | Not tested |
| Image download | Browser | URL available | **Yes** | Code | URL available | No | No |
| Structured JSON | No | **Yes** | **Yes** | Intended | **Yes** | Internal | Internal |
| Deduplicated notifications | Official saved-search feature requires account | Code | Limited | No | No | Code | Code |

## Logged-in feature matrix

These cells describe project surface area, not live validation. The official
site supports more account functionality than any project below.

| Capability | `monkrel` | Reader | `kleinanzeigen-bot` | Other surveyed tools |
|---|---:|---:|---:|---:|
| Auth0 login + refresh | Code | No; bring your own session | Browser session | No |
| List conversations / read messages | Code | No | No | No |
| Reply in an existing conversation | Code | No | No | No |
| Start a conversation | Library code; not CLI | No | No | No |
| Mark conversations read | Library code; not CLI | No | No | No |
| List watchlist | Code | No | No | No |
| Add/remove watchlist items | No | No | No | No |
| Manage saved searches/alerts | No | No | No | No |
| List own ads, including paused | Code | Limited own-paused read | Code | No |
| Create an ad | Code | No | Code | No |
| Upload local ad images | No; already-hosted URLs only | No | Code | No |
| Edit ad content | No | No | Code | No |
| Pause / activate / renew / delete | Code | No | Code | No |
| Offers, direct buy, payment, shipping | No | No | No | No |
| User follow, block/report, profile settings | No | No | No | No |

The official help center confirms that posting requires an account and documents
saved-search alerts, watchlists, messaging moderation, and the "Sicher bezahlen"
offer/direct-buy flow. Relevant references:

- [Create an ad](https://hilfe.kleinanzeigen.de/hc/de/articles/17083044980764-Wie-kann-ich-eine-Anzeige-aufgeben)
- [Saved-search notifications](https://hilfe.kleinanzeigen.de/hc/de/articles/17102975598492-Ich-m%C3%B6chte-automatisch-neue-Suchergebnisse-zu-meiner-Suche-erhalten-Wie-richte-ich-das-ein)
- [Notification/watchlist overview](https://hilfe.kleinanzeigen.de/hc/de/articles/17128455544092-Wie-kann-ich-Benachrichtigungen-abbestellen)
- [Report or block through messages](https://hilfe.kleinanzeigen.de/hc/de/articles/17153500099868-Wie-melde-ich-eine-unangemessene-Nachricht)
- [Buying with "Sicher bezahlen"](https://hilfe.kleinanzeigen.de/hc/de/articles/17211974127004-So-funktioniert-der-Kauf-mit-Sicher-bezahlen)

## Maintainability assessment

1. **Best proof-of-concept base: `monkrel/kleinanzeigen-api`.** It is the only
   candidate that joined working search/detail data with meaningful chat and
   seller-account code. Treat it as an experimental adapter, not a stable API.
   Pin its version, keep request volume very low, and add a daily read-only
   contract test before depending on it only if written permission covers that
   automated test.
2. **Useful adjunct: `kleinanzeigen-bot`.** Adopt only if managing one's own
   listings is a core requirement and AGPL-3.0 is acceptable. The large test
   suite and contributor base are positives; redesign-related open issues
   [#1257](https://github.com/Second-Hand-Friends/kleinanzeigen-bot/issues/1257)
   and [#1256](https://github.com/Second-Hand-Friends/kleinanzeigen-bot/issues/1256)
   show the continuing maintenance cost.
3. **Useful narrow component: `kleinanzeigen-reader`.** It is attractive for an
   agent that receives a listing URL and needs structured content or images. It
   does not solve discovery or chat.
4. **Do not adopt the current HTML-search implementations as the core.** Four
   candidates converged on old selectors and failed together. Their architecture
   is useful as reference material, but the parsers need a rewrite and recurring
   live contract tests.
5. **Do not automate transactions.** None of the projects covers offers,
   payment, shipping, or dispute flows. Those are high-risk and should stay in
   the official UI.

For a maintainable internal prototype, put a small adapter around `monkrel`,
store search criteria outside the library, and make every messaging or listing
mutation require a human preview and explicit approval. Avoid its aggressive
"frontier" watcher: the documented 15–20 requests per second conflicts sharply
with a low-load posture. A normal, infrequent search poll is the least-bad
technical experiment—but still requires Kleinanzeigen's permission for compliant
operation.

## Reproducible smoke checks

The two scripts are deliberately small and pin the tested package version:

Do not run them without written permission covering the intended automated
access. `--acknowledge-terms-risk` is an accidental-execution guard, not a
substitute for permission.

```bash
# Anonymous: search, detail, and one image response.
uv run --with kleinanzeigen-api==0.4.0 python scripts/smoke_anonymous.py \
  --acknowledge-terms-risk

# Authenticated but read-only: counts chats, own ads, and watchlist entries.
# This requires the package's one-time login and was not run in this review.
uv run --with kleinanzeigen-api==0.4.0 python \
  scripts/smoke_authenticated_readonly.py --acknowledge-terms-risk
```

Neither script schedules itself. The authenticated script contains no message,
post, edit, renew, pause, activate, or delete call.

## Supply-chain note

Search results also surfaced a supposed API project whose instructions asked
users to disable antivirus, download a password-protected archive, and run it as
administrator. It was intentionally not cloned or executed. Repository popularity
is not enough: inspect install instructions and source before trying any project
in this ecosystem.
