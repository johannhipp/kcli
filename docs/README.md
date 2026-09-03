# Project documentation

- [Product and technical scope](kcli-scope.md) — the full kcli boundary,
  release layering, architecture, compatibility rules, safety model, and
  agent-DX target.
- [v0.1 scope](v0.1-scope.md) — the authoritative release boundary, narrowed
  user stories, explicit exclusions, and acceptance criteria for local discovery
  and messaging.
- [Implementation plan](implementation-plan.md) — the chosen Go stack,
  feasibility gates, architecture, phased work, test strategy, estimates, and
  complete v0.1 story traceability.
- [Proposed command syntax](command-syntax.md) — exact command families,
  arguments, output contract, confirmation flow, and exit behavior.
- [Endpoint coverage map](endpoint-coverage.md) — scoped user needs mapped to
  mobile endpoints, evidence levels, local implementations, and gaps.
- [DM synchronization design](dm-sync.md) — one-shot polling, foreground NDJSON
  watching, cursor semantics, optional daemon, and MCP/responder integration.
- [Undocumented mobile API reference](mobile-api.md) — every endpoint currently
  useful to this project, including authentication, request headers, payloads,
  response fields, risk levels, and known gaps.
- [Kleinanzeigen web user stories](user-stories.md) — the complete marketplace
  journey reference in one consistent user-story format, including browser
  capabilities deliberately outside kcli, with API coverage summarized by area.
- [Research and candidate comparison](../README.md) — the investigation that led
  to choosing the mobile API approach.
- [Changelog](../CHANGELOG.md) — SemVer release history and pending changes.
- [Agent maintenance instructions](../AGENTS.md) — scope, documentation,
  changelog, Conventional Commits, safety, and verification rules.
- [Contributing guide](../CONTRIBUTING.md) — local workflow, required checks,
  commit format, pull-request expectations, and versioning.
- [Security policy](../SECURITY.md) — private reporting and the project's
  credential, automation, and message-safety boundaries.
- [kcli agent skill](../.agents/skills/kcli/SKILL.md) — actionable guardrails
  (dry runs, field masks, confirmation bindings, cursors, fail-closed exits)
  for driving kcli from an agent.

The endpoint reference is based on
[`monkrel/kleinanzeigen-api` v0.4.0](https://github.com/monkrel/kleinanzeigen-api/tree/efb2d82bf449c38a49558b8c71df8d888effbfd9),
plus the anonymous live smoke test performed on 2 September 2026. Kleinanzeigen
does not publish or support these interfaces.
