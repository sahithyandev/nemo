# nemo

a CLI tool for hiding files.

## Docs

- [docs/overall-plan.md](docs/overall-plan.md) - goals, scope, targeted filesystems
- [docs/user-interface.md](docs/user-interface.md) - modes and commands
- [docs/architecture.md](docs/architecture.md) - package layout, interfaces
- [docs/techniques/](docs/techniques/overview.md) - how the hiding techniques work on disk, for contributors
- [docs/work-breakdown.md](docs/work-breakdown.md)

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
```

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
