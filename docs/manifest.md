---
title: The manifest
nav_group: "User Guide"
nav_order: 5
---

# The manifest

The manifest is a file nemo writes during `hide` so that `clear` can later put the original bytes back. It only matters for the `slack-space` technique.

Slack space holds whatever bytes were left over from earlier files. When `hide` writes a payload there, it overwrites them. The manifest keeps a copy of what was overwritten. Without it, `clear` can remove the payload but cannot recover the old bytes, so it fills the region with zeros.

`named-stream` and `timestomp` do not use it. A named stream is a separate object that `clear` deletes. A timestomp needs you to supply the original timestamp when you clear.

## Where it lives

The default path is `nemo-manifest.jsonl` in the directory where you run nemo. Both commands take `--manifest <path>` to use another file:

```sh
nemo hide hello.txt -t slack-space -i disk.img --manifest ~/cases/disk.manifest
nemo clear hello.txt -t slack-space -i disk.img --manifest ~/cases/disk.manifest
```

Use the same path for both. If you run `clear` from a different directory without `--manifest`, nemo looks for a manifest there, finds none, and zero-fills.

`hide` appends to the file and never rewrites it, so one manifest can cover many hides and many images.

## What a record contains

The file is JSON Lines: one record per line, one line per `hide`. It is plain text, so you can read it with `less` or search it with `grep`.

| Field           | Meaning                                                                                  |
| --------------- | ---------------------------------------------------------------------------------------- |
| `Technique`     | always `slack-space` today                                                               |
| `Target`        | the file the payload was hidden in                                                       |
| `Location`      | the byte range of the slack region                                                       |
| `Original`      | the overwritten bytes, base64-encoded                                                    |
| `ImageIdentity` | which image the record belongs to (device and inode, size, and the ext4 UUID when known) |
| `FrameSHA256`   | SHA-256 of the exact frame `hide` wrote                                                  |
| `Timestamp`     | unused for slack space, so it is the zero time                                           |

`Original` is a copy of real disk bytes, which can include fragments of deleted files. Treat the manifest with the same care as the image.

## How clear uses it

`clear` finds the newest record for the same target and location that belongs to this image. It then reads the frame currently on disk and compares its SHA-256 with `FrameSHA256`. If both checks pass, it writes `Original` back over the frame.

The checks exist so an old backup can never overwrite data it does not belong to. That would be silent corruption of evidence.

If you hide the same target twice, `clear` restores the bytes from the second hide. Each hide writes a fresh frame, so the first record no longer matches the digest.

## When the manifest does not match

What happens depends on whether you passed `--manifest` yourself.

| Situation                                                                                               | Default manifest                | Explicit `--manifest`                       |
| ------------------------------------------------------------------------------------------------------- | ------------------------------- | ------------------------------------------- |
| No manifest file                                                                                        | frame is zero-filled            | `clear` fails, nothing is changed           |
| Records exist, but none match this image and target                                                     | nemo reports it and zero-fills  | `clear` fails, nothing is changed           |
| Record from an older nemo (no identity or digest)                                                       | nemo reports it and zero-fills  | `clear` fails, nothing is changed           |
| Matching record, but the frame on disk has changed since                                                | `clear` fails, nothing changed  | `clear` fails, nothing is changed           |

Passing `--manifest` tells nemo you expect a restore, so it refuses to fall back to zeros. Leaving it out tells nemo a zero-filled region is acceptable.

A record stops matching when the image is copied, resized, or replaced, because its identity changes. Moving the image within the same filesystem or opening it through a symlink does not break the match. A copied image has to be cleared by hand: find the record in the manifest, base64-decode `Original`, and write it at `Location`.

## Keeping it safe

- Keep the manifest next to the image it describes, and copy them together.
- Do not edit it by hand. A malformed line in the middle of the file makes `clear` fail. A single bad last line is ignored, since it usually means a `hide` was interrupted.
- If the manifest cannot be written, for example because the disk is full or the path is read-only, `hide` stops before it changes the image.
- Deleting the manifest does not break the hidden data. `detect` and `extract` read the frame straight from the image. Only the restore is lost.

## Related pages

- [`hide`](cli/hide.html) and [`clear`](cli/clear.html) for the flags
- [Slack-space hiding](techniques/slack-space.html#reversibility-the-manifest) for the frame layout and the code that reads and writes the manifest
- [Technique interfaces](architecture/technique.html) for the record type and the backup contract
