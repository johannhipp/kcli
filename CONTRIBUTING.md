# Contributing

Read [AGENTS.md](AGENTS.md), the [scope](docs/v0.1-scope.md), and
[command contract](docs/command-syntax.md). Keep changes focused; update affected
docs and `CHANGELOG.md` under `Unreleased` in the same change.

## Checks

```sh
make check
make lint  # additional staticcheck and vulnerability checks
make cross
```

`make check` covers formatting, module integrity, build, vet, offline race tests,
script tests and documentation in one CI job. `make lint` adds staticcheck and
vulnerability analysis for local/release checks. Four cross-builds cover
macOS/Linux amd64/arm64.
Tests must not contact the live website automatically. Explicit live acceptance
requires written permission, conservative pacing and no access-control bypass.
Do not commit captures, credentials, tokens or personal data.

## Commits and releases

Use Conventional Commits: `type(scope): imperative summary`, such as
`fix(search): retain pagination metadata` or `feat(filters): support ranges`.
Use `!` and a `BREAKING CHANGE:` footer for breaking contracts. The optional
commit template is enabled with `git config commit.template .gitmessage`.

Pull requests run the validation job. Every validated push to `main` runs the
same four-target release build. The initial release is `0.1.0`; later releases
use the commits since the latest tag: breaking changes bump major, `feat` bumps
minor, and other changes bump patch. Tags use `vMAJOR.MINOR.PATCH`.

The release job moves `Unreleased` notes into a dated version section, records a
bot commit with `[skip ci]`, and atomically pushes that commit and its annotated
tag. It then uploads the four archives and SHA-256 checksums to GitHub Releases.
A newer main commit prevents the older run from changing main; reruns recover
an already prepared tag. The workflow needs repository contents-write access.
Never move a published tag or claim a release exists before GitHub publishes it.

History is the authoritative commit list. Long-lived plans and agent transcripts
do not belong in the repository; preserve useful API findings in the maintained
docs or link superseded research through [history](docs/history.md).
