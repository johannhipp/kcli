# Endpoint coverage

The release uses the public website without cookies, login or app credentials.
See [web API](web-api.md) for exact observed paths and [test results](test-results.md)
for current verification. Unknown contracts are unsupported, never invented.

| Story | Implementation / evidence |
|---|---|
| V01-SEARCH-01 | Public search form and category filter links; metadata/search tests |
| V01-SEARCH-02 | Public search form and category filter links; metadata/search tests |
| V01-SEARCH-03 | Public search form and category filter links; metadata/search tests |
| V01-SEARCH-04 | Public search form and category filter links; metadata/search tests |
| V01-SEARCH-05 | Public search form and category filter links; metadata/search tests |
| V01-SEARCH-06 | Public search form and category filter links; metadata/search tests |
| V01-SEARCH-07 | Public search form and category filter links; metadata/search tests |
| V01-SEARCH-08 | Public search form and category filter links; metadata/search tests |
| V01-SEARCH-09 | Public search form and category filter links; metadata/search tests |
| V01-SEARCH-10 | Public search form and category filter links; metadata/search tests |
| V01-LISTING-01 | Returned listing URL and exact image URLs; listing/media tests |
| V01-LISTING-02 | Returned listing URL and exact image URLs; listing/media tests |
| V01-LISTING-03 | Returned listing URL and exact image URLs; listing/media tests |
| V01-LISTING-04 | Returned listing URL and exact image URLs; listing/media tests |
| V01-LISTING-05 | Returned listing URL and exact image URLs; listing/media tests |
| V01-LISTING-06 | Returned listing URL and exact image URLs; listing/media tests |
| V01-LISTING-07 | Returned listing URL and exact image URLs; listing/media tests |
| V01-LISTING-08 | Returned listing URL and exact image URLs; listing/media tests |
| V01-USER-01 | Listing seller data and bounded local index; seller tests |
| V01-USER-02 | Listing seller data and bounded local index; seller tests |
| V01-USER-03 | Listing seller data and bounded local index; seller tests |
| V01-USER-04 | Listing seller data and bounded local index; seller tests |
| V01-USER-05 | Listing seller data and bounded local index; seller tests |

Authentication and messaging are excluded. The website does not expose a global
username directory. Picture-required filtering is unsupported; category-filter
coverage is limited to observed and recognized encodings. Website markup can
change independently of kcli; contract failures must be surfaced clearly.
