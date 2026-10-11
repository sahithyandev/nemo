---
title: Linux Implementation
parent: Architecture
nav_order: 5
---

# Linux implementation and macOS parity

Linux uses the same `FileSystem`, `Entry`, capability interfaces and technique
layer as macOS. Its backend interprets ext4 metadata, while the macOS backend
interprets APFS metadata. Native operations use each platform's file APIs;
explicit slack reads use the image parser against verified device access.

## Implemented changes

| Area | Linux behavior |
| --- | --- |
| Xattr capacity | New attributes preserve existing inode-body values; external fallback or rejection leaves unrelated values intact |
| Directory images | Linear and validated HTree directories, including index nodes and 64 KiB record-length decoding |
| Recursive scans | Unsupported slack layouts/children are skipped; child warnings go to stderr; permission, I/O and corruption errors remain fatal |
| Live timestamps | Access/modify writes preserve the other field atomically; ctime reads and optional `statx` birth-time reads |
| Live slack | Explicit detection/extraction uses verified, read-only backing-device access; mounted writes are refused |
| Backups | Opened-file identity, ext4 UUID and a per-hide nonce protect cross-image/stale-frame restoration |
| CLI capabilities | `nemo features --mode live` explains host-specific partial/read-only/unavailable capabilities |
| Acceptance | Image/native extraction, preserved sources, permission denial and skipped-child scans are checked |

HTree traversal validates root/index records, counts, limits, referenced blocks,
hash order and duplicate names. Integration fixtures use `mke2fs`, `debugfs` and
`e2fsck -D` to build genuine indexed 1 KiB/4 KiB ext4 directories with 700 files.

Linux raw slack discovery checks the opened target's mount ID and device number
against mountinfo, handles bind roots and escaped paths, and verifies the opened
device's major/minor numbers. The raw inode must match the mounted target.
These reads can see stale or changing mounted data and are not a snapshot.

## Existing branch updates and assigned dates

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

## Validation

Run the regular checks and a native ext4 acceptance pass:

```sh
go test ./...
go vet ./...
python3 scripts/linux-acceptance.py --work-parent <ext4-directory> --require-live
```

The native pass creates disposable files and an ext4 image in the supplied
directory. It checks image named-stream/slack hide, detection, extraction and
restoration; native xattrs/timestamps/extraction; existing-output protection;
source immutability; symlink/FIFO scan warnings; permission refusal; mounted
slack-write refusal; and independently verified custody records.

The dedicated Linux CI workflow requires native coverage instead of allowing
it to silently skip. Pure/injected fixtures test mount parsing, read-only
wrapping, descriptor identity and cleanup without touching a host raw device.
Windows amd64 and macOS amd64/arm64 builds are also checked; cross-building does
not execute native Windows/macOS operations.

The final integrated version passed all of the checks above on 2026-10-12,
including native ext4 permission-denial and symlink/FIFO traversal checks,
10 independently verified custody records, and builds for Linux amd64,
Windows amd64 and macOS amd64/arm64. The backup replacement/repeated-hide
reproductions now fail before changing image bytes, and legacy frame reads are
covered by regression tests.

## Remaining boundaries

- New external xattr-block allocation, shared-block CoW and block freeing remain
  unsupported. They require allocation/checksum accounting and transactional
  design; APFS's own image writer also has constrained allocation behavior.
- Live creation/change-time writes are unavailable through ordinary Linux APIs.
- Mounted raw slack writes remain refused. Device read access requires suitable
  permissions, a visible matching device node and kernel mount-ID support.
- Privileged raw-device round trips are not part of the native file acceptance
  pass. Native macOS/Windows live tests require those operating systems.
- APFS extent-allocation and clone-sharing findings from the earlier review are
  separate backend work; this Linux update does not claim to resolve them.
- Timestomp detection still has no historical baseline. Restoring a timestamp
  requires the original value.
