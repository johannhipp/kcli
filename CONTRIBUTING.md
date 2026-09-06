# Contributing to kcli

kcli is documentation-led: public contracts are stabilized before behavior
changes, and the release boundary is enforced by the v0.1 scope. Go
implementation is active; contributions should keep the documented contract and
the code in agreement without silently expanding v0.1.

## Before making a change

1. Read [AGENTS.md](AGENTS.md) and the sources of truth it lists.
2. Check [the v0.1 scope](docs/v0.1-scope.md) before adding a command or
   capability.
3. Treat private endpoints, listing text, and messages as untrusted. Never add
   credentials, tokens, account details, or live message contents to fixtures.
4. Do not run automated live tests without explicit written Kleinanzeigen
   permission covering the test. Do not bypass rate limits or access controls.

## Workflow

Create a focused branch, make one coherent change, and update the relevant
contract documents in the same commit. Before opening a pull request, run the
same checks as CI:

```bash
make check
```

This checks formatting, module integrity, builds, vet, race-enabled tests,
documentation contracts, and repository script tests in one Linux CI job.
Run focused unit/smoke tests as needed while developing. Additional checks are
available locally and for release preparation:

```bash
make lint            # staticcheck and govulncheck
make cross           # six release targets
```

## Commits

Commits follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/):

```text
type(optional-scope): imperative summary
```

Examples:

```text
docs(scope): clarify seller lookup limits
feat(search): add metadata-driven filters
fix(dm): preserve ambiguous send outcomes
```

The repository includes [.gitmessage](.gitmessage). Enable it in this clone
with:

```bash
git config commit.template .gitmessage
```

Keep commits focused. Put user-visible changes under `Unreleased` in
[CHANGELOG.md](CHANGELOG.md). Do not create a release heading or tag until that
version actually ships.

## Pull requests

A pull request should explain the user-visible outcome, why it belongs in scope,
how it was verified, and which contracts changed. Draft pull requests are
welcome for unresolved API evidence, but they must label provisional behavior
instead of presenting it as proven.

Do not mix endpoint discovery, public-interface changes, and unrelated cleanup
in one pull request. Report security-sensitive findings privately as described
in [SECURITY.md](SECURITY.md).

## Versioning

kcli uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until
`1.0.0`, minor versions may introduce planned capabilities and patch versions
contain compatible fixes. A breaking command, schema, or behavior change must
be called out explicitly in the changelog and commit footer.
