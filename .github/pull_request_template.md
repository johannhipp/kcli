## Summary

<!-- What user-visible or repository outcome does this change produce? -->

## Why

<!-- Why is this change needed, and how does it fit the documented scope? -->

## Verification

<!-- List the exact checks run and any intentionally untested behavior. -->

## Contract and safety checklist

- [ ] The change stays within `docs/v0.1-scope.md`, or updates that scope
      explicitly.
- [ ] Relevant API, command, synchronization, and implementation docs are
      updated with the behavior.
- [ ] `CHANGELOG.md` is updated for user-visible or meaningful contract changes.
- [ ] `python3 scripts/check_docs.py` passes.
- [ ] No credentials, tokens, account identifiers, personal data, or live
      message content are committed.
- [ ] Live API evidence, if any, was gathered under written permission and
      without bypassing access controls.
- [ ] Message or account-state mutations, if any, retain documented preview,
      confirmation, and no-blind-retry behavior.
