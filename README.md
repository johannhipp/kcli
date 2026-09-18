# kcli

An unofficial, account-free Kleinanzeigen CLI for macOS and Linux. Browse
categories, search offers, inspect listings and images, and rediscover sellers.
No login or application credentials are required. Authentication, messaging and
marketplace writes are outside the `0.1.0` release.

## Install

Download the archive for your OS and architecture from
[GitHub Releases](https://github.com/johannhipp/kcli/releases), verify it against
`checksums.txt`, extract it, and put `kcli` on your `PATH`. Supported targets:
macOS (`darwin`) and Linux, each on Intel/AMD (`amd64`) and ARM (`arm64`).

```sh
tar -xzf kcli_VERSION_OS_ARCH.tar.gz
install -m 755 kcli "$HOME/.local/bin/kcli"
kcli version
```

Create `$HOME/.local/bin` first if needed and ensure it is on your `PATH`.
On macOS, downloaded unsigned binaries may require approval in System Settings.

## Use

```sh
kcli category search Fahrrad
kcli location resolve Berlin
kcli search Fahrrad --location Berlin --radius 10 --max-price 200 --limit 10
kcli filter list --category 217
kcli listing get 'PUBLIC_LISTING_URL'
kcli listing images 'PUBLIC_LISTING_URL'
kcli seller get --listing 'PUBLIC_LISTING_URL'
kcli seller search NAME
kcli schema show search
kcli doctor
```

Use an actual listing URL returned by search. Piped output is JSON; interactive
output is a table. `--output json`, `--fields`, `--input`, and `--help` support
scripts and agents. `schema list` exposes every supported operation.

Seller-name search covers locally encountered sellers, not a global directory.
The website controls pagination sizes. Unsupported filters fail explicitly;
there is no verified picture-required filter. Listing and image URLs can expire.

Requests are bounded and paced across processes using local state. Rate limits
and access challenges stop the request; kcli does not bypass them. Website
markup is unofficial and can change. Automated use requires written
Kleinanzeigen permission covering your use. This project is not endorsed by
Kleinanzeigen.

## Develop

```sh
go build -o bin/kcli ./cmd/kcli
make check
make cross
python3 scripts/smoke_release.py bin/kcli
```

Tests run offline. Explicit live acceptance is separate and requires permission.
Use Conventional Commits and update [CHANGELOG.md](CHANGELOG.md). CI validates
changes; successful `main` builds create a SemVer GitHub release with macOS/Linux
archives and checksums. See [Contributing](CONTRIBUTING.md).

[Scope](docs/v0.1-scope.md) · [Commands](docs/command-syntax.md) ·
[Verification](docs/test-results.md) · [Documentation](docs/README.md)
