---
title: detect
parent: CLI Reference
nav_order: 2
---

# `nemo detect`

Scans a target, or an entire image, for hidden data and reports what it finds. `detect` is read-only: it never writes to the target.

Usage:

```
nemo detect [target] [--technique <technique>] [--image <path>]
```

Arguments:

- `[target]`: path to a specific file or directory to check; a directory is scanned recursively. In live mode, a path on the local filesystem. In image mode, a path inside the given disk image; if omitted, every file in the image is scanned. Omitting the target is only valid in image mode.

Options:

- `--technique, -t`: restrict the scan to a single technique (`named-stream`, `slack-space`, or `timestomp`). Default: scan for all three.
- `--image, -i`: path to a raw disk image. If given, `detect` runs in image mode against that image instead of the live filesystem. The image is opened read-only.

Output: a `TECHNIQUE  TARGET  LOCATION  SIZE` table, one row per finding. The columns are the technique, the entry it was found in, the location within that entry (stream name or slack offset range), and the size in bytes of the hidden data recovered. An empty result prints nothing and exits 0.

When `--technique` is given explicitly and no entry in the scan supports it, `detect` exits with an error naming the technique. The default all-three scan silently skips techniques a filesystem does not support. `timestomp` never yields findings, regardless of filesystem, because nemo cannot read a timestamp back to judge whether it was altered; see [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp) for why. `detect` never touches the custody log.

Live mode is only built for APFS on macOS today. An unqualified live scan (no
`--technique`) never attempts a live `slack-space` detect, the same way it skips any
technique a filesystem doesn't support; `--technique slack-space` explicitly does,
opening the volume's raw device read-only (root only if that device node isn't
already readable by the current user). See [APFS live
mode](../file-systems/apfs.html#live-mode-macos).
