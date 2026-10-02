# Nirmal — Remaining Work Report

## Implementation update — 2026-10-01

Work branch: `NirmalKBandara/val-02-rel-01-validation-release`.

- Integrated fetched `origin/main` (`3f97794`) with the existing ext4 branch history
  in merge commit `e694ffe`; preserved timestamp, slack and Linux live work.
- Retained the shared CRC slack format and detect/clear/backup APIs from main.
  Corrected default ext4 scans so directory slack does not abort traversal.
- Implemented local-image validation with strict manifests, exact expected-finding
  scoring, per-filesystem/technique totals, read-only access and explicit
  missing/unsupported/error outcomes. See [validation](docs/validation/README.md).
- Added disposable Linux/ext4 acceptance and independent custody checks. See
  [acceptance evidence](docs/release/linux-acceptance.md).
- Prepared `0.1.0-rc.1`, reproducible Linux/macOS/Windows packages, release notes
  and an eight-minute demo plan. See [release pack](docs/release/README.md).
- Clean-clone test/build/package results are recorded in
  [rehearsal evidence](docs/release/rehearsal.md).

Still external/pending: a verified local public-dataset subset and ground truth;
team review (both teammates for shared interface changes); DOC-01/QA-01 sign-off;
native Windows/macOS runtime acceptance; and a timed spoken presentation rehearsal.
The branch is local; no PR, remote-main merge, final tag or publication is claimed.
The public-dataset requirement is not satisfied by the synthetic ext4 sample.
`workDivide.md` and `create-issues.sh` remain local planning inputs.

The report below is the original September snapshot, retained for assignment
context; its branch counts and unchecked items are historical.

---

**Prepared:** 2026-09-23  
**Source of assignment:** `workDivide.md`  
**Status basis:** local Git history and branches; this report does not claim the current status of GitHub issues or pull requests because no live GitHub check was performed.

> Note: `workDivide.md` and `create-issues.sh` are currently untracked files. The older `docs/work-breakdown.md` names generic owners and still lists the validation harness owner as TBD. This report follows the newer, explicit assignment to Nirmal in `workDivide.md` and `create-issues.sh`; commit those planning files once the team confirms them.

## Summary

Nirmal has **two implementation/delivery issues that have not been started in the local repository**:

1. `VAL-02` — build the public-dataset validation harness.
2. `REL-01` — prepare the v0.1 semester release and demo pack.

There are also **three completed ext4 feature branches that still need to be integrated into `main`**:

- `FS-EXT4-03` — timestomping.
- `FS-EXT4-04` — slack-space operations.
- `FS-EXT4-05` — Linux live mode.

The current branch passes `go test ./...` and `go vet ./...` when a writable Go build cache is used.

## Remaining work, in recommended order

### 1. Finish integration of `FS-EXT4-03`, `FS-EXT4-04`, and `FS-EXT4-05`

The code exists locally, but none of these branch tips is contained in local `main`.

#### `FS-EXT4-03` — ext4 timestomping

- Branch: `NirmalKBandara/fs-ext4-03-timestomp`
- Local state: 5 commits ahead of `main`.
- Remote state: the local branch is 4 commits ahead of its remote branch.
- Remaining actions:
  - Push the missing local commits.
  - Open or update the pull request.
  - Obtain the required teammate review.
  - Address review findings and keep tests/vet green.
  - Merge into `main`.

#### `FS-EXT4-04` — ext4 slack-space operations

- Branch: `NirmalKBandara/fs-ext4-04-slack-space`
- Local state: 11 commits ahead of `main` and includes the preceding timestomp work.
- No matching remote-tracking branch is present locally.
- Remaining actions:
  - Integrate `FS-EXT4-03` first, then rebase or merge the latest `main` as appropriate.
  - Publish the branch.
  - Open a pull request linked to the issue.
  - Obtain review and address findings.
  - Re-run the hide/detect/clear acceptance round trip on a disposable ext4 image.
  - Merge into `main`.

#### `FS-EXT4-05` — Linux live mode

- Branch: `NirmalKBandara/fs-ext4-05-linux-live-mode`
- Local and remote branch tips match.
- Local state: 17 commits ahead of `main` and includes the preceding ext4 branches.
- Remaining actions:
  - Integrate `FS-EXT4-03` and `FS-EXT4-04` first, then update this branch against `main`.
  - Open or finish its pull request.
  - Obtain review and address findings.
  - Record Linux acceptance evidence for xattr/named-stream and timestomp round trips, symlinks, privilege failures, unsupported live slack behavior, and non-Linux builds.
  - Merge into `main`.

### 2. `VAL-02` — Build the public-dataset validation harness

**Planned effort:** 2 days  
**Dependency:** `CLI-02` plus named-stream support for NTFS, APFS, and ext4.

Implement a deterministic harness that runs detection against a **locally supplied** subset of the `fkie-cad/hide-and-seek` dataset and scores expected findings.

Required work:

- Accept the dataset location as an argument; do not download data silently.
- Add a version-controlled manifest of expected findings.
- Run detection against the selected dataset cases.
- Report pass/fail and totals per filesystem and technique.
- Distinguish missing or unsupported cases from genuine detection failures.
- Document and run at least one sample end to end.
- Add automated tests for manifest parsing, scoring, and error handling.
- Commit validation results suitable for the Week 5 acceptance and release evidence.

Definition of done:

- A repeatable command can validate a locally supplied dataset subset.
- The output clearly shows scores and unsupported/missing cases.
- At least one documented sample completes end to end.
- Tests and vet pass.

### 3. Support Week 5 cross-platform acceptance

`QA-01` belongs to Sahithyan, but it depends on `VAL-02`, and Nirmal owns the Linux/ext4 implementation end to end.

Nirmal must provide:

- Linux results for `version`, `features`, and command help.
- Linux live-mode hide/detect/clear evidence for supported techniques.
- At least one ext4 image-mode round trip.
- Independent verification of custody hashes for Nirmal's mutations.
- Evidence that detection does not modify its source.
- Reproduction details and fixes for release-blocking Linux/ext4 defects.
- Honest documentation of any deferred coverage or limitations.

### 4. `REL-01` — Prepare the v0.1 semester release and demo pack

**Planned effort:** 1.5 days  
**Dependencies:** `DOC-01` and `QA-01`, which themselves rely on the integrated platform work and `VAL-02`.

Required work:

- Update the version through `cmd/VERSION`.
- Prepare release notes covering:
  - implemented features;
  - validation and test evidence;
  - known limitations and deferred work;
  - destructive-operation and disposable-fixture safety warnings.
- Provide binaries or reproducible build instructions for Linux, macOS, and Windows.
- Prepare a concise semester demonstration plan showing supported workflows, forensic safeguards, validation results, and limitations.
- Use only disposable fixtures in the demonstration.
- Rehearse the demo within the allocated presentation time.
- Perform and record a clean-clone build/test/release rehearsal.
- Tag/package the v0.1 release only after the required acceptance and documentation work is complete.

Definition of done:

- The version and release notes agree.
- All three supported OS targets have artifacts or verified build instructions.
- The demo is reproducible, safe, and fits its time limit.
- A clean clone can reproduce the release package.

## Ongoing responsibilities until release

These are not separate numbered issues, but they are part of Nirmal's assigned ownership:

- Own ext4 parser, xattrs, timestamps, slack space, Linux live mode, platform tests, and review fixes through release.
- Use one issue branch per issue and link pull requests with `Closes #<issue-number>`.
- Get at least one teammate review; contract changes require both teammates.
- Update from `main` before final review.
- Keep `go test ./...` and `go vet ./...` green.
- Test mutations only on disposable images/files/snapshots and verify custody logs.
- Raise dependency blockers on the same day.
- Attend the twice-weekly checkpoint with completed work, next work, blockers, and a native-platform demo.
- Record reduced coverage honestly in the feature matrix and release notes.

## Original assignment status

| Issue | Assignment | Status from local repository |
| --- | --- | --- |
| `CORE-01` | Image abstraction | Merged into `main` |
| `CORE-04` | Filesystem/capability interfaces | Merged into `main` |
| `CLI-01` | Hide command | Merged into `main` |
| `CLI-04` | Features command | Merged into `main` |
| `FS-EXT4-01` | ext4 parser | Merged into `main` |
| `FS-EXT4-02` | ext4 xattrs | Merged into `main` |
| `FS-EXT4-03` | ext4 timestomp | Implemented locally; integration remains |
| `FS-EXT4-04` | ext4 slack space | Implemented locally; publication/review/integration remain |
| `FS-EXT4-05` | Linux live mode | Implemented and pushed; review/integration remain |
| `VAL-02` | Public-dataset harness | Remaining |
| `REL-01` | v0.1 release/demo pack | Remaining |

## Immediate checklist

- [ ] Push the four unpublished `FS-EXT4-03` commits.
- [ ] Review and merge `FS-EXT4-03`.
- [ ] Publish, review, and merge `FS-EXT4-04`.
- [ ] Update, review, and merge `FS-EXT4-05`.
- [ ] Implement and document `VAL-02`.
- [ ] Supply Linux/ext4 evidence to `QA-01` and fix release blockers.
- [ ] Wait for/coordinate completion of `DOC-01` and `QA-01`.
- [ ] Complete `REL-01`, rehearse from a clean clone, and prepare the v0.1 tag/package.
