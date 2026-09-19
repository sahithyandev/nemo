---
title: clear
parent: CLI Reference
nav_order: 3
---

# `nemo clear`

Not built yet. This is the intended shape. Tracked as item 9 in the [Roadmap](../roadmap.html).

Removes previously hidden data and restores the target to its original state.

Usage:

```
nemo clear <target> --technique <technique> [--image <path>] [options]
```

Arguments:

- `<target>`: path to the file to clear hidden data from. In live mode, a path on the local filesystem. In image mode, a path inside the given disk image.

Options:

- `--technique, -t`: which technique's hidden data to remove. One of `named-stream`, `slack-space`, `timestomp`. Default: remove all.
- `--image, -i`: path to a raw disk image. If given, `clear` runs in image mode against that image instead of the live filesystem.
- `--stream-name`: name of the stream to remove. Required for `named-stream`.

As with `hide`, every `clear` operation writes an entry to the chain-of-custody log, in both modes.

Restoration limits: clearing a `slack-space` payload restores the original residual bytes only if a manifest from the earlier `hide` is available; without one the frame is zero-filled. See [Slack-Space Hiding](../techniques/slack-space.html#reversibility-the-manifest) for the manifest contract. Clearing a `timestomp` requires the original timestamp to be supplied explicitly, because nemo cannot read a prior value back off the filesystem; see [Timestomping](../techniques/timestomping.html#why-nemos-detect-reports-nothing-for-timestomp). There is no manifest path for timestomp the way there is for slack-space.

Live mode is only built for APFS on macOS today, with the same `slack-space` limit
`hide` has: it always fails against a mounted volume, use `--image` instead. See
[APFS live mode](../file-systems/apfs.html#live-mode-macos).
