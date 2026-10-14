---
title: Slack-Space Hiding
parent: How techniques work
nav_order: 2
---

# Slack-space hiding

## What it is

Filesystems allocate storage in fixed-size units. NTFS calls them clusters. ext4 and APFS call them blocks. All three default to 4 KiB.

A file almost never ends exactly on a unit boundary, so its last unit is only partly used. The filesystem still counts the whole unit as the file's. The leftover bytes at the tail belong to nobody, and nothing reads them. That is slack, and you can write a payload there.

Here is the arithmetic for a 10,000-byte file on 4096-byte blocks:

```text
ceil(10000 / 4096) = 3 blocks allocated  = 12288 bytes reserved
file content                             = 10000 bytes used
slack                                    =  2288 bytes  <-- hiding spot
```

The file's reported size stays 10,000. Reading it returns 10,000 bytes. `du` reports 12 KiB, as it always did, because that is just the allocation. Writing those 2288 bytes changes nothing you can see.

## Kinds of slack

```text
one 4096-byte block:
+-----------------------------+---------------------+
|   file's last 1808 bytes    |       slack         |
|   (10000 - 2*4096)          |   payload goes here  |
+-----------------------------+---------------------+
                              ^ end of file content
```

- **File slack** is the tail of the last data block, as above. This is the main target.
- **RAM slack** was the part of file slack between end-of-file and the next sector boundary. Older systems padded it with in-memory garbage. Modern systems zero it, so it is no longer useful on its own.
- **Metadata-record slack** is the unused tail of a record. An NTFS MFT record is 1024 bytes and a small file uses a few hundred. The rest is slack that needs no data block. ext4's in-inode xattr gap works the same way.
- **Volume or partition slack** is the space between the end of the filesystem and the end of the partition, or between the last partition and the end of the disk. It belongs to the volume, not a file, so nemo's per-target `hide` does not cover it.

## Why it is fragile

Contributors get this wrong more than anything else about the technique. Any of these destroys a slack payload:

- Appending to the file, which fills the last block or allocates a new one.
- Truncating or rewriting the file.
- Defragmentation (`e4defrag`, Windows `defrag`). It moves the file's blocks and frees the old ones, slack included.
- TRIM, `fstrim`, or discard. These tell the SSD the freed region is unused, so it may zero it. On a thin-provisioned or encrypted volume the slack can read back as zeros even sooner.
- Any copy-on-write rewrite on APFS.
- Imaging with a tool that copies allocated file content instead of raw sectors. The slack is left out of the copy.

Treat slack as volatile storage. If the payload has to persist, use a different technique.

## Privilege

Reading and writing slack needs raw device access. No normal file API on any OS exposes bytes past end-of-file.

- **Image mode:** the image is the raw bytes, so read access to the file is enough.
- **Live mode:** nemo opens `\\.\PhysicalDriveN`, `/dev/diskN`, or `/dev/sdX` and runs the same parser. That needs admin or root.

Per [Modes](../architecture/modes.html), live `hide`, `detect`, and `clear` with `--technique slack-space` must notice the missing privilege and fail with a clear error. They must not quietly degrade. `named-stream` and `timestomp` have no such requirement, because they use ordinary syscalls.

### macOS (APFS)

The code is in `internal/filesystem/apfs/live_darwin.go`.

- A live slack **write** always fails with a "busy" error. macOS refuses a read-write open of the buffered device while its volume is mounted.
- `detect` opens the raw character device instead. macOS allows that read-only while mounted, so it works.
- Whether `detect` needs `sudo` depends on who owns the device node. A disk image you attached yourself usually does not. Your boot volume does.
- A permission failure names the device and suggests `sudo`.

See [APFS live mode](../file-systems/apfs.html#live-mode-macos).

### Linux (ext4)

Explicit read-only live detection and extraction work through verified mount and device discovery. They need device read permission and kernel mount-ID support.

Mounted `hide` and `clear` slack writes are refused. Raw reads can see unflushed or concurrent changes, so use an offline image when you need a consistent view. Native xattrs and timestamps still use ordinary file permissions.

## nemo's payload frame

Raw slack holds whatever the last file to own that block left behind. An arbitrary payload looks the same as that noise. `detect` would have nothing to match on, and `clear` would not know how many bytes it wrote.

So nemo writes each new slack payload behind a 28-byte versioned header (`internal/technique/slackframe.go`):

```text
offset  size  field
  0      4    magic    ASCII "NEM2"
  4      4    length   payload length, uint32 little-endian
  8      4    crc32    CRC-32 (IEEE polynomial) of the payload
 12     16    nonce    random identity for this hide operation
 28    length payload  the hidden bytes
```

`detect` reports a slack finding only when the magic matches and the CRC validates ([`decodeFrame`]({{ site.src }}/internal/technique/slackframe.go#L41) and [`readFrame`]({{ site.src }}/internal/technique/technique.go#L461) in [`technique.go`]({{ site.src }}/internal/technique/technique.go)).

This is a deliberate trade-off. The string `NEM2` (or the older `NEMO`) followed by a valid CRC is easy to spot. In return, `hide`, `detect`, and `clear` stay reliable and reversible. Nemo is a forensic and CTF tool, not a stealth implant. Hiding from a determined examiner is a non-goal.

The `length` field caps a payload at `math.MaxUint32`, about 4 GiB. In practice a region is one block's slack, a few KiB.

## Reversibility: the manifest

Before overwriting slack bytes, [`slackSpaceTechnique.Hide`]({{ site.src }}/internal/technique/technique.go#L201) calls [`Request.Backup`]({{ site.src }}/internal/technique/technique.go#L86) with the original residual bytes.

The command layer saves that as one JSON Lines record per line in `nemo-manifest.jsonl`. Use `--manifest` to move it. `Original` is base64-encoded. The code is in `internal/technique/manifest.go` ([`AppendManifest`]({{ site.src }}/internal/technique/manifest.go#L24), [`LoadManifest`]({{ site.src }}/internal/technique/manifest.go#L41), [`LatestImageBackup`]({{ site.src }}/internal/technique/manifest.go#L125)). If the manifest write fails, `hide` aborts before touching the disk.

### Identity checks

New records include two extra fields.

- `ImageIdentity`: the opened file's device and inode on Unix, or volume serial and file index on Windows. It also holds the size, plus the ext4 UUID when nemo recognizes one.
- `FrameSHA256`: the exact frame that `hide` wrote.

Symlink and hardlink aliases resolve to the same identity, and so do moves within a filesystem. Copied, resized, or replaced images cannot reuse these backups automatically. Non-file images have process-local identities.

An in-place ext4 replacement with a different UUID also fails the match. Re-hiding an identical payload produces a new nonce, which fails stale-frame validation.

### Clearing

`clear` checks the image identity, target, and offset range. Then it verifies the current frame digest before restoring. A stale frame is an error before any write.

- **Matching record:** `clear` writes the original bytes back over the frame and sets `Result.Restored = true`.
- **No matching record:** `clear` zero-fills the frame and sets `Restored = false`.
- **Legacy record** (no identity or digest): never restored automatically. The default manifest prints a diagnostic and zero-fills. An explicit `--manifest` needs a matching image-bound record, or it fails without changing anything. Keep the original image and manifest if you need to recover a legacy backup by hand.

A [`Request.Restore`]({{ site.src }}/internal/technique/technique.go#L81) slice that does not match the frame length is rejected, not zero-filled ([`slackSpaceTechnique.Clear`]({{ site.src }}/internal/technique/technique.go#L288)). Later manifest records win, so re-hiding and then clearing restores the most recent hide's bytes.

The older `NEMO` format has a 12-byte header. `detect`, `extract`, and `clear` can still read it. New frames carry a random nonce, so repeated hides of the same payload cannot reuse a stale digest.

## Detection by a third party

- **Scan the last cluster or block of every allocated file.** Compare `size mod cluster_size` with what the slack holds. Non-zero, high-entropy slack is the flag. It is stronger if it repeats across many files or contains recognizable headers. The Sleuth Kit exposes slack through `blkls -s`.
- **For MFT-record and inode slack,** compare the used attribute length with the record size.
- **For nemo specifically,** scan raw slack for `NEM2` or the older `NEMO` at 512-byte or 4096-byte alignment.

## Traces left behind

- The payload stays in the block until that block is reallocated and overwritten. Raw-sector imaging recovers it, even after a `clear` that zero-filled the frame, if the block was not reused.
- Writing slack does not change the file's size, mtime, or the block bitmap. There is no filesystem-metadata trace. That is why examiners find it by sector-level scanning, not metadata inspection.
- On a journalling filesystem, the raw block write passes through the block layer's write cache but is not journalled, because it is not filesystem metadata.

## Implementation Notes

### On-disk mechanics

The technique layer writes slack the same way on every filesystem. A filesystem's [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) implements [`filesystem.SlackSpaceCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L52) and returns `[]filesystem.SlackRegion{ {Offset, Length} }`. Those are absolute byte offsets into the image.

The technique picks the first region big enough, reads the bytes it is about to overwrite so it can back them up, and writes the frame. What differs per filesystem is how the regions are computed.

#### NTFS

- **Cluster slack:** parse the file's `$DATA` data runs to find the last cluster. Compute `end_of_file mod cluster_size`. The region runs from `last_cluster_start + used` to `last_cluster_start + cluster_size`.
- **Resident-`$DATA` slack:** when the contents are resident in the MFT record, the free space between the last attribute and the record's `bytes_allocated` (typically 1024) is usable. `chkdsk` and any record rewrite reclaim it.

#### ext4

- **Block slack:** the file uses extents (an `ext4_extent` tree). Walk to the last extent. The slack is the tail of its last block.
- **Three features change or remove the slack:**
  - `inline_data`: a tiny file's contents live in the inode and the in-inode xattr area. There is no data block, so there is no block slack.
  - `bigalloc`: allocation happens in multi-block clusters. There is more slack, but the unit is not the block size.
  - Delayed allocation: a freshly written file may not have blocks yet. The slack does not exist until writeback.

#### APFS

- The code is in `internal/filesystem/apfs/slack.go`. Every allocated byte past the file's logical size counts as slack. Nemo computes it per `FILE_EXTENT`, not just for the last one. A file with unused whole blocks before its final partial block gets a region for each.
- The logical size and physical extents come from the inode's `j_dstream_t` extended field, keyed by its `private_id`. A stream-backed xattr's data uses the same lookup.
- Nemo refuses a `com.apple.decmpfs`-compressed file, or one whose data stream has a non-zero `default_crypto_id` (per-file encryption). A region computed against a meaningless logical size would be wrong. See [the APFS parser doc](../file-systems/apfs.html#slack-space-slackgo).
- **Copy-on-write matters here more than anywhere else.** APFS never overwrites a live block. Any change to the file, including one the OS makes for its own reasons, writes the new data to a fresh block and repoints the extent. The old block, with your payload in its slack, is freed or frozen by a snapshot.
- So an APFS slack payload is valid only until the next write to that file, and you do not control that. Automatic `tmutil` local snapshots make it worse.

### In nemo

[`filesystem.SlackSpaceCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L52):

```go
SlackRegions() ([]filesystem.SlackRegion, error)  // {Offset, Length} into the image
```

[`slackSpaceTechnique`]({{ site.src }}/internal/technique/technique.go#L197) (`internal/technique/technique.go`) requires [`Request.Image`]({{ site.src }}/internal/technique/technique.go#L76), which is image-backed storage. It frames the payload and writes into the first region that fits.

- APFS, ext4, and NTFS implement [`SlackRegions`]({{ site.src }}/internal/filesystem/filesystem.go#L53), subject to each backend's layout limits. ext4 uses the final allocated block of a regular file.
- `fakefs.Entry.SlackRegions` (`internal/filesystem/fakefs/fakefs.go`) is the reference. It returns regions that point into its backing byte-slice image, and the technique's tests run against it.

## Further reading

- [Slack space](https://en.wikipedia.org/wiki/Slack_space) on Wikipedia, a short intro.
- [The Sleuth Kit](https://www.sleuthkit.org/sleuthkit/docs.php) for tools such as `blkls -s` that read slack.
- [ext4 dynamic structures](https://docs.kernel.org/filesystems/ext4/dynamic.html) for extents and inline data.
- [More in References](../references.html).
