---
title: clear
parent: CLI Reference
nav_order: 5
---

# `nemo clear`

Removes hidden data and restores the target to its original state.

```sh
nemo clear <target> --technique <technique> [--image <path>] [options]
```

## Arguments

- `<target>`: the file to clear. In live mode, a path on the local filesystem. In image mode, a path inside the disk image.

## Options

- `--technique, -t` (required): `named-stream`, `slack-space`, or `timestomp`.
- `--image, -i`: path to a raw disk image. With it, `clear` runs in image mode. Without it, `clear` runs on the live filesystem.
- `--stream-name`: stream to remove. Required for `named-stream`.
- `--field`: timestamp field to restore: `created`, `modified`, `accessed`, or `changed`. Required for `timestomp`.
- `--timestamp`: the original timestamp to restore, in RFC 3339 format. Required for `timestomp`.
- `--manifest`: backup manifest used to restore slack space. Default is `nemo-manifest.jsonl`. An explicit `--manifest` needs a backup bound to the same image and an exact digest of the current frame.

Like `hide`, every `clear` writes an entry to the chain-of-custody log, in both modes.

## Restoration limits

**Slack-space.** `clear` restores the original bytes only if the manifest from the earlier `hide` is available. Without it, the frame is zero-filled. The manifest contract is in [reversibility and the manifest](../techniques/slack-space.html#reversibility-the-manifest).

**Timestomp.** You must supply the original timestamp. Nemo cannot read the old value back off the filesystem, and timestomp has no manifest. See [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp).

### When the manifest does not match

- Records from an older nemo or from a different image are not restored automatically.
- With the default manifest, nemo reports the mismatch and zero-fills.
- With an explicit manifest, the command fails before changing anything.
- A stale frame digest also fails before any change.
- Each new frame carries a per-hide nonce, so hiding the same payload twice does not make an old backup valid.

## Live mode

Live mode works on ext4 (Linux), NTFS (Windows, read-only), and APFS (macOS). A target on any other volume is refused with an error that names the filesystem.

Live `slack-space` clear always fails on a mounted APFS volume. It is unsupported on Linux and NTFS. Use `--image` instead.

See [Modes](../modes.html).
