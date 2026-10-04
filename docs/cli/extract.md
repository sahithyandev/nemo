---
title: extract
parent: CLI Reference
nav_order: 4
---

# `nemo extract`

Reads a previously hidden payload back out of a target and writes it to a file or standard output. `extract` is read-only: it never writes to the target and never touches the custody log.

Usage:

```
nemo extract <target> --technique <technique> [--image <path>] [options]
```

Arguments:

- `<target>`: path to the file to extract hidden data from. In live mode, a path on the local filesystem. In image mode, a path inside the given disk image.

Options:

- `--technique, -t`: which technique's hidden data to read. One of `named-stream` or `slack-space` (required). `timestomp` is rejected: it overwrites a timestamp field in place and stores no retrievable payload.
- `--image, -i`: path to a raw disk image. If given, `extract` runs in image mode against that image, opened read-only, instead of the live filesystem.
- `--stream-name`: name of the stream to read. Required for `named-stream`, rejected for `slack-space`.
- `--output, -o`: file to write the recovered payload to. Default: standard output.

There is no `--manifest` flag. A slack-space payload is self-describing: nemo wraps every slack payload in a 12-byte, CRC-checked frame when hiding it, so `extract` finds and reads that frame directly off disk without needing the manifest `clear` uses for restoration. See [Slack-Space Hiding](../techniques/slack-space.html#nemos-payload-frame) for the frame layout. If a target's slack space holds no such frame, `extract` errors rather than returning residual noise as if it were a payload.

Live mode is only built for APFS on macOS today, and nothing else, even there: a
target on a mounted NTFS, ext4, or other non-APFS volume refuses with a clear error
naming that filesystem rather than falling through to something wrong. See [APFS live
mode](../file-systems/apfs.html#live-mode-macos).
