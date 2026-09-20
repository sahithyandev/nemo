---
title: hide
parent: CLI Reference
nav_order: 1
---

# `nemo hide`

Writes payload data into a target, using one of the three supported techniques.

Usage:

```
nemo hide <target> --technique <technique> [--image <path>] [options]
```

Arguments:

- `<target>`: path to the file to hide data against. In live mode, a path on the local filesystem. In image mode, a path inside the given disk image.

Options:

- `--technique, -t` (required): which technique to hide with. One of `named-stream`, `slack-space`, `timestomp`. See [Techniques](../techniques/).
- `--image, -i`: path to a raw disk image. If given, `hide` runs in image mode against that image instead of the live filesystem.
- `--data, -d`: path to the file whose contents to hide. Required for `named-stream` and `slack-space`.
- `--stream-name`: name of the stream to write (NTFS ADS name, xattr name, or APFS resource-fork stream name). Required for `named-stream`.
- `--field`: which timestamp to alter (`created`, `modified`, `accessed`, or `changed`). Required for `timestomp`.
- `--timestamp`: the value to set the chosen timestamp field to, in RFC 3339 format. Required for `timestomp`.
- `--manifest`: path to the backup manifest (default `nemo-manifest.jsonl`). For `slack-space`, `hide` appends the residual bytes it is about to overwrite to this JSON Lines file so a later `clear` can restore them; the hide aborts if the manifest cannot be written. See [Technique Interfaces](../architecture/technique.html) for the full backup contract.

See [Modes](./#modes) and [Custody logging](./#custody-logging) for how `--image` and the custody record work.

Live mode is only built for APFS on macOS today, and nothing else, even there: a
target on a mounted NTFS, ext4, or other non-APFS volume refuses with a clear error
naming that filesystem rather than falling through to something wrong. `named-stream`
and `timestomp` work against any APFS path; `slack-space` always fails against a
mounted volume, since macOS refuses a read-write open of the raw device while its
volume is mounted, use `--image` for a live slack-space hide instead. See [APFS live
mode](../file-systems/apfs.html#live-mode-macos).
