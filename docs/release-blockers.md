# Release status and blockers

> **One-line status:** the full **offline** implementation of kcli (phases 1–8 +
> hardening) is **built, tested, and merged to `main`**. It is **not a release**:
> six external items still gate live evidence and the `0.1.0` tag. None of the
> six can be resolved from inside this repository.

## ✅ Done — merged to `main`

All offline work is complete, green, and committed (merged via PR #1 as
`11507f1`, plus test hardening `ec9f4ac`).

- [x] **Foundation** — single Go binary, Kong CLI with all 33 v0.1 leaf
  commands, operation catalog + JSON Schema, JSON/NDJSON/table/raw encoders
  with `--fields`, per-profile SQLite (13 tables, goose migrations),
  split-entry OS keyring with >2560-byte chunking + build-tagged test fake,
  cross-process rate reservations, `schema`/`config`/`version`/`doctor`/
  `completion`.
- [x] **API boundary** — host-allowlisted `net/http` mobile transport
  (main/gateway/login/media), mobile headers + redaction, namespace/value/
  singleton decoding, request classes + bounded retry, category/location/
  filter discovery with caches.
- [x] **Search** — validated `SearchInputV1`, category/location resolution,
  metadata-driven filter serialization, bounded pagination/dedup/exclusions,
  canonical spec, seller indexing.
- [x] **Listing / media / sellers** — complete normalized listing + redacted
  raw, sandboxed image download, official-URL open, source/completeness-labelled
  local seller lookup.
- [x] **Auth** — PKCE login (TTY + non-echo stdin), single JSON token exchange,
  ID-token claim checks (iss/aud/exp/nonce, TLS back-channel per OIDC Core
  §3.1.3.7), account-ID resolution, split-keyring persistence, leased refresh,
  local logout.
- [x] **DM — reads** — inbox list/get/mark-read with body-free fingerprints,
  listing/counterparty context snapshots, account isolation.
- [x] **DM — messaging** — preview-bound single-confirm reply/start
  (digest-only plans, atomic single-use claims, ambiguous-outcome
  reconciliation, exit 8); external create/send are exactly-one-request with no
  auto-401-refresh-retry.
- [x] **DM — sync** — resumable list-only `dm poll` + foreground NDJSON
  `dm watch` (DB-validated cursors, at-least-once events, sync lease,
  SIGINT/SIGTERM handling).
- [x] **Hardening** — staticcheck/govulncheck/cross-compile CI lanes, `go`
  toolchain pin (no stdlib vulns), installable kcli agent skill, fuzz targets
  + a CLI testscript scenario, README/CONTRIBUTING/docs/CHANGELOG consistency.

Verified: `gofmt -l` empty · `go build`/`go vet` · `go test -tags testing -race ./...`
· `staticcheck` · `govulncheck` (no vulns) · six-target cross-compile ·
`scripts/check_docs.py`. CLI smoke confirms every remote command **fails closed**
with the correct exit code and makes **no live network call**.

> ⚠️ "Done" here means **code is implemented and offline-tested**, not that the
> release acceptance is met. Every story/acceptance criterion that needs live
> evidence remains **open** until the blocked items below are satisfied.

## ⬜ Blocked — the only things standing between this and `0.1.0`

These are **not scope gaps**; they are the plan's own Phase 0 gates plus the
release tag. They are external (permission, network, accounts) and can only be
unblocked by the operator. In-repo, the evidence labels stay `source` /
`provisional` / `designed` until these pass.

- [ ] **1. Written Kleinanzeigen permission** — explicit permission (ToS §5)
  for automated access, collection, authenticated testing, and distribution.
  *Needed before: any live test or release. Not substitutable by a local risk
  acknowledgement.*
- [ ] **2. Development network IP block clearing** — the 2 Sep 2026 temporary
  IP-range block must expire; the first transport comparison runs from the same
  network only after it clears.
  *Needed before: live read/transport testing. Forbidden: change networks,
  rotate fingerprints, proxy, or solve challenges.*
- [ ] **3. Two authorized dedicated test accounts** — one owning a disposable
  listing, for non-destructive authenticated testing and end-to-end
  send/receipt.
  *Needed before: any authenticated/DM evidence.*
- [ ] **4. Phase 0 live transport matrix** — prove the fixed transport (standard
  `net/http` vs one documented profile), login host + image CDN accept standard
  Go TLS, capture a fixture per filter kind, record credential/header stability.
  *Depends on 1–3. Decides whether `0.1.0` can be called such at all.*
- [ ] **5. Phase 0 authenticated contract capture** — prove Auth0
  issuer/audience/nonce, token rotation/reuse, account-ID/profile lookup,
  conversation-read side effects, warning-flag semantics, contact-name default,
  conversation-creation visibility, `/oauth/revoke` support, and the two-account
  reply/first-contact receipt.
  *Depends on 1–4.*
- [ ] **6. `0.1.0` release tag** — signed tag + artifacts, checksums, SBOM,
  install docs.
  *Depends on 1–5 and a green release-candidate matrix.*

## How to unblock

```text
  ① written permission  ──▶  ② IP block clears  ──▶  ③ two test accounts
                                                        │
                      ④ live transport matrix  ◀────────┘
                                        │
                      ⑤ authenticated contract capture
                                        │
                    ⑥ release `v0.1.0`
```

Only ① and ② ③ are truly external (permission, time, account provisioning). Once
those exist, ④–⑥ are executable in the plan's documented sequence.

## Evidence labels in-repo (unchanged until live)

| Surface | Current label | Becomes |
|---|---|---|
| anonymous discovery (category/location/search/listing/filter) | `Live`-intended, **not yet live-proven** | `Live` after ① ② ④ |
| authenticated auth/DM | `source` / `designed` / `provisional` | `source`→verified after ① ③ ⑤ |
| local seller index, search exclusions, cursor/event behaviour | `Local` | stays `Local` |

## 🔭 Still to test / decide (consolidated)

The offline codebase is complete and green. Everything below still needs a
**decision from you**, **live/external verification**, or is a **deliberate
by-design** trade-off. Nothing here can be finished purely in-repo today.

### Needs a decision

- **OIDC ID-token signature (audit #9)** — keep the plan's deliberate
  TLS-back-channel validation (OIDC Core §3.1.3.7), or switch to JWKS signature
  verification (adds a fixture + fake JWKS server). Not done unilaterally
  because it reverses the documented plan decision.
- **Conversation preview retention (audit #16)** — pick a retention period so
  the code, `docs/dm-sync.md`, `docs/kcli-scope.md`, and the kcli skill agree.
  Currently the code keeps previews for the account's lifetime while the docs
  state a 7-day preview retention.

### Needs live evidence (the six gates above)

- Anonymous discovery and the authenticated/DM contracts cannot be proven by an
  offline test; the six blocked items above are the prerequisites.

### Coverage gaps that need a code change

- **Keyring backend coverage (#20)** — the shipping `osBackend` and
  `CheckAvailability` are untested; the test-only fake is `//go:build testing`,
  so an untagged `go test ./...` skips the secret Store tests (by design). Full
  coverage needs a `zalando/go-keyring` mock or a non-tagged fake that is
  excluded from release binaries.
- **testscript exit-code scenarios (#19)** — full send/pagination scenarios are
  not offline-reachable because the CLI transport cannot be injected; the
  local/destructive exit-code cases are already covered.

### By design (deliberate, not defects)

- ecode machine-readable output is 2/3: NDJSON is opt-in and finite pagination
  emits one envelope so stop/count metadata is not lost.
- Schema introspection is 2/3: the category-filter overlay stays static until
  live metadata is cached.
- Knowledge packaging is 2/3: operation examples are surfaced via
  `kcli schema show`, not inline `--help`, because Kong has no native
  per-command examples.
