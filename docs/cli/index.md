---
title: CLI Reference
nav_order: 3
has_children: true
---

# CLI Reference

Built today: `hide`, `detect`, `features`, `version`, `help`, all image mode only. `clear` and live mode are planned; where a page describes them, it is design intent.

## Modes

Nemo runs in one of two modes, chosen per command:

- **Image mode** (used when `--image` is given): operates offline against a raw disk image. Filesystem type is detected from the image, not specified by the user.
- **Live mode** (planned; used when `--image` is omitted): operates directly on a file on the local, running machine, using the OS's native filesystem calls. This is the mode an everyday user reaches for to hide a file on their own machine.

Until a native filesystem implementation or image detector is registered, the corresponding mode fails with a clear unsupported/unrecognized error; nemo never falls back from one mode to the other. See [Live Mode](../architecture/live-mode.html) for how the two modes are built internally.

## Custody logging

Every successful `hide` and `clear` writes an entry to the chain-of-custody log automatically, in both modes, with no flag to disable it. See [Image and Custody](../architecture/image.html) for why this can't be opted out of. `detect` is read-only: it never writes to the target and never touches the custody log.

On success, `hide` and `clear` emit their custody record as one JSON object on standard output, including the selected technique, technique-specific detail, and affected byte count. If the output sink fails after the filesystem mutation, the command reports the failure but does not imply that the mutation was rolled back.

## The manifest

`--manifest` (default `nemo-manifest.jsonl`) names a JSON Lines file that `hide` appends to before overwriting slack-space bytes, so a later `clear` can restore them. See [Technique Interfaces](../architecture/technique.html) for the backup and restore contract, and [Slack-Space Hiding](../techniques/slack-space.html) for what gets recorded.

## Commands

| Command | Purpose | Status |
| --- | --- | --- |
| [`hide`](hide.html) | write payload data into a target using one technique | built |
| [`detect`](detect.html) | scan a target or image for hidden data | built |
| [`clear`](clear.html) | remove previously hidden data and restore the target | planned |
| [`features`](info.html#nemo-features) | print the filesystem × technique support matrix | built |
| [`version`](info.html#nemo-version) | print the tool's version | built |
| [`help`](info.html#nemo-help) | print usage information | built |

## Out of Scope

Any interface beyond the command line (no GUI).
