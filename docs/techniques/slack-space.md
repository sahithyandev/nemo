# Slack-Space Hiding

## What it is

Filesystems allocate storage in fixed-size units: a cluster on NTFS, a block on
ext4 and APFS, 4 KiB by default. A file almost never ends exactly on a unit
boundary, so the last unit is only partly used. The filesystem still counts the
whole unit as allocated to that file, and the leftover bytes at the tail belong
to nobody and are read by nothing. Those leftover bytes are slack, and you can
write a payload there.

Walk the arithmetic. Take a 10,000-byte file on a filesystem with 4096-byte
blocks:

```
ceil(10000 / 4096) = 3 blocks allocated  = 12288 bytes reserved
file content                             = 10000 bytes used
slack                                    =  2288 bytes  <-- hiding spot
```

The file's reported size stays 10,000. Reading the file returns 10,000 bytes.
`du` reports 12 KiB, which it always did, because that is just the allocation.
Nothing about the file's visible behaviour changes when those 2288 bytes get
written.

## Kinds of slack

```
one 4096-byte block:
+-----------------------------+---------------------+
|   file's last 10000-9*4096  |       slack         |
|          bytes              |   payload goes here  |
+-----------------------------+---------------------+
                              ^ end of file content
```

- File slack is the tail of the last data block, as above. This is the main
  target.
- RAM slack was, on older systems, the portion of file slack between end-of-file
  and the next sector boundary, which those systems padded with in-memory
  garbage. Modern systems zero it, so it is no longer separately useful.
- Metadata-record slack is the unused tail of a record. An NTFS MFT record is
  1024 bytes and a small file uses only a few hundred of them; the rest is slack
  that needs no data block at all. ext4's in-inode xattr gap works the same way.
- Volume or partition slack is the space between the end of the filesystem and
  the end of the partition, or between the last partition and the end of the
  disk. It is volume-level rather than per-file, so it is out of scope for nemo's
  per-target `hide`.

## On-disk mechanics

nemo's slack write is filesystem-agnostic at the technique layer. A filesystem's
`Entry` implements `filesystem.SlackSpaceCapable` and returns
`[]filesystem.SlackRegion{ {Offset, Length} }`, giving absolute byte offsets into
the image. The technique picks the first region big enough, reads the bytes it is
about to overwrite so it can back them up, and writes the frame. What differs per
filesystem is how the regions get computed.

### NTFS

- Cluster slack: parse the file's `$DATA` data-runs to find the last cluster,
  compute `end_of_file mod cluster_size`, and the region runs from
  `last_cluster_start + used` to `last_cluster_start + cluster_size`.
- Resident-`$DATA` slack: when the file's contents are resident in the MFT
  record, the free space between the end of the last attribute and the record's
  `bytes_allocated` (typically 1024) is usable, though `chkdsk` and any record
  rewrite reclaim it.

### ext4

- Block slack: the file uses extents (an `ext4_extent` tree), so walk to the last
  extent, and the slack is the tail of its last block.
- Three features change or remove the slack:
  - Inline data (`inline_data`): a tiny file's contents live in the inode and the
    in-inode xattr area, so there is no data block and no block slack.
  - `bigalloc`: allocation happens in multi-block clusters, so there is
    proportionally more slack, but the unit size is not the block size.
  - Delayed allocation: a freshly written file may not have blocks assigned yet,
    so slack does not exist until the writeback.

### APFS

- Block slack works the same way: the tail of the file's last allocated block.
- The copy-on-write caveat matters here more than anywhere else. APFS never
  overwrites a live block. Any modification to the file, including one the OS
  makes for its own reasons, writes the changed data to a new block and repoints
  the file's extent at it. The old block, with your payload in its slack, becomes
  free, or gets pinned by a snapshot and frozen. A slack payload on APFS is
  therefore valid only until the next write to that file, which you do not
  control. Automatic `tmutil` local snapshots make this worse.

## Why it is fragile

Contributors get this wrong more than anything else about the technique. A slack
payload is destroyed by any of:

- Appending to the file, which fills the last block or allocates a new one.
- Truncating or rewriting the file.
- Defragmentation (`e4defrag`, Windows `defrag`), which relocates the file's
  blocks and frees the old ones, slack included.
- TRIM, `fstrim`, or discard, which tells the SSD the freed region is unused so
  it may physically zero it. On a thin-provisioned or encrypted volume the slack
  can read back as zeros even before that.
- Any copy-on-write rewrite on APFS, as above.
- Imaging with a tool that copies only allocated file content rather than raw
  sectors, which leaves the slack out of the copy.

Treat slack hiding as volatile storage. If the payload has to persist, this is
the wrong technique.

## Privilege

Reading and writing slack needs raw device access, because no normal file API on
any OS exposes bytes past end-of-file. In nemo's image mode the image is the raw
bytes, so nothing beyond read access to the file is required. In live mode,
slack-space means opening `\\.\PhysicalDriveN`, `/dev/diskN`, or `/dev/sdX` and
running the same parser, which needs admin or root. Per `docs/architecture.md`,
live `hide`, `detect`, and `clear` with `--technique slack-space` must detect the
missing-privilege condition and fail with a clear error rather than silently
degrade. `named-stream` and `timestomp` do not have this requirement; they go
through ordinary syscalls.

## nemo's payload frame

Raw slack normally holds whatever residual bytes the last file to own that block
left behind, so an arbitrary payload cannot be told apart from that noise.
`detect` would have nothing to key on, and `clear` would not know how many bytes
it wrote. nemo therefore wraps every slack payload in a 12-byte frame
(`internal/technique/slackframe.go`):

```
offset  size  field
  0      4    magic    ASCII "NEMO"
  4      4    length   payload length, uint32 little-endian
  8      4    crc32    CRC-32 (IEEE polynomial) of the payload
 12    length payload  the hidden bytes
```

`detect` reports a slack finding only when the magic matches and the CRC
validates (`decodeFrame` and `readFrame` in `technique.go`). This is a deliberate
trade-off. It makes nemo's own payloads easy to spot, since the literal string
`NEMO` at a block boundary followed by a valid CRC is not subtle, and in exchange
`hide`, `detect`, and `clear` stay reliable and reversible. nemo is a forensic
and CTF tool, not a stealth implant. Hiding from a determined examiner is a
non-goal.

The payload length is capped at `math.MaxUint32`, about 4 GiB, by the frame's
`length` field. In practice a region is one block's slack, a few KiB.

## Reversibility: the manifest

Before overwriting slack bytes, `slackSpaceTechnique.Hide` calls `Request.Backup`
with the original residual bytes. The command layer persists that as one JSON
Lines record per line in `nemo-manifest.jsonl` (`--manifest` to relocate), with
`Original` base64-encoded. See `internal/technique/manifest.go` (`AppendManifest`,
`LoadManifest`, `LatestBackup`). A `hide` aborts before touching disk if the
manifest write fails.

`clear` replays it. With a matching manifest record it writes the original
residual bytes back over the frame and reports `Result.Restored = true`. Without
one it zero-fills the frame and reports `Restored = false`. A `Request.Restore`
slice whose length does not match the frame is rejected, not zero-filled
(`slackSpaceTechnique.Clear`). Later manifest records win, so re-hiding then
clearing a target restores the most recent hide's bytes.

## Detection by a third party

- Scan every allocated file's last cluster or block, and compare `size mod
  cluster_size` against what the slack actually contains. Non-zero, high-entropy
  slack is the flag, especially when it matches across many files or contains
  recognisable headers. The Sleuth Kit exposes slack via `blkls -s`.
- For MFT-record and inode slack, compare used attribute length against record
  size.
- For nemo specifically, grep the raw device for `NEMO` at 512-byte or 4096-byte
  alignment.

## Traces left behind

- The payload bytes stay in the block until that block gets reallocated and
  overwritten, so raw-sector imaging recovers them even after `clear` zero-fills,
  as long as `clear` was never run.
- Writing slack does not update the file's size, mtime, or block bitmap, so there
  is no filesystem-metadata trace. That is why sector-level scanning, not
  metadata inspection, is how it gets found.
- On a journalling filesystem the raw block write still passes through the block
  layer's write cache but is not journalled, since it is not filesystem metadata.

## How nemo implements it

`filesystem.SlackSpaceCapable`:

```go
SlackRegions() ([]filesystem.SlackRegion, error)  // {Offset, Length} into the image
```

`slackSpaceTechnique` (`internal/technique/technique.go`) requires
`Request.Image` (image-backed storage), frames the payload, and writes into the
first region that fits.

- No real filesystem implements this yet. ext4, APFS, and NTFS all need
  `SlackRegions` added, computing the region from the last extent or data-run.
- `fakefs.Entry.SlackRegions` (`internal/filesystem/fakefs/fakefs.go`) is the
  reference. It returns regions pointing into its backing byte-slice image, and
  it is what the technique's tests run against.
