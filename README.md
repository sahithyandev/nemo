# nemo

a filesystem forensics CLI: hides, detects, extracts, and clears data hidden with
anti-forensic techniques (NTFS ADS, APFS/ext4 xattrs, slack space, timestomping).

## Install

No Go installation needed. On Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/sahithyandev/nemo/main/scripts/install.sh | sh
```

On Windows (PowerShell):

```powershell
powershell -c "irm https://raw.githubusercontent.com/sahithyandev/nemo/main/scripts/install.ps1 | iex"
```

Each script downloads the latest release for your OS/architecture from the
[releases page](https://github.com/sahithyandev/nemo/releases) and installs the `nemo`
binary. Read a script before piping it into a shell if you'd rather not take that on
faith; you can also download the archive for your platform from the releases page and
extract it by hand.

`hide` and `clear` mutate the target; `detect` and `extract` are read-only. Run `nemo features`
to see which filesystem/technique pairs are supported, and practice against disposable image
copies or files, not real data. See [the docs site](https://sahithyandev.github.io/nemo/) for
the full CLI reference, platform/privilege requirements for live mode, and known limitations.

## Docs

- [docs/overview.md](docs/overview.md) - goals, scope, and targeted filesystems
- [docs/cli/](docs/cli/index.md) - modes and commands
- [docs/architecture/](docs/architecture/index.md) - package layout and interfaces
- [docs/techniques/](docs/techniques/index.md) - how the hiding techniques work on disk
- [docs/architecture/decisions.md](docs/architecture/decisions.md) - shared contract decisions
- [docs/roadmap.md](docs/roadmap.md)

## Semester release candidate

The `0.1.0-rc.1` demo/review build is a prerelease. See the
[release and demo instructions](docs/release/README.md) for reproducible
four-target packages, checksums, disposable demos and validation limitations.
The one-line installers above select the latest stable release; download the
candidate explicitly from its GitHub prerelease page when reviewing it.

## Development

### Prerequisites

- Go 1.26+
- GNU Make

### Setup

After cloning the repo, run `make hooks` which will setup the git-managed hooks for the project.

Currently there is a pre-commit hook that:
- checks the `cmd/VERSION` and `CHANGELOG.md` formats (using [`scripts/check-release-files.sh`](scripts/check-release-files.sh))
- formats staged `.go` files with `gofmt` and re-stages for the commit

### Dependencies

| Name                                          | Description                                             |
| --------------------------------------------- | ------------------------------------------------------- |
| [spf13/cobra](https://github.com/spf13/cobra) | CLI framework (commands, flags, help/usage)             |
| spf13/pflag                                   | transitive dep of Cobra, POSIX-style flags              |
| inconshreveable/mousetrap                     | transitive dep of Cobra, Windows double-click detection |

### Commands

```
make build   # compile ./bin/nemo
make run     # go run .
make test    # run tests
make vet     # static checks
make fmt     # gofmt all files
make vulncheck # scan deps and Go stdlib for known vulnerabilities
```

CI runs `make vulncheck` (govulncheck) on pull requests and every Monday. A failing scheduled run means a new advisory affects a dependency or the Go version in `go.mod`; bump it to fix.

Version is read from `cmd/VERSION` and embedded into the binary at compile time (`nemo version`).

### Releasing

Releases are driven by `cmd/VERSION`. Never push tags by hand.

1. Rename `## Unreleased` in `CHANGELOG.md` to `## <version>`, matching `cmd/VERSION`.
2. Bump `cmd/VERSION` if needed.
3. Merge to `main`. CI tags `v<version>` and runs goreleaser, using that changelog section as the release notes.

Every push to `main` releases `cmd/VERSION` if it isn't released yet, whether or not the file changed in that push. If `CHANGELOG.md` has no non-empty section for the version, or the tag already exists, CI skips the release and says so in a run annotation.

`cmd/VERSION` must be a single `MAJOR.MINOR.PATCH` line, and `CHANGELOG.md` must start with `## Unreleased` followed by unique `## <version>` headings. The pre-commit hook and the first CI step both enforce this.

If the release fails after the tag is created, CI deletes the tag so the next push to `main` retries. Any GitHub release goreleaser already created is deleted too.

Check the notes extraction locally with `sh scripts/release-notes_test.sh`. Don't run goreleaser locally to release.

#### Versioning

[SemVer](https://semver.org) (`MAJOR.MINOR.PATCH`), with the usual 0ver caveat while
`MAJOR` is `0`: nothing is API-stable yet, so any bump can contain a breaking change.
Once something external depends on nemo's behavior staying put, cut `1.0.0` and start
honoring MAJOR for breaks, MINOR for features, PATCH for fixes.

Until then, bump:

- `PATCH` for a fix or docs-only change.
- `MINOR` for a new feature, filesystem, or technique.
- `MAJOR` only to mark that stability promise starting at `1.0.0`.

#### Writing the changelog

Everything lands under `## Unreleased` as it's written. One bullet per user-visible
change: what changed and why it matters to someone running `nemo`. The commits should not be listed.
Skip anything with no user-visible effect (refactors, test-only changes, CI
tweaks that don't change release behavior) unless it's the kind of operational detail
(CI, release process) covered below.

A few rules to keep entries consistent:

- Write in the present tense, describing what `nemo` does now, not what changed
  ("`clear` restores backed-up slack-space bytes", not "fixed `clear` to restore...").
- Start each bullet with the affected command, filesystem, or area (`` `hide`: ``,
  `NTFS:`, `CI:`) when it's not obvious from context.
- No em dashes, no filler phrases. Say the thing plainly, in as few words as it takes.
- Reference flags, commands, and paths with backticks.
- Group entries loosely by theme if a section has several; otherwise plain bullets
  are fine.
- One idea per bullet. Don't mix a general feature and an edge case or caveat into
  the same sentence; give the caveat its own bullet.

Releasing renames that section to the version, per the steps above; a fresh
`## Unreleased` section starts accumulating the next round of changes immediately
after.

`## Unreleased` always stays at the top of `CHANGELOG.md`, above every version
section. Version sections are in reverse chronological order below it, latest
first.

## Authors

- [Bandara S. A. N. K.](https://github.com/NirmalKBandara)
- [Sahithyan K.](https://github.com/sahithyandev)
- [Senanayake H. P. V. R.](https://github.com/Viranske-1)

## Validation and release candidate

Run detection against an explicitly supplied local dataset with the
[validation harness](docs/validation/README.md). The documented synthetic ext4
sample runs without downloading a public corpus. Public-dataset acceptance is
tracked separately from this sample.

See the [v0.1 release candidate pack](docs/release/README.md) for cross-platform
builds, Linux acceptance evidence, the demonstration plan, and remaining release
gates. `make release-package OUTPUT=/tmp/nemo-new-release` creates archives in a
new directory without tagging or publishing.

