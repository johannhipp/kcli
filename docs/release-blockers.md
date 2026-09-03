# Release blockers

Status: active — these items are the only things standing between the merged
offline implementation and an `0.1.0` release. None of them can be resolved from
inside the repository. Everything recorded here is also tracked in the
[implementation plan](implementation-plan.md) (Phase 0) and the
[v0.1 scope](v0.1-scope.md) acceptance criteria.

> These are **not** scope gaps. Phases 1–8 are implemented, build, and test
> green against redacted fixtures and fake servers. The items below gate
> **live evidence and the release tag**, which the plan deliberately keeps
> external and explicit.

## Blocked items

### 1. Written Kleinanzeigen permission

- **What:** explicit written permission (Kleinanzeigen `Nutzungsbedingungen`
  §5) covering automated access, collection, authenticated testing, and
  distribution of the intended use.
- **Why:** the plan requires this before any automated live test or release.
  A local risk acknowledgement is not a substitute.
- **Unblock:** obtain and record the permission; then run the Phase 0 gates.

### 2. Development network IP block clearing

- **What:** the temporary IP-range block received on 2 September 2026 must
  naturally expire, and the first transport comparison must run from the same
  network and only after it clears.
- **Why:** a block or challenge is a stop condition. The plan forbids changing
  networks, rotating fingerprints, using proxies, or solving challenges.
- **Unblock:** wait/persist on the allowed network; do not attempt to evade.

### 3. Two authorized dedicated test accounts

- **What:** two separately authorized Kleinanzeigen accounts (one owning a
  disposable test listing) for non-destructive authenticated testing and the
  end-to-end send/receipt checks.
- **Why:** the authenticated inbox/messaging contracts cannot be proven or
  marked complete without them.
- **Unblock:** provision the two accounts under the written permission.

### 4. Phase 0 live transport matrix

- **What:** prove the fixed transport (standard `net/http` vs one documented
  profile) succeeds on the allowed network after the block clears; verify the
  login host and image CDN accept standard Go TLS; capture redacted fixtures
  for every filter kind; record app credential/header stability.
- **Why:** decides whether `0.1.0` can be called such at all, and fixes the
  filter serialization rules.
- **Unblock:** run the Phase 0 contract capture once 1 + 2 + 3 are satisfied.

### 5. Phase 0 authenticated contract capture

- **What:** prove Auth0 issuer/audience/nonce, token rotation/reuse behavior,
  account-ID/profile lookup, conversation read side effects, warning-flag
  semantics, contact-name default, conversation-creation visibility, and
  `/oauth/revoke` support — plus the two-account reply/first-contact receipt
  and read-state handling.
- **Why:** the authenticated transport/auth/DM contract evidence labels are
  `source`/`designed`/`provisional` until this passes; stories cannot be marked
  complete without it.
- **Unblock:** run with the two authorized accounts under written permission,
  read-only first, then the documented acceptance sends.

### 6. `0.1.0` release tag

- **What:** a signed `v0.1.0` tag plus release artifacts, checksums, SBOM, and
  install docs.
- **Why:** the plan's definition of done requires Phase 0 to have passed
  without bypassing an access control before a release is tagged.
- **Unblock:** after 1–5 pass and all release-candidate checks are green.

## Status summary

| Item | State | Can resolve in-repo |
|---|---:|---|
| Written permission | blocked | no — external |
| IP block clearing | blocked | no — external / time |
| Two test accounts | blocked | no — external |
| Live transport matrix | blocked | no — after 1–3 |
| Authenticated contract capture | blocked | no — after 1–4 |
| `0.1.0` release tag | blocked | no — after 1–5 |

Everything else in the implementation plan is complete: the offline code
(phases 1–8) builds, cross-compiles for the six targets, is tested against
redacted fixtures and fake servers, and holds no standard-library
vulnerabilities. The commit history and the open installable kcli agent skill
are in this repository.
