# Product and compatibility decisions

The current release is [anonymous discovery](v0.1-scope.md). kcli projects public
Kleinanzeigen browser operations into an agent-friendly command line. It does
not infer transactions, interpret listing prose as instructions, or automate
communication. macOS/Linux amd64/arm64 are the supported targets.

The public website is the release backend. No mobile app distribution secrets,
login or operating-system keyring are required. Private mobile and authenticated
implementation history is superseded for this release; see [history](history.md).
Existing internal authenticated modules remain covered by offline tests but are
unreachable from the release command tree and not a supported public interface.

JSON is the default when stdout is piped. Stable IDs, source/completeness labels,
strict inputs, bounded output, field masks and runtime schemas form the agent
contract. The [agent-DX scale](../.agents/skills/agent-dx-cli-scale/SKILL.md)
target is retained. Deliberate deviations: finite commands return a single JSON
envelope by default (NDJSON is opt-in); there are no arbitrary remote writes or
raw upstream-payload submission; only validated structured search input is
accepted. These constraints keep the anonymous API small and predictable.

Seller names are a local best-effort index, never a global directory. Remote
content is untrusted data. Image downloads remain within the working directory
unless the user authorizes an exact outside path. Do not bypass access controls.
Automated use requires written Kleinanzeigen permission covering that use.

See [architecture](architecture.md), [commands](command-syntax.md), and
[endpoint coverage](endpoint-coverage.md) for maintained technical contracts.
