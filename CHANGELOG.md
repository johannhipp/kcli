# Changelog

All notable changes to this project are documented here. Released versions use
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) in `MAJOR.MINOR.PATCH`
form, and entries follow the structure of
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

The first planned release is `0.1.0`. There are no tagged releases yet.

## [Unreleased]

### Added

- Planned `0.1.0` scope for anonymous discovery, complete listing inspection,
  best-effort seller discovery, and authenticated DMs, while explicitly leaving
  in-person coordination as ordinary message text.
- Scoped v0.1 user stories and release acceptance criteria.
- Maintenance rules for SemVer, Conventional Commits, documentation, and this
  changelog.
- Initial Kleinanzeigen automation landscape research and feature matrices.
- Anonymous and authenticated-read-only smoke-test scripts.
- Undocumented mobile API endpoint reference.
- Comprehensive Kleinanzeigen web user-story inventory.
- Full kcli product and technical scope with an agent-first design target.
- Command syntax proposal covering discovery, listings, sellers, authentication,
  DMs, schemas, diagnostics, and later daemon/MCP surfaces.
- User-story-to-mobile-endpoint coverage map with explicit evidence and gaps.
- DM synchronization design for incremental polling, foreground NDJSON watching,
  resumable cursors, an optional per-user daemon, and responder integration.
- Repository-local installation of the `agent-dx-cli-scale` skill used to assess
  the proposed interface.
- Documentation validation that requires every scoped v0.1 story to appear in
  the endpoint-coverage map.
- Detailed Go implementation plan covering dependency choices, architecture,
  release gates, delivery phases, test strategy, estimates, and every scoped
  story; reviewed with OMP's `fbl` model preset and revised from its findings.
- Initial repository hygiene: ignore and line-ending rules, shared editor
  settings, a Conventional Commit template, contribution and security policies,
  a pull-request checklist, and documentation-contract CI.
- Functional `category`, `location`, and `filter` discovery commands backed by the bounded mobile transport, with raw evidence fields and fail-closed behavior when distribution credentials are absent.
- Added search, listing, seller, and authentication command families over the bounded mobile transport and the profile-local state, with strictly validated identifiers, fail-closed access when credentials are absent, and no live-network experiments.
- Added the authenticated DM surface: inbox reads with body-free fingerprints and account isolation, preview-bound single-confirm reply/start messaging (digest-only plans, atomic single-use claims, ambiguous-outcome reconciliation), and resumable list-only `dm poll` plus foreground NDJSON `dm watch`.
- Hardening: staticcheck and govulncheck lanes, a six-target cross-compile lane, an installable kcli agent skill, and a `go` toolchain pin with no standard-library vulnerabilities.

### Changed

- Classified the comprehensive web user-story inventory as the long-term backlog
  rather than the v0.1 implementation boundary.
- Clarified that kcli exposes browser-equivalent CLI primitives. Pickup,
  inspection, negotiation, meeting-place, and payment-on-pickup details remain
  opaque DM text rather than separate commands or workflows.
- Added `dm poll` and `dm watch` to the planned `0.1.0` boundary while keeping the
  background daemon and MCP adapter as later, optional layers.
- Made DM polling and watching conversation-list-only by default; opening
  changed conversations is explicit because it may alter account read/load
  state.
- Tightened message confirmation, ambiguous-outcome recovery, OAuth/OIDC token
  handling, local-secret retention, filter metadata semantics, and transport
  feasibility gates.
- Reduced the v0.1 output/dependency surface to tables, JSON, NDJSON, redacted
  raw JSON, and field selection; YAML, embedded jq, PowerShell completion, and
  refresh-token environment injection are deferred.
- Second plan review pass: replaced the OAuth/OIDC libraries with
  standard-library PKCE, JSON token grants, and ID-token claim checks (TLS
  back-channel validation per OIDC Core §3.1.3.7); replaced the keyring
  fail-closed size ceiling with chunking at the real Windows limit; removed
  `dm get --mark-read` and the public-website location fallback from v0.1;
  specified exit codes for `resync_required`, `rate_limited_local`, and
  `SIGTERM`; fixed the watch interval floor and monotonic cursor head; defined
  the release-snapshot filter audit; and allowed offline phases to proceed
  while phase-0 permission gates are pending.

### Fixed

- Made smoke-script help available on a fresh checkout before the optional
  `kleinanzeigen-api` research dependency is installed.
