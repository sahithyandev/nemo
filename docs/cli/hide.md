---
title: hide
parent: CLI Reference
nav_order: 2
---

# `nemo hide`

Writes payload data into a target using one of the three techniques.

```sh
nemo hide <target> --technique <technique> [--image <path>] [options]
```

## Arguments

- `<target>`: the file to hide data against. In live mode, a path on the local filesystem. In image mode, a path inside the disk image.

## Options

- `--technique, -t` (required): `named-stream`, `slack-space`, or `timestomp`. See [Techniques](../techniques/).
- `--image, -i`: path to a raw disk image. With it, `hide` runs in image mode on that image. Without it, `hide` runs on the live filesystem.
- `--data, -d`: file whose contents to hide. Required for `named-stream` and `slack-space`.
- `--stream-name`: name of the stream to write. This is an NTFS ADS name, an xattr name, or an APFS resource-fork stream name. Required for `named-stream`.
- `--field`: which timestamp to change: `created`, `modified`, `accessed`, or `changed`. Required for `timestomp`.
- `--timestamp`: the new value for that field, in RFC 3339 format. Required for `timestomp`.
- `--manifest`: backup manifest path. Default is `nemo-manifest.jsonl`.

### The manifest

For `slack-space`, `hide` first appends the bytes it is about to overwrite to the manifest, a JSON Lines file. A later `clear` uses it to restore them. If the manifest cannot be written, the hide aborts. See [The manifest](../manifest.html) for the format and how `clear` uses it.

## Related pages

[Modes](../modes.html) and [Image and custody](../architecture/image.html) explain `--image` and the custody record.

## Live mode

- Writes work on APFS (macOS) and ext4 (Linux).
- Linux accepts `user.*` xattrs and accessed or modified timestamps on verified ext4 mounts. It refuses live slack.
- Windows NTFS is read-only.
- APFS refuses writes to mounted raw volumes. Use disposable image files for slack demos.

See [Modes](../modes.html).
