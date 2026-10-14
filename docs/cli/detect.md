---
title: detect
parent: CLI Reference
nav_order: 3
---

# `nemo detect`

Scans a target, or a whole image, for hidden data and reports what it finds. `detect` is read-only. It never writes to the target.

```sh
nemo detect [target] [--technique <technique>] [--image <path>]
```

## Arguments

- `[target]`: a file or directory to check. A directory is scanned recursively. In live mode, it is a path on the local filesystem. In image mode, it is a path inside the image. If you omit it, nemo scans every file in the image. Omitting the target only works in image mode.

## Options

- `--technique, -t`: scan for one technique only: `named-stream`, `slack-space`, or `timestomp`. Default is all three.
- `--image, -i`: path to a raw disk image. With it, `detect` runs in image mode, and the image is opened read-only.

## Output

One row per finding:

```text
TECHNIQUE  TARGET  LOCATION  SIZE
```

- `TECHNIQUE`: the technique that matched.
- `TARGET`: the entry it was found in.
- `LOCATION`: the stream name or slack offset range.
- `SIZE`: bytes of hidden data recovered.

No findings means no output and exit code 0.

## Edge cases

- If you pass `--technique` and no entry in the scan supports it, `detect` exits with an error naming the technique. The default scan silently skips techniques a filesystem lacks.
- `timestomp` never yields findings. Nemo cannot read a timestamp back to judge whether it was altered. See [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp).
- `detect` never touches the custody log.

## Live mode

Live mode works on ext4 (Linux), NTFS (Windows, read-only), and APFS (macOS). Each platform only reaches its native filesystem. A target on any other volume is refused with an error that names the filesystem.

Live slack-space detection:

- A scan without `--technique` never tries it, just like any technique a filesystem lacks.
- On macOS, `--technique slack-space` runs it. Nemo opens the volume's raw device read-only. You need root only if the current user cannot already read that device node.
- On Linux and NTFS it is unsupported.

See [Modes](../modes.html) and [APFS live mode](../file-systems/apfs.html#live-mode-macos).
