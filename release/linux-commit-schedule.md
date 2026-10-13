# Linux ext4 branch updates and assigned commit dates

These changes were implemented and checked on **2026-10-12**. Author/committer
dates were assigned within **2026-08-02 through 2026-10-11** at the repository
owner's request; they describe the requested commit metadata schedule, not when
the implementation was performed. Existing historical commits were not rewritten.

| Assigned date | Existing branch | Change |
| --- | --- | --- |
| Aug 3 | `NirmalKBandara/fs-ext4-02-xattrs` | Preserve xattrs on capacity failures |
| Aug 10 | `feature/fs-ext4-01-parser` | HTree traversal and malformed-index coverage |
| Aug 17 | `NirmalKBandara/fs-ext4-04-slack-space` | Unsupported-layout detection behavior |
| Aug 24 | `NirmalKBandara/fs-ext4-05-linux-live-mode` | Resilient scans with skipped-child diagnostics |
| Aug 31 | `NirmalKBandara/fs-ext4-03-timestomp` | Native timestamp reads and atomic preservation |
| Sep 1 | `NirmalKBandara/CLI-01-hide-command` | Opened backing-file identity on Unix/Windows |
| Sep 7 | `NirmalKBandara/CLI-01-hide-command` | Image/frame-bound restoration |
| Sep 14 | `NirmalKBandara/fs-ext4-05-linux-live-mode` | Verified read-only live slack discovery |
| Sep 21 | `NirmalKBandara/CLI-04-features-command` | Native capability matrix |
| Sep 28 | `NirmalKBandara/val-02-rel-01-validation-release` | Required native ext4 CI acceptance |
| Oct 5 | `NirmalKBandara/val-02-rel-01-validation-release` | Extraction and recursive-scan acceptance |
| Oct 8 | `NirmalKBandara/CLI-01-hide-command` | Filesystem/repeated-hide backup identity hardening |
| Oct 11 | `NirmalKBandara/val-02-rel-01-validation-release` | Consolidated implementation and validation documentation |

The validation branch integrates the focused branches. Integration merge commits
are distinct from implementation commits. No remote push or publication is part
of this update.
