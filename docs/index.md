---
title: Home
nav_order: 1
---

# nemo

Nemo is a cross-platform CLI tool, written in Go, for hiding, detecting, and clearing hidden data on NTFS, APFS, and ext4. It's a forensic utility, an offensive/CTF tool for anti-forensic technique practice, and, through live mode, something you can point at your own machine.

`hide` and `clear` mutate the target. Practice against disposable image copies or
disposable files, not production data or evidence: a bug or a missing backup
manifest can destroy data with no way back. `detect` and `extract` are read-only.
Live-mode slack-space access needs admin/root on the platforms that support it at
all; see [Live Mode](architecture/live-mode.html) for exactly what each OS allows.

## A quick example

Hide the contents of `secret.bin` inside a named stream on `report.txt`, in a disk image:

```
nemo hide report.txt --technique named-stream --data secret.bin --stream-name notes --image disk.raw
```

Find it again:

```
nemo detect report.txt --image disk.raw
TECHNIQUE      TARGET       LOCATION   SIZE
named-stream   report.txt   notes      1024
```

See [CLI Reference](cli/) for every command and flag.

## Three techniques

- **Named-stream hiding** attaches extra, named data to a file without changing its visible size or hash: NTFS Alternate Data Streams, ext4 and APFS extended attributes, APFS resource forks.
- **Slack-space hiding** writes into the unused tail bytes a filesystem allocates but a file doesn't use.
- **Timestomping** rewrites a file's timestamps to defeat timeline analysis.

[Techniques](techniques/) covers what each one does on disk, how much data it holds, and how it gets detected, along with the current filesystem support matrix.

## Where to go next

- Use the tool: [CLI Reference](cli/).
- Understand the techniques: [Techniques](techniques/).
- Contribute a filesystem or technique: [Architecture](architecture/) and [File Systems](file-systems/).
