# Changelog

Notable changes use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Release automation moves pending entries into a dated section when publishing.

## [Unreleased]

## [0.1.11] - 2026-09-18

### Changed

- Expand the concise agent skill with launch methods, command discovery and bounded usage guidance.

## [0.1.10] - 2026-09-18

### Changed

- fix(media): anchor downloads to directory handles (#22) (7800664)

## [0.1.9] - 2026-09-18

### Changed

- fix(cli): include state initialization in command deadlines (#21) (74c98fa)

## [0.1.8] - 2026-09-18

### Changed

- fix(output): propagate table flush failures (#20) (8f9be59)

## [0.1.7] - 2026-09-18

### Changed

- fix(output): preserve NDJSON metadata when projecting fields (#19) (47c4ba0)

## [0.1.6] - 2026-09-18

### Changed

- fix(cli): keep recovery commands usable with broken local files (#18) (a42ee00)
- fix(state): enforce seller retention for search indexing (#17) (84def96)

## [0.1.5] - 2026-09-18

### Changed

- fix(transport): classify website hydration failures (#16) (851516a)

## [0.1.4] - 2026-09-18

### Changed

- fix(search): preserve website pagination and report truncation (#15) (2366a87)

## [0.1.3] - 2026-09-18

### Changed

- fix(media): follow validated public image redirects (#14) (1539c2b)

## [0.1.2] - 2026-09-18

### Fixed

- Anchor image writes to an opened directory and reject directory replacement during downloads.

- Include state initialization and SQLite lock waiting in the command timeout.

- Report buffered table-output write failures instead of returning success.

- Preserve NDJSON source, completeness, pagination, and warnings independently of field projection.

- Keep doctor, version, and config path available with malformed local configuration or state.

- Apply seller age and count retention limits to search and inventory indexing.

- Classify malformed or unsupported website hydration as a safe upstream-contract error.

- Preserve website search continuation and label within-page result truncation.

- Follow bounded same-host image redirects through the production web transport.

- Stop queued public requests when another process records a rate-limit cooldown.

## [0.1.1] - 2026-09-18

### Changed

- Require draft PRs for repository changes, focused checkpoint commits, concise
  maintained PR descriptions, and PR/diff links after substantial milestones.

## [0.1.0] - 2026-09-18

### Added

- Account-free public website browsing: categories, location resolution, search,
  supported category filters, listing details, images and seller discovery.
- JSON/NDJSON output, field projection, structured search input, runtime schemas,
  shell completions, local configuration and optional network diagnostics.
- Conservative shared request pacing, deadlines, response limits and explicit
  errors for unsupported website behavior, rate limits and access challenges.
- macOS and Linux binaries for amd64 and arm64, with SHA-256 checksums.
- Automatic SemVer tags, dated changelog entries and GitHub Releases after
  successful main-branch validation and builds.

### Changed

- Narrow the first release to unauthenticated, read-only website capabilities;
  remove auth and DM commands from the public CLI and schema inventory.
- Replace the mobile-credential-dependent release backend with public web reads.
- Consolidate CI and replace superseded planning documents with maintained scope,
  architecture, command and endpoint references; link historical research in Git.

### Fixed

- Initialize search state, validate documented seller URLs consistently, expose
  cached category-filter schemas, and describe raw JSON values correctly.
- Keep unsupported filters explicit and seller-name discovery locally scoped.
- Preserve commas in range-filter arguments and treat numeric location values
  as IDs rather than text suggestions; resolve postcodes explicitly first.
- Retain public seller profiles and richer encountered names across inventory
  reads, recognize commercial-listing seller IDs, and preserve relative image
  output paths through CLI parsing.

[Unreleased]: https://github.com/johannhipp/kcli/compare/v0.1.11...HEAD
[0.1.11]: https://github.com/johannhipp/kcli/compare/v0.1.10...v0.1.11
[0.1.10]: https://github.com/johannhipp/kcli/compare/v0.1.9...v0.1.10
[0.1.9]: https://github.com/johannhipp/kcli/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/johannhipp/kcli/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/johannhipp/kcli/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/johannhipp/kcli/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/johannhipp/kcli/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/johannhipp/kcli/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/johannhipp/kcli/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/johannhipp/kcli/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/johannhipp/kcli/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/johannhipp/kcli/releases/tag/v0.1.0
