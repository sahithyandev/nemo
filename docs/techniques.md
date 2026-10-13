---
title: Techniques
nav_group: "User Guide"
nav_order: 3
---

# Techniques

nemo hides, detects, and clears data with three techniques, picked per command
with `--technique`. This page is what each one does and when to reach for it. For
the on-disk mechanics, per-filesystem detail, and nemo's implementation, see
[How techniques work](techniques/).

| Technique | What it does | Rough capacity | Survives a cross-filesystem copy | nemo can detect it |
| --------- | ------------ | -------------- | -------------------------------- | ------------------ |
| `named-stream` | Attaches a named chunk of data to a file (an NTFS Alternate Data Stream, or an ext4/APFS extended attribute). The file still opens and checksums as if nothing was added. | NTFS: up to free space. ext4/APFS xattr: a few KiB. | No | Yes |
| `slack-space` | Writes into the unused tail of the last block a file already occupies. The file's reported size and contents do not change. | Small, under one block (usually 4 KiB) per file. | No | Yes |
| `timestomp` | Rewrites a file's MACB timestamps to mislead a forensic timeline. It hides *when*, not *what*. | No payload. | Timestamps are often reset by the copy itself. | No |

## Named-stream

The practical default for stashing a file's worth of data that reads back
cleanly. The payload hangs off the target file, so `ls`, `dir`, Finder, and a
plain checksum all see only the original. It needs no special privilege. The
catch is portability: copying to FAT32/exFAT, syncing through most cloud clients,
or emailing the file drops the stream. A forensic tool that enumerates streams
(`dir /r`, `getfattr`, `xattr -l`) finds it, and so does `nemo detect`.

## Slack-space

For a small payload you want to leave no size change behind. Because it lives in
space the filesystem already allocated to the file, nothing about the file's
listed size shifts. It is fragile: growing or rewriting the file can overwrite
the payload, and the capacity is under a single block. In live mode it needs
administrator (Windows) or root (macOS, Linux) because it has to open the raw
device; see [Modes](modes.html). `nemo detect` finds nemo-framed slack.

## Timestomp

Use this to alter a timeline rather than hide bytes: it changes a file's
modified, accessed, changed, and birth timestamps. nemo cannot detect it, because
there is no timestamp-anomaly detector, so `nemo detect` reports nothing for a
timestomped file. That is a limit of nemo today, not proof the change is
undetectable to a full forensic suite.

Check which filesystem/technique pairs are available before you run one with
[`nemo features`](cli/features.html).
