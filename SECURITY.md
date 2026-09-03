# Security policy

## Supported versions

kcli has no released version yet. Security work currently targets the default
branch and the planned `0.1.0` release.

## Reporting a vulnerability

Do not open a public issue containing credentials, tokens, private endpoint
details that create immediate account risk, message contents, or personal data.
Use a [private GitHub security advisory](https://github.com/johannhipp/kcli/security/advisories/new)
for this repository instead.

Include the affected command or document, impact, a minimal reproduction, and
any suggested mitigation. Remove secrets and personal data from screenshots,
logs, and fixtures. If a live credential was exposed, rotate or revoke it at its
source; deleting it from Git history is not sufficient.

## Project security boundaries

- The undocumented mobile API is unsupported and may change without notice.
- Automated live access requires written Kleinanzeigen permission.
- Access controls, rate limits, warnings, and anti-automation measures must not
  be bypassed.
- Outbound messages require an exact preview and explicit confirmation and are
  never blindly retried.
- Authentication secrets belong in the operating-system secret store, not the
  repository, config files, command arguments, logs, or test fixtures.
