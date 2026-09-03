# Endpoint coverage for the kcli scope

Status: design mapping, 3 September 2026
Endpoint contracts: [`mobile-api.md`](mobile-api.md)

This is the bridge between the scoped user stories, proposed commands, and the
private mobile calls available today. It intentionally distinguishes evidence
from aspiration.

## Evidence labels

- **Live** — exercised anonymously against the current service.
- **Source** — implemented and tested by the pinned upstream client, but not
  exercised here with a logged-in account.
- **Provisional** — metadata or response evidence exists, but kcli still needs a
  contract test for the intended use.
- **Local** — implemented from kcli state rather than a Kleinanzeigen endpoint.
- **Gap** — no useful endpoint is currently documented; do not invent one.

## `0.1.0` operations

| Scoped stories | User need | Proposed command | Mobile endpoint or source | Auth | Evidence | Important constraint |
|---|---|---|---|---|---|---|
| `V01-SEARCH-03` | Resolve a town or postcode | `kcli location resolve` | `GET /api/locations.json?q=` | No | Live | Returns location IDs used by search |
| `V01-SEARCH-02` | Browse or inspect categories | `kcli category list/get/search` | `GET /api/categories.json` plus cache lookup | No | Live + Local | Include full path and ID; refresh explicitly |
| `V01-SEARCH-05`, `V01-SEARCH-06` | Discover category filters | `kcli filter list/get` | `GET /api/ads/search-metadata/{category_id}.json` | No | Live | Preserve unknown types and raw metadata |
| `V01-SEARCH-01`, `V01-SEARCH-04`, `V01-SEARCH-07` | Search with common filters and sort | `kcli search` | `GET /api/ads.json` | No | Live | Common filters are proven |
| `V01-SEARCH-05`, `V01-SEARCH-06` | Apply a dynamic filter | `kcli search --filter` | Metadata option key/value serialized into `GET /api/ads.json` according to `search-style` | No | Provisional | Every release-snapshot searchable type/style needs a contract test |
| `V01-SEARCH-08` | Page and deduplicate results | `kcli search --page/--paginate` | `page`, `size`, `ads.value.paging.numFound` | No | Live + Local | Explicit result bound and deduplication |
| `V01-SEARCH-09` | Exclude unwanted text | `kcli search --exclude` | Title/description post-filter | No | Local | Label client-side filtering |
| `V01-SEARCH-10` | Preserve a reproducible search | `kcli search --input/--output json` | Versioned local schema around search parameters | No | Local | No translation loss or hidden defaults |
| `V01-LISTING-01`, `V01-LISTING-02`, `V01-LISTING-05`, `V01-LISTING-06`, `V01-LISTING-07`, `V01-LISTING-08` | Inspect a listing completely | `kcli listing get` | `GET /api/ads/{ad_id}.json` | No | Live | `404` means unavailable, not outage |
| `V01-LISTING-03` | Enumerate listing images | `kcli listing images` | `pictures.picture[].link[]` in listing data | No | Live | Image CDN links, not another API call |
| `V01-LISTING-04` | Download an image | `kcli listing images --download` | Selected returned CDN URL | No | Live | Safe output directory and size limits |
| `V01-USER-01`, `V01-USER-04`, `V01-USER-05` | Resolve listing seller | `kcli seller get --listing` | Seller fields embedded in listing detail | No | Live | Preserve source and completeness |
| `V01-USER-02`, `V01-USER-05` | Find a known seller | `kcli seller get <id-or-url>` | Known listing/profile links plus local records | No | Provisional | No global user endpoint |
| `V01-USER-03`, `V01-USER-05` | Search seller names | `kcli seller search` | Locally encountered seller index | No | Local | Never call it exhaustive/global search |
| `V01-USER-04`, `V01-USER-05` | List known seller listings | `kcli seller listings` | Local index and known public links | No | Local | Public `/api/users/{id}/ads.json` returned `401` |
| `V01-DM-01` | Begin interactive login | `kcli auth login` | `GET /authorize`, then `POST /oauth/token` | Login | Source | Auth0 authorization-code flow with PKCE |
| `V01-DM-01` | Refresh a session | Automatic / `kcli auth status` | `POST /oauth/token` refresh grant | Login | Source | Refresh token is password-equivalent |
| `V01-DM-01` | End the local session | `kcli auth logout` | Local keyring and state removal; no verified revocation endpoint | Local | Gap | Never claim remote token revocation |
| `V01-DM-01` | Resolve account ID | Internal auth setup | `GET /api/users/{email}/profile.json` | Yes | Source | Redact email from request logs |
| `V01-DM-02` | List DMs | `kcli dm list` | `GET …/users/{uid}/conversations` | Yes | Source | Page through and normalize wrapper variants |
| `V01-DM-03`, `V01-DM-07` | Read a DM thread with context | `kcli dm get` | `PUT …/conversations/{conversation_id}` | Yes | Source | This read may touch read/load state |
| `V01-DM-04`, `V01-DM-08` | Reply to a DM | `kcli dm reply` | `POST …/conversations/{conversation_id}` | Yes | Source | Plain text, dry run, bound confirmation, one attempt |
| `V01-DM-06` | Mark DMs read | `kcli dm mark-read` | `POST …/conversations/read?ids=` | Yes | Source | Account mutation, not external speech |
| `V01-DM-05`, `V01-DM-08` | Contact a listing | `kcli dm start` | `POST /api/users/{uid}/create-conversation/{ad_id}`, then reply | Yes | Source | Reconcile after ambiguous create; no blind retry |
| `V01-DM-09` | Detect new DM activity once | `kcli dm poll` | Conversation list by default; changed conversation reads only with `--open-changed` | Yes | Designed | Default mode must not mark a thread loaded/read |
| `V01-DM-10` | Stream new DM activity | `kcli dm watch` | Repeated list-only `dm poll` with local cursor/deduplication | Yes | Designed | It is synthesized polling, not server push; opening changes is opt-in |

The main API still requires the mobile application's distribution-level Basic
credential for nominally public requests. “Auth: No” means no Kleinanzeigen user
session, not no request authentication at all.

## Desired operation-to-endpoint reasoning

### Search and filters

The search command needs three discovery calls: categories identify a category,
locations identify a geographic ID, and search metadata supplies dynamic option
keys, capability markers, styles, and values. The final query goes to
`/api/ads.json`. `search-param` is a capability marker, not the query key.
Common filters have known parameter names. Dynamic filters stay provisional
until tests prove every searchable type/style in the release snapshot; kcli
must fail clearly rather than silently dropping a filter or guessing an
encoding.

### Listing and media

One listing-detail call supplies the description, attributes, seller fields,
public link, and all image variants. Image display requires no authenticated
Kleinanzeigen endpoint. A download follows an exact URL returned by the listing
and must not accept an arbitrary host through the listing command.

### Sellers

Seller identity is embedded in listings. There is no verified anonymous mobile
directory or arbitrary username-search call. The authenticated
`/api/users/{user_id}/ads.json` operation means “my ads” in the selected client;
an anonymous request for another seller returned `401`. Therefore direct and
local-index lookup satisfy v0.1 honestly, while global search remains a gap.

### Authentication and DMs

Login yields account tokens; the account-profile call yields the numeric user ID
required by message paths. Listing and opening conversations are separate calls.
Starting contact is also a two-call operation: create the conversation on the
main API, then send its first text through the gateway. This boundary matters
for confirmations, timeout reconciliation, and duplicate prevention.

## Relevant gaps and deferred browser parity

| Browser capability | Known endpoint | Product status |
|---|---|---|
| Read watchlist | `GET /api/users/{uid}/watchlist.json` | Known but deferred from `0.1.0` |
| Add/remove watchlist item | None | Gap; do not infer paths |
| Saved searches and alert settings | None | Gap |
| Message attachments | None | Gap |
| Report or block | None | Gap |
| Follow a seller | None | Gap |
| Global username search | None; [official free-site help says it is unavailable](https://hilfe.kleinanzeigen.de/hc/de/articles/17105730204060-Wie-kann-ich-bei-Kleinanzeigen-jemanden-suchen) | Explicit non-goal |
| Listing-management operations | Several known endpoints | Excluded product area |
| Offers, checkout, payments, or shipping | None in selected client | Excluded product area |

Any newly discovered endpoint enters [`mobile-api.md`](mobile-api.md) first with
its host, headers, payload, response, evidence, and risk. Only then may this map
and the release scope change.
