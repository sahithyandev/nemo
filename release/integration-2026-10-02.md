# Integration verification, 2026-10-02

Integrated GitHub main at `6e37f3c` with the ext4 timestamp, slack, Linux live,
and validation/release branch. Reconciled the superseded `optimize-ci` branch
without replacing main's current workflow. All other feature branches were
already contained in main.

Validation passed on the integrated source:

- `go test ./...` and `go vet ./...`.
- `make crossbuild`: Linux amd64, Windows amd64, Darwin amd64/arm64.
- `sh scripts/check-release-files.sh` and `sh scripts/release-notes_test.sh`.
- `python3 scripts/linux-acceptance.py --work-parent /home/nirmal/Projects/nemo`:
  image and native ext4 round trips, read-only detection, symlink/permission
  refusal, and independent verification of all ten custody records passed.

Acceptance artifacts are session-local at `/tmp/nemo-integration-native-acceptance`.
A tmpfs run separately passed image checks and explicitly skipped native checks.

`cmd/VERSION` remains `0.0.1`, already released on main. New changelog entries
are under `Unreleased`, so integration does not trigger a candidate publication.
Historical `0.1.0-rc.1` evidence remains tied to its recorded source commit.

Issues #22, #23, and #26 have implementation and focused acceptance evidence;
close them through the integration PR after required teammate review and merge.
The project work plan requires one teammate review and both teammates for shared
contract changes. This integration changes shared filesystem/technique contracts.

Issue #30 remains open for a verified public-dataset subset, ground truth,
versioned public-case manifest, and a recorded corpus run. Issue #33 remains
open for DOC-01/QA-01 sign-off, native Windows/macOS acceptance, and a timed
spoken demo rehearsal. Synthetic samples and cross-builds do not satisfy those
external requirements.
