# Changelog

Notable changes use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Release automation moves pending entries into a dated section when publishing.

## [Unreleased]

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

[Unreleased]: https://github.com/johannhipp/kcli/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/johannhipp/kcli/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/johannhipp/kcli/releases/tag/v0.1.0
