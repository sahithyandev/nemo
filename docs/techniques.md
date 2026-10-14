---
title: Techniques
nav_group: "User Guide"
nav_order: 3
---

# Techniques

Nemo has three techniques. Pick one per command with `--technique`.

This page says what each one does and when to use it. For on-disk details and how nemo implements them, see [How techniques work](techniques/).

| Technique | What it does | Rough capacity | Survives a cross-filesystem copy | nemo can detect it |
| --------- | ------------ | -------------- | -------------------------------- | ------------------ |
| `named-stream` | Attaches a named chunk of data to a file (an NTFS Alternate Data Stream, or an ext4/APFS extended attribute). The file still opens and checksums as if nothing was added. | NTFS: up to free space. ext4/APFS xattr: a few KiB. | No | Yes |
| `slack-space` | Writes into the unused tail of the last block a file already occupies. The file's reported size and contents do not change. | Small, under one block (usually 4 KiB) per file. | No | Yes |
| `timestomp` | Rewrites a file's MACB timestamps to mislead a forensic timeline. It hides *when*, not *what*. | No payload. | Timestamps are often reset by the copy itself. | No |

## Named-stream

This is the default choice for stashing a file's worth of data. The payload hangs off the target file, so `ls`, `dir`, Finder, and a plain checksum only see the original. It needs no special privilege.

The catch is portability. Copying to FAT32 or exFAT, syncing through most cloud clients, or emailing the file drops the stream.

Tools that list streams (`dir /r`, `getfattr`, `xattr -l`) will find it. So will `nemo detect`.

## Slack-space

Use it for a small payload when the file's size must not change. The data sits in space the filesystem already gave the file, so the listed size stays put.

It is fragile. Growing or rewriting the file can overwrite the payload. Capacity is under one block.

In live mode it needs administrator (Windows) or root (macOS, Linux), because nemo opens the raw device. See [Modes](modes.html). `nemo detect` finds slack data that nemo wrote.

## Timestomp

Use it to change a timeline, not to hide bytes. It sets a file's modified, accessed, changed, and birth timestamps.

Nemo cannot detect it. There is no timestamp-anomaly detector, so `nemo detect` reports nothing for a timestomped file. That is a limit of nemo today. It does not mean a full forensic suite would miss it.

To check which filesystem and technique pairs are available, run [`nemo features`](cli/features.html).
