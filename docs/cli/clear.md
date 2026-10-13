---
title: clear
parent: CLI Reference
nav_order: 5
---

# `nemo clear`

Removes previously hidden data and restores the target to its original state.

Usage:

```sh
nemo clear <target> --technique <technique> [--image <path>] [options]
```

Arguments:

- `<target>`: path to the file to clear hidden data from. In live mode, a path on the local filesystem. In image mode, a path inside the given disk image.

Options:

- `--technique, -t` (required): which technique's hidden data to remove. One of `named-stream`, `slack-space`, `timestomp`.
- `--image, -i`: path to a raw disk image. If given, `clear` runs in image mode against that image instead of the live filesystem.
- `--stream-name`: name of the stream to remove. Required for `named-stream`.
- `--field`: which timestamp field to restore (`created`, `modified`, `accessed`, or `changed`). Required for `timestomp`.
- `--timestamp`: the original RFC 3339 timestamp value to restore. Required for `timestomp`.
- `--manifest`: backup manifest for slack restoration (default `nemo-manifest.jsonl`); an explicit `--manifest` requires a matching image-bound backup and exact current-frame digest.

As with `hide`, every `clear` operation writes an entry to the chain-of-custody log, in both modes.

Restoration limits: clearing a `slack-space` payload restores the original residual bytes only if a manifest from the earlier `hide` is available; without one the frame is zero-filled. See [Slack-Space Hiding](../techniques/slack-space.html#reversibility-the-manifest) for the manifest contract. Clearing a `timestomp` requires the original timestamp to be supplied explicitly, because nemo cannot read a prior value back off the filesystem; see [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp). There is no manifest path for timestomp the way there is for slack-space.

Legacy or foreign-image records are not restored automatically. The default
manifest reports the mismatch and zero-fills; an explicit manifest fails before
mutation. A stale frame digest also fails before mutation. New frames carry a
per-hide nonce, so repeating the same payload does not qualify an old backup.

Live mode runs on ext4 (Linux), NTFS (Windows, read-only), and APFS (macOS). A
target on a non-native or unsupported volume refuses with a clear error naming
that filesystem rather than falling through to something wrong. `slack-space`
live clear always fails against a mounted APFS volume and is unsupported on
Linux and NTFS entirely; use `--image` instead. See [Live
Mode](../architecture/live-mode.html).
