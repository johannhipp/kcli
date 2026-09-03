# Kleinanzeigen undocumented mobile API

Status: research snapshot, 3 September 2026
Upstream reference: [`monkrel/kleinanzeigen-api` v0.4.0](https://github.com/monkrel/kleinanzeigen-api/tree/efb2d82bf449c38a49558b8c71df8d888effbfd9)

This document records every endpoint from the selected client that is useful to
this project. It is a practical integration reference, not an official API
contract. Anonymous search, listing detail, location lookup, and returned image
URLs were exercised live. Authenticated endpoints are documented from upstream
source and tests but have not been exercised with a real account here.

Kleinanzeigen's
[`Nutzungsbedingungen`, section 5](https://themen.kleinanzeigen.de/nutzungsbedingungen/?locale=de-DE)
prohibit automated access without written permission. Obtain permission before
operating this integration, use conservative request rates, and keep all account
mutations behind explicit human approval.

## Hosts

| Name | Base URL | Purpose |
|---|---|---|
| Main API | `https://api.kleinanzeigen.de` | Search, listings, metadata, account lookup, watchlist, and own-ad operations |
| Message gateway | `https://gateway.kleinanzeigen.de` | Conversation listing, reading, replies, and read state |
| Login | `https://login.kleinanzeigen.de` | Auth0 Authorization Code + PKCE login and token refresh |
| Public website fallback | `https://www.kleinanzeigen.de` | Location autocomplete fallback only; not a mobile endpoint |

## Transport evidence

The pinned Python client uses `curl_cffi.Session(impersonate="chrome")` and says
ordinary HTTP clients are rejected because their TLS signature differs. A
standard Go `net/http` probe from the current development network instead
received Kleinanzeigen's explicit temporary IP-range block page on 2 September
2026. That result does not isolate TLS compatibility. Testing stopped at the
block; do not change IPs, rotate fingerprints, use proxies, or solve challenges.

After written permission is obtained and the block has naturally cleared, test
the pinned Python reference and standard Go client from the same network, in
that order and at minimal volume. If standard Go fails while the reference is
accepted, test one fixed, documented compatibility profile. A block or
challenge is a stop condition, not an invitation to try more identities.

## Endpoint inventory

`Public` below means that no Kleinanzeigen user account is needed. Main-API
calls still require the mobile app's distribution-level Basic authorization.

| Method | Path | Access | Effect | Project use |
|---|---|---|---|---|
| `GET` | `/api/locations.json` | Public | Read | Resolve a human place name to a location ID |
| `GET` | `/api/categories.json` | Public | Read | Refresh the category tree |
| `GET` | `/api/ads.json` | Public | Read | Search and filter listings |
| `GET` | `/api/ads/search-metadata/{category_id}.json` | Public | Read | Discover category-specific filters and values |
| `GET` | `/api/ads/{ad_id}.json` | Public | Read | Fetch one full listing and its image URLs |
| `GET` | `/authorize` | Browser login | Starts auth | Begin Auth0 PKCE login |
| `POST` | `/oauth/token` | Login | Token write | Exchange an authorization code or refresh token |
| `GET` | `/api/users/{email}/profile.json` | User | Read | Resolve the numeric user ID needed by account endpoints |
| `GET` | `/messagebox/api/users/{user_id}/conversations` | User | Read | List conversation threads |
| `PUT` | `/messagebox/api/users/{user_id}/conversations/{conversation_id}` | User | State-touching read | Load a conversation and its messages |
| `POST` | `/messagebox/api/users/{user_id}/conversations/{conversation_id}` | User | **Write** | Send a text reply |
| `POST` | `/messagebox/api/users/{user_id}/conversations/read` | User | **Write** | Mark conversations read |
| `POST` | `/api/users/{user_id}/create-conversation/{ad_id}` | User | **Write** | Start a conversation with a seller |
| `GET` | `/api/users/{user_id}/watchlist.json` | User | Read | List saved listings |
| `GET` | `/api/users/{user_id}/ads.json` | User | Read | List the user's own active and paused ads |
| `GET` | `/api/users/{user_id}/ads/{ad_id}.json` | User | Read | Fetch one own ad, including a paused ad |
| `POST` | `/api/users/{user_id}/ads.json` | User | **Write** | Create a new ad from XML |
| `PUT` | `/api/users/{user_id}/ads/paused/{ad_id}.json` | User | **Write** | Pause an ad |
| `PUT` | `/api/users/{user_id}/ads/active/{ad_id}.json` | User | **Write** | Reactivate an ad |
| `GET` | `/api/users/{user_id}/ads/extend/status` | User | Read | Check renewal eligibility |
| `POST` | `/api/users/{user_id}/ads/extend/{ad_id}` | User | **Write** | Renew or extend an ad |
| `DELETE` | `/api/users/{user_id}/ads/{ad_id}` | User | **Destructive** | Permanently delete an ad |

## Request identity and authentication

### Common mobile headers

The tested client impersonates the Android app and sends these headers to the
main API:

```http
X-EBAYK-APP: <install-uuid><client-creation-unix-time-ms>
X-ECG-USER-AGENT: ebayk-android-app-<app-version>
X-ECG-USER-VERSION: <app-version>
User-Agent: Kleinanzeigen/<app-version> (Android 13; Pixel 7)
Accept: application/json
Accept-Language: de-DE
Authorization: Basic <base64-app-distribution-credentials>
```

The reference client creates the complete `X-EBAYK-APP` value once per client
installation; its timestamp suffix is not regenerated per request. It used app
version `2026.25.0`. Treat both as compatibility values, not permanent
constants, and re-check them during the authorized phase-0 contract capture.

The Basic credential identifies the app distribution; it is not the user's
login. Its concrete value is intentionally not copied into this repository.
Supply current values through:

```text
KLEINANZEIGEN_BASIC_USER
KLEINANZEIGEN_BASIC_PW
```

The upstream client contains fallbacks, but they can rotate. A `401` or `403` on
otherwise-public main-API calls is a strong signal that the app credential or
required headers changed.

### Logged-in main-API headers

Authenticated calls to `api.kleinanzeigen.de` retain the Basic header and add:

```http
X-EBAYK-USERID-TOKEN: <user-access-token>
X-ECG-Authorization-User: email=<account-email>,access=<user-access-token>
```

### Logged-in gateway header

Calls to `gateway.kleinanzeigen.de` use conventional bearer authentication:

```http
Authorization: Bearer <user-access-token>
```

They also use the common app/version/user-agent headers above, excluding the
main API's Basic and `X-ECG-Authorization-User` values.

### Auth0 PKCE flow

#### `GET https://login.kleinanzeigen.de/authorize`

Starts interactive account login.

Query parameters:

| Parameter | Value |
|---|---|
| `client_id` | Mobile application's Auth0 client ID; obtain from the current client configuration |
| `response_type` | `code` |
| `redirect_uri` | `https://login.kleinanzeigen.de/android/com.ebay.kleinanzeigen/callback` |
| `scope` | `openid email profile offline_access` |
| `code_challenge` | Base64url SHA-256 hash of the PKCE verifier |
| `code_challenge_method` | `S256` |
| `state` | Fresh random CSRF value; validate it on return |
| `nonce` | Fresh random OIDC nonce; validate it in the ID token |
| `prompt` | `login` |

Keep the verifier and state in short-lived local process state. Never log the
returned authorization code.

#### `POST https://login.kleinanzeigen.de/oauth/token`

Exchanges the returned code:

```json
{
  "grant_type": "authorization_code",
  "client_id": "<mobile-client-id>",
  "code": "<authorization-code>",
  "code_verifier": "<original-pkce-verifier>",
  "redirect_uri": "https://login.kleinanzeigen.de/android/com.ebay.kleinanzeigen/callback"
}
```

Refreshes an expired access token:

```json
{
  "grant_type": "refresh_token",
  "client_id": "<mobile-client-id>",
  "refresh_token": "<refresh-token>"
}
```

Expected response fields are `access_token`, `expires_in`, optional rotated
`refresh_token`, and optional `id_token`. The email used by the reference client
comes from the ID token. Refresh one minute before expiry.

The exact OIDC issuer, audience, discovery document, and token-endpoint client
authentication style must be captured during the authorized login test. kcli
must validate signature, issuer, audience, expiry, and nonce and must configure
the observed token auth style explicitly so an auto-detection retry cannot
repeat a state-changing code exchange.

Store refresh tokens like passwords. The selected client defaults to
`~/.kleinanzeigen_api/token.json`, attempts file mode `0600`, and supports
`KLEINANZEIGEN_TOKEN_DIR` or `KLEINANZEIGEN_REFRESH_TOKEN`. For this project,
prefer the operating environment's secret store over a repository file.

## Public discovery endpoints

### Resolve locations

```http
GET https://api.kleinanzeigen.de/api/locations.json?q=Berlin
```

| Query | Required | Notes |
|---|---:|---|
| `q` | Yes | Place name or postcode text |

The response is a nested location tree under the CAPI namespace
`http://www.ebayclassifiedsgroup.com/schema/location/v1`. Useful fields are
location `id`, `localized-name`, `id-name`, and nested `location` children.
Search endpoints expect the returned numeric ID, not the display label.

If this endpoint fails, the upstream client falls back to the public website
autocomplete endpoint documented in the appendix.

### Fetch categories

```http
GET https://api.kleinanzeigen.de/api/categories.json
```

Returns the current hierarchical category catalog. Store both category IDs and
localized paths. Category IDs are required for precise search, metadata lookup,
and posting.

This endpoint is more suitable for refreshing a local catalog occasionally than
for calling on every search.

### Search listings

```http
GET https://api.kleinanzeigen.de/api/ads.json?page=0&size=25&q=ThinkPad&locationId=<id>&distance=50&sortType=DATE_DESCENDING
```

| Query | Required | Values / meaning |
|---|---:|---|
| `page` | Yes | Zero-based page number |
| `size` | Yes | Results per page; the tested client uses at most about 25 |
| `categoryId` | No | Numeric category ID; omit for all categories |
| `locationId` | No | Numeric location ID; omit for all Germany |
| `distance` | No | Radius in kilometres |
| `minPrice` | No | Minimum euro price |
| `maxPrice` | No | Maximum euro price |
| `adType` | No | `OFFERED` or `WANTED`; client default is `OFFERED` |
| `q` | No | Server-side keyword query |
| `pictureRequired` | No | String `true` to require a picture |
| `sortType` | No | `PRICE_ASCENDING`, `PRICE_DESCENDING`, `DATE_DESCENDING`, or `DISTANCE_ASCENDING` |

The total is found at `ads.value.paging.numFound` inside the ad namespace. The
listing collection is `ads.value.ad`; when only one result exists it may be an
object rather than an array.

Useful result fields:

| Normalized field | API source |
|---|---|
| `id` | `id` |
| `title` | `title` |
| `description` | `description` |
| `price`, `price_type` | `price.amount`, `price.price-type` |
| `city`, `zip_code` | `ad-address.state`, `ad-address.zip-code` |
| `latitude`, `longitude` | `ad-address.latitude`, `ad-address.longitude` |
| `posted` | `start-date-time` |
| `poster_type` | `poster-type` |
| `category_id` | `category.id` |
| Public URL | `link` entry whose `rel` is `self-public-website` |
| Images | `pictures.picture[].link[]`; prefer `XXL`, `large`, then `teaser` |
| Attributes | `attributes.attribute[]` using `localized-label` or `name` and its first `value` |

The live detail response also exposed useful seller fields that the selected
client does not yet normalize: `contact-name`, `contact-name-initials`,
`user-id`, `seller-account-type`, `user-since-date-time`, `user-rating`,
`userBadges`, and a `self-user` link. Preserve these for v0.1 seller inspection
and local seller indexing. A public unauthenticated request to the corresponding
`/api/users/{user_id}/ads.json` path returned `401` in the same check, so it is
not a proven public seller-inventory endpoint.

The API wraps many scalars in one or more `{ "value": ... }` objects. Parsing
must recursively unwrap those values. HTML entities can appear in titles and
descriptions and should be decoded after parsing.

Listings are volatile: a result may be deleted between the search and detail
request. Treat a detail `404` as normal rather than as an integration outage.

### Discover category-specific search filters

```http
GET https://api.kleinanzeigen.de/api/ads/search-metadata/{category_id}.json
```

Returns filter definitions under the namespace
`http://www.ebayclassifiedsgroup.com/schema/ad/v1` key
`ads-search-options`. Each option can contain:

- `localized-label`
- `type`
- `search-param`, an optional/required/unsupported capability marker rather
  than the outgoing parameter name
- `search-style`, such as `eq` or `in`
- `supported-value[]`, each with `value` and `localized-label`

Use this endpoint to drive dynamic filters rather than hard-coding every
category attribute. The metadata option object's key is the candidate outgoing
query parameter. `search-param` describes whether that option participates in
search, and `search-style` describes how its value is expressed. This mapping is
source-backed but each observed type/style and its exact serialization still
needs an authorized contract test. Preserve the raw option and fail visibly for
post-release drift; do not silently omit an advertised filter or invent range
syntax.

### Fetch one listing

```http
GET https://api.kleinanzeigen.de/api/ads/{ad_id}.json
```

Returns one listing with the same fields described under search, usually wrapped
under the ad namespace key. This is the preferred endpoint for description,
attributes, coordinates, full public URL, and the image gallery after discovery.

Expected status behavior:

| Status | Interpretation |
|---|---|
| `200` | Listing returned |
| `404` | Missing, expired, or deleted listing; expected marketplace state |
| `401` / `403` | App-distribution authentication or header contract likely changed |
| `429` | Slow down; do not try to bypass the limit |
| `5xx` | Temporary upstream error; retry sparingly with backoff and jitter |

## Account identity

### Resolve email to numeric user ID

```http
GET https://api.kleinanzeigen.de/api/users/{url_encoded_email}/profile.json
```

Access: authenticated main API.
Purpose: obtain `data.id` or top-level `id`. Most account URLs require this
numeric user ID. Cache it for the lifetime of the token/account rather than
resolving it before every operation.

An email address in a URL is personal data. Ensure request logs redact the path.

## Messaging endpoints

Messaging lives primarily on the gateway host. Because messages are external
communications, every send and new-conversation action should require a human
preview and explicit confirmation.

### List conversations

```http
GET https://gateway.kleinanzeigen.de/messagebox/api/users/{user_id}/conversations?page=0&size=100
```

The response may expose the list as top-level `conversations`, top-level `data`,
or nested `data.conversations` / `data.items`. Useful thread fields observed in
the client parser are:

- `id`, `adId`, `adTitle`, `role`
- `sellerName`, `buyerName`
- `unread`, `unreadMessagesCount`
- `receivedDate`, `textShortTrimmed`

If `role` is `BUYER`, the counterparty is the seller; otherwise it is the buyer.

### Open a conversation and read messages

```http
PUT https://gateway.kleinanzeigen.de/messagebox/api/users/{user_id}/conversations/{conversation_id}?contentWarnings=true
```

This unusual read uses `PUT`. The client notes that opening a thread also marks
it as loaded, so classify it as state-touching even though no message is sent.
Messages are found at top-level `messages` or `data.messages`.

Useful message fields:

- `text`, falling back to `textShort` or `title`
- `boundness` or `direction` (`IN` = received, `OUT` = sent)
- `receivedDate`

Preserve the raw message object initially because attachments and unrecognized
message kinds are not normalized by the selected client.

### Reply in an existing conversation

```http
POST https://gateway.kleinanzeigen.de/messagebox/api/users/{user_id}/conversations/{conversation_id}?warnPhoneNumber=false&warnEmail=false&warnBankDetails=false
Content-Type: application/json

{
  "message": "Hallo, ist das noch verfügbar?"
}
```

This sends a real external message. The warning flags are copied from the
reference client; setting them to `false` may suppress useful safety warnings.
Their response and acknowledgement contract is unknown. Capture it with two
dedicated accounts and harmless warning-triggering text before implementing
sends. Until then, kcli must not claim it can acknowledge or preserve platform
warnings and must not expose a flag that blindly disables them.

Only plain text is documented. Attachments, templates, offers, and other message
types are not covered.

### Mark conversations read

```http
POST https://gateway.kleinanzeigen.de/messagebox/api/users/{user_id}/conversations/read?ids=<id1,id2,...>
```

The `ids` value is a comma-separated list. This changes account state but does
not communicate with another user.

### Start a conversation on a listing

```http
POST https://api.kleinanzeigen.de/api/users/{user_id}/create-conversation/{ad_id}?contactName=<name>
```

Access: authenticated main API. The response is the new conversation object.
The selected client treats creation and the first text as two separate actions:
create the conversation, obtain its ID, then use the gateway reply endpoint.

Do not retry this call blindly after a timeout: the first attempt may have
succeeded and an automatic retry could create duplicate state.

## Watchlist

### List saved ads

```http
GET https://api.kleinanzeigen.de/api/users/{user_id}/watchlist.json?_in=<field-selector>&page=0&size=25
```

The response uses the same namespaced ads-list shape as search. The `_in` query
selects returned fields. The reference client requests IDs, title, description,
dates, category, location, price, pictures, attributes, ad status, coordinates,
seller type, ad type, and related metadata.

Adding or removing an item is **not documented** because the selected client
does not implement those endpoints.

## Own-ad endpoints

### List own ads

```http
GET https://api.kleinanzeigen.de/api/users/{user_id}/ads.json?_in=<field-selector>&page=0&size=25
```

Optional query parameters:

| Query | Meaning |
|---|---|
| `sortType` | Server-side ordering value |
| `q` | Keyword filter over the user's ads |

The endpoint returns both active and paused ads in the standard namespaced
ads-list response.

### Fetch one own ad

```http
GET https://api.kleinanzeigen.de/api/users/{user_id}/ads/{ad_id}.json
```

Unlike the public detail endpoint, this can fetch a paused ad owned by the
logged-in user.

### Create an ad

```http
POST https://api.kleinanzeigen.de/api/users/{user_id}/ads.json
Content-Type: application/xml
```

The body is XML using Kleinanzeigen's CAPI namespaces. Minimal conceptual shape:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<ad:ad xmlns:ad="http://www.ebayclassifiedsgroup.com/schema/ad/v1"
       xmlns:cat="http://www.ebayclassifiedsgroup.com/schema/category/v1"
       xmlns:loc="http://www.ebayclassifiedsgroup.com/schema/location/v1"
       xmlns:types="http://www.ebayclassifiedsgroup.com/schema/types/v1"
       xmlns:attr="http://www.ebayclassifiedsgroup.com/schema/attribute/v1"
       xmlns:pic="http://www.ebayclassifiedsgroup.com/schema/picture/v1"
       xmlns:payment="http://www.ebayclassifiedsgroup.com/schema/payment/v1"
       locale="en_US" id="0">
  <ad:title>Example title</ad:title>
  <ad:description>Example description</ad:description>
  <ad:email>account@example.invalid</ad:email>
  <ad:poster-type><ad:value>PRIVATE</ad:value></ad:poster-type>
  <ad:ad-type><ad:value>OFFERED</ad:value></ad:ad-type>
  <cat:category id="278"/>
  <loc:locations><loc:location id="&lt;location-id&gt;"/></loc:locations>
  <ad:ad-address><types:show-full-address>false</types:show-full-address></ad:ad-address>
  <ad:price>
    <types:price-type><types:value>SPECIFIED_AMOUNT</types:value></types:price-type>
    <types:amount>100</types:amount>
  </ad:price>
  <pic:pictures/>
  <attr:attributes/>
  <payment:buy-now selected="false"/>
</ad:ad>
```

Supported mappings observed in the client:

| Friendly value | XML price type | Amount |
|---|---|---|
| `FIXED` | `SPECIFIED_AMOUNT` | Required in practice |
| `NEGOTIABLE` / `VB` | `PLEASE_CONTACT` | Asking price |
| `FREE` | `FREE` | Omitted |

Other supported input fields are contact name, phone, latitude, longitude,
category-specific attributes, and picture URLs. Text and attribute values must
be XML-escaped.

Important limitations:

- Contact email is required in the reference implementation; without one, the
  server reportedly returns `500`.
- The location must be a specific postable city/postcode location, not a broad
  region.
- Pictures must already be uploaded URLs. No image-upload endpoint is known.
- A successful new ID may be in the `Location` response header or response ad.
- This creates a real public listing and must never run without a final preview
  and explicit user confirmation.

### Pause an ad

```http
PUT https://api.kleinanzeigen.de/api/users/{user_id}/ads/paused/{ad_id}.json
```

Reversible with the activate endpoint. Require confirmation because the public
listing becomes unavailable.

### Activate an ad

```http
PUT https://api.kleinanzeigen.de/api/users/{user_id}/ads/active/{ad_id}.json
```

Publishes a paused listing again. Require confirmation because it changes public
state.

### Check extension eligibility

```http
GET https://api.kleinanzeigen.de/api/users/{user_id}/ads/extend/status?adids=<id1,id2,...>
```

The `adids` query is comma-separated. The selected client returns the response
JSON without normalization, so initially preserve and log its shape with tokens
and personal data redacted.

### Extend an ad

```http
POST https://api.kleinanzeigen.de/api/users/{user_id}/ads/extend/{ad_id}
```

Renews or extends the listing. Check eligibility first and require confirmation.
Do not automatically retry after an ambiguous timeout.

### Delete an ad

```http
DELETE https://api.kleinanzeigen.de/api/users/{user_id}/ads/{ad_id}
```

This is permanently destructive. An agent should show the exact listing ID,
title, status, and owner, require explicit confirmation, and then make one
attempt. The upstream CLI does not add a confirmation prompt, so do not expose
it directly as an autonomous tool.

## Public website fallback

This endpoint is not part of the mobile API but is operationally useful when
mobile location lookup fails:

```http
GET https://www.kleinanzeigen.de/s-ort-empfehlungen.json?query=Berlin
X-Requested-With: XMLHttpRequest
Accept-Language: de-DE
```

It returns an object mapping keys such as `_<location-id>` to labels. Strip the
leading underscore and ignore ID `0`.

## Not currently covered

The selected implementation provides no known useful endpoint for:

- remotely revoking an OAuth refresh-token family on logout (`/oauth/revoke`
  has not been verified); v0.1 logout therefore clears the local session only;
- adding or removing watchlist items;
- creating, changing, or deleting saved searches and notifications;
- editing the content of an existing ad;
- uploading local images for a new ad;
- sending message attachments;
- following or unfollowing users;
- blocking or reporting users/messages;
- offers, direct purchase, payment, shipping, refunds, or disputes;
- profile and notification settings.

Do not infer those URLs from naming patterns. Discovering them would require a
separate, explicitly scoped investigation, and payment or safety operations
should remain in the official interface.

## Integration rules for this project

1. Default to a minimum 2.5 seconds between requests, add jitter, and use bounded
   retries only for `429`, `500`, and `503`.
2. Never attempt to circumvent `429`, bot controls, or access restrictions.
3. Cache categories and resolved locations. Do not poll metadata unnecessarily.
4. Treat `404` listing detail as expected deletion/expiry, not a retry target.
5. Redact Basic credentials, bearer tokens, refresh tokens, email path segments,
   message bodies, and precise account data from logs.
6. Keep read-only anonymous operations separate from authenticated operations.
7. Require a human preview and explicit confirmation for every message or
   public-state mutation. Require stronger confirmation for deletion.
8. Assign idempotency locally. Never blind-retry conversation creation, replies,
   ad creation, extension, or deletion after an ambiguous timeout.
9. Pin and record the app version and endpoint-contract version used by each
   run. A coordinated set of `401`/`403` responses can indicate credential or
   header rotation; parsing failures can indicate a response-shape change.
10. Maintain a low-volume live contract test for location, search, detail, and
    one image URL only when written permission covers it. Run authenticated
    checks manually with two separately authorized dedicated accounts,
    read-only first, so sends and warning behavior can be verified end to end.

## Known reliability boundaries

- These endpoints are private implementation details and may change without
  versioning or notice.
- The app-distribution Basic credential and Auth0 client configuration can
  rotate independently of this repository.
- Response objects use inconsistent wrappers and sometimes switch between an
  object and an array for a single item.
- The mobile API can expose more structured fields than the public website; do
  not assume that technical availability grants permission to collect or retain
  them.
- Only the anonymous discovery path was live-proven in this project snapshot.
  All authenticated request and response contracts remain provisional until
  checked with two separately authorized dedicated accounts, beginning with
  non-destructive tests and limiting writes to the documented acceptance cases.
