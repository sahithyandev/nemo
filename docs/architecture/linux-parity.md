---
title: Linux Implementation
parent: Architecture
nav_order: 5
---

# Linux implementation and macOS parity

Linux uses the same [`FileSystem`]({{ site.src }}/internal/filesystem/filesystem.go#L24), [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31), capability interfaces, and technique layer as macOS. The Linux backend reads ext4 metadata. The macOS backend reads APFS metadata.

Native operations use each platform's file APIs. Explicit slack reads run the image parser on a verified device.

## Implemented changes

| Area | Linux behavior |
| --- | --- |
| Xattr capacity | New attributes keep existing inode-body values. An external fallback or a rejection leaves unrelated values intact |
| Directory images | Linear and validated HTree directories, including index nodes and 64 KiB record-length decoding |
| Recursive scans | Unsupported slack layouts and children are skipped, and child warnings go to stderr. Permission, I/O, and corruption errors stay fatal |
| Live timestamps | Access and modify writes keep the other field atomically. Reads cover ctime and optional `statx` birth time |
| Live slack | Explicit detection and extraction use verified, read-only backing-device access. Mounted writes are refused |
| Backups | Opened-file identity, ext4 UUID, and a per-hide nonce guard against cross-image and stale-frame restores |
| CLI capabilities | `nemo features --mode live` explains partial, read-only, and unavailable capabilities on the host |
| Acceptance | Image and native extraction, preserved sources, permission denial, and skipped-child scans are checked |

### HTree checks

HTree traversal validates the root and index records, counts, limits, referenced blocks, hash order, and duplicate names. Integration fixtures use `mke2fs`, `debugfs`, and `e2fsck -D` to build real indexed 1 KiB and 4 KiB ext4 directories with 700 files.

### Raw slack discovery

Linux checks the opened target's mount ID and device number against mountinfo. It handles bind roots and escaped paths. It also verifies the opened device's major and minor numbers, and the raw inode must match the mounted target.

These reads can see stale or changing mounted data. They are not a snapshot.

## Validation

Run the regular checks and a native ext4 acceptance pass:

```sh
go test ./...
go vet ./...
python3 scripts/linux-acceptance.py --work-parent <ext4-directory> --require-live
```

The native pass creates disposable files and an ext4 image in the directory you give it. It checks:

- image named-stream and slack hide, detection, extraction, and restoration
- native xattrs, timestamps, and extraction
- protection of existing output files
- source immutability
- symlink and FIFO scan warnings
- permission refusal
- refusal of mounted slack writes
- independently verified custody records

The Linux CI workflow requires native coverage instead of letting it skip silently. Pure and injected fixtures test mount parsing, read-only wrapping, descriptor identity, and cleanup without touching a host raw device.

Windows amd64 and macOS amd64/arm64 builds are also checked. Cross-building does not run native Windows or macOS operations.

The final integrated version passed all of these checks on 2026-10-12. That run included native ext4 permission-denial and symlink/FIFO traversal checks, 10 independently verified custody records, and builds for Linux amd64, Windows amd64, and macOS amd64/arm64. The backup replacement and repeated-hide reproductions now fail before changing image bytes. Regression tests cover legacy frame reads.

## Remaining boundaries

- New external xattr-block allocation, shared-block copy-on-write, and block freeing are unsupported. They need allocation and checksum accounting plus a transactional design. APFS's own image writer also has limited allocation behavior.
- Live creation-time and change-time writes are unavailable through ordinary Linux APIs.
- Mounted raw slack writes are refused. Device read access needs suitable permissions, a visible matching device node, and kernel mount-ID support.
- The native file acceptance pass does not include privileged raw-device round trips. Native macOS and Windows live tests need those operating systems.
- APFS extent-allocation and clone-sharing findings from the earlier review are separate backend work. This Linux update does not claim to resolve them.
- Timestomp detection still has no historical baseline. Restoring a timestamp needs the original value.
