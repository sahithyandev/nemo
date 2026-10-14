---
title: extract
parent: CLI Reference
nav_order: 4
---

# `nemo extract`

Reads a hidden payload out of a target and writes it to a file or standard output. `extract` is read-only. It never writes to the target and never touches the custody log.

```sh
nemo extract <target> --technique <technique> [--image <path>] [options]
```

## Arguments

- `<target>`: the file to extract from. In live mode, a path on the local filesystem. In image mode, a path inside the disk image.

## Options

- `--technique, -t` (required): `named-stream` or `slack-space`. `timestomp` is rejected, because it changes a timestamp in place and stores no payload.
- `--image, -i`: path to a raw disk image. With it, `extract` runs in image mode, and the image is opened read-only.
- `--stream-name`: stream to read. Required for `named-stream`, rejected for `slack-space`.
- `--output, -o`: file to write the payload to. Default is standard output.

## Slack-space payloads

There is no `--manifest` flag. Nemo wraps every slack payload in a 12-byte, CRC-checked frame when it hides it. `extract` reads that frame straight off the disk, so it does not need the manifest that `clear` uses. The layout is in [nemo's payload frame](../techniques/slack-space.html#nemos-payload-frame).

If the slack space holds no frame, `extract` fails. It never hands back leftover noise as a payload.

### Several frames on one target

`extract` returns only the first valid frame it finds. A target can hold several if an earlier hide was never cleared. `detect` lists all of them, so the two commands can disagree about which one is the payload.

A future `--index` flag could let you pick one. See [Technique interfaces](../architecture/technique.html#first-frame-limitation).

## Live mode

Live mode works on ext4 (Linux), NTFS (Windows, read-only), and APFS (macOS). Each platform only reaches its native filesystem. A target on any other volume is refused with an error that names the filesystem.

See [Modes](../modes.html) and [APFS live mode](../file-systems/apfs.html#live-mode-macos).
