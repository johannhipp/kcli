# Anonymous web interface evidence

Observed 6 September 2026, after the operator confirmed written Kleinanzeigen
permission and requested the unofficial web API for current testing. No user
cookies, login, account access, or mutations were used. Requests used ordinary
HTTP clients from the development network; no fingerprint switching or bypass.

The anonymous release uses these website contracts through the Go web adapter.
Search is HTML with structured Astro hydration data, not a standalone JSON search
API. Internal compatibility request identifiers do not make mobile API calls.

## Verified reads

| Request | Response and useful contract | Evidence |
|---|---|---|
| `GET https://www.kleinanzeigen.de/` | Search form and nested category tree in `astro-island` props | 160 category entries, including all-categories ID `0` |
| `GET /s-ort-empfehlungen.json?query=TEXT` | JSON object: `_<location-id>` → label; `0` means Germany | Berlin: 16 entries; postcode 10115: 3 |
| `GET /s-suchanfrage.html?...` | Redirect to a canonical search URL, then HTML | Form action and input names taken from returned HTML |
| `GET` canonical search URL | `SearchForm` props: selected filters/category/location; `ImpressionTracker` props: `resultAds[].organicAdPreview` | Combined search returned 26 records |
| `GET` returned pagination link | Next search page, one-based `seite:2` in URL | 25 records on page 2; no duplicate IDs in this sample |
| `GET` returned `/s-anzeige/<slug>/<id>-<category>-<location>` | Full detail HTML; currently legacy `viewad-*` elements | Title, price, location, description, attributes, seller, and gallery sections present |
| `GET` returned `/s-bestandsliste.html?userId=<id>` | Public seller inventory HTML | Listing links present anonymously |
| `GET` exact returned `https://img.kleinanzeigen.de/...` URL | Image bytes, no credentials | Valid 213,554-byte JPEG; two other returned variants gave 404 without retry |

Observed search fields: `keywords`, `categoryId`, `locationId`, `locationStr`,
`radius`, `minPrice`, `maxPrice`, `adType`, `posterType`, `sortingField`,
`buyNowEnabled`, `shipping`, and `shippingCarrier`. The last three are discovery
signals only; no purchase or shipping action is authorized by this inventory.

Verified combinations used category 217, Berlin location 3331, 10 km radius,
EUR 50–200, and `adType=OFFER`. Wanted listings use `WANTED`. Sort values returned
by the website and accepted in separate requests were `SORTING_DATE`,
`PRICE_AMOUNT`, `PRICE_AMOUNT_DESC`, and `DISTANCE`; distance was advertised when
a location was selected. Tests assert selected state, not full result-order
correctness in the presence of promotions and volatile listings.

Category filters are exposed as links, for example a returned URL suffix
`+fahrraeder.type_s:<value>`. Following that exact link populated the
`nonRangeAttributeMap`/`attributeMap` state. Condition links used `global.zustand`.
The bicycle filter was applied, followed by a car range and two boolean options.
The car form and its `RenderBrowseRange` script use two ordered inputs named
`attributeMap[autos.km_i]` (minimum then maximum). Submitting `10000` and `50000`
echoed a `10000,50000` range with both bounds intact. Repeated `clickableOptions`
values retained `autos.air_conditioning_b` and `autos.navi_b` together. These are
observed web encodings, not evidence for mobile query serialization. Multi-value
selection within one enum is still unverified. A picture-required web filter
was not found in the inspected form.

Keyword-free category browsing returned 34 tracking records; a deliberately
unmatched keyword returned zero records and an explicit empty-result message.
Counts include records used for impression tracking and should not be described
as a count of unique organic cards without normalization.

Structured search data includes identity, title, description, price, public
`seoLink`, `userId`, poster type, sorting date, location, distance, attributes,
`imageList`, shipping signals, and promotion flags. Preserve unknown keys and
distinguish organic results from promotion/tracking records. Detail HTML and
search HTML currently use different rendering architectures.

Public seller inventory does not establish a global username directory.
`seller listings` fetches one bounded public inventory page; seller-name lookup
remains local and completeness-labeled.

## Reproduce

```sh
python3 scripts/test_web_anonymous.py --live
python3 scripts/probe_web.py search.html --fetch 'https://www.kleinanzeigen.de/s-fahrrad/k0'
```

The small probe enforces at least 2.5 seconds plus jitter between website
requests, exact-host redirects, response limits, and no retries after an access
or rate failure. Run one probe process at a time. Raw pages and result files
stay in ignored `.tmp/web-acceptance/`; do not commit them because they include
live listing data and page tokens. Stdout contains safe structure/counts only.
The image-only check accepts an existing capture basename and fetches one exact
returned image; it can finish image verification after a volatile 404 without
rerunning the successful search checks.

See [test-results.md](test-results.md) for the current Go CLI acceptance evidence.

## Release adapter contract

The client has no cookie jar and sends no authorization headers. Every outgoing
read, including redirects and image fetches, uses shared SQLite request pacing.
Responses are bounded; operation deadlines include waiting. There are no retries
on challenges or rate limits. Persisted cooldowns are checked before reserving
and immediately before dispatch, including requests already waiting in another
process. Redirects remain on the exact public website host;
media starts from exact returned URLs on `img.kleinanzeigen.de`; redirects are
bounded and must remain on that exact image host.

Categories and locations normalize public identity/labels. Filter metadata comes
from returned links and form inputs: enum values use observed suffixes, booleans
use `clickableOptions`, and ranges use ordered `attributeMap[KEY]` bounds entered
as `MIN,MAX`. Unsupported shapes must not be advertised as accepted encodings.
Picture-required and non-default page-size requests return explicit input errors.

Listing URLs are accepted directly. ID-only lookup requires a public URL already
encountered by the profile; the client never fabricates a listing URL. Public
listing fields and unknown captured attribute values are normalized into JSON;
raw output is a redacted extracted record, not full HTML containing page tokens.
Absent metadata remains unknown. Search pagination follows returned links.

The advertised `global.zustand` enum is echoed by the website under
`globalFilters["global.condition"]`. Filter verification recognizes that exact
observed alias; mismatched values or unknown aliases fail. Numeric CLI location
references are IDs; postcode suggestions are resolved explicitly first.
