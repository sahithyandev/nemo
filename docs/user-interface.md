# Nemo: User Interface

## Overview

Nemo runs in one of two modes, chosen per command:

- **Image mode** (default when `--image` is given): operates offline against a raw disk image. Filesystem type is detected from the image, not specified by the user.
- **Live mode** (default when `--image` is omitted): operates directly on a file on the local, running machine, using the OS's native filesystem calls. This is the mode an everyday user reaches for to hide a file on their own machine.

Six commands make up the interface: `hide`, `detect`, `clear`, `features`, `version`, and `help`.

## `nemo hide`

Writes payload data into a target, using one of the three supported techniques.

Usage:

```
nemo hide <target> --technique <technique> [--image <path>] [options]
```

Arguments:

- `<target>`: path to the file to hide data against. In live mode, a path on the local filesystem. In image mode, a path inside the given disk image.

Options:

- `--technique, -t` (required): which technique to hide with. One of `named-stream`, `slack-space`, `timestomp`.
- `--image, -i`: path to a raw disk image. If given, `hide` runs in image mode against that image instead of the live filesystem.
- `--data, -d`: path to the file whose contents to hide. Required for `named-stream` and `slack-space`.
- `--stream-name`: name of the stream to write (NTFS ADS name, xattr name, or APFS resource-fork stream name). Required for `named-stream`.
- `--field`: which timestamp to alter (`created`, `modified`, or `accessed`). Required for `timestomp`.
- `--timestamp`: the value to set the chosen timestamp field to, in RFC 3339 format. Required for `timestomp`.
- `--manifest`: path to the backup manifest (default `nemo-manifest.jsonl`). For `slack-space`, `hide` appends the residual bytes it is about to overwrite to this JSON Lines file so a later `clear` can restore them; the hide aborts if the manifest cannot be written.

Every successful `hide` writes an entry to the chain-of-custody log (operation, target, SHA-256 hash, timestamp), in both modes. This logging is automatic and has no corresponding flag to disable.

On success, `hide` emits that custody record as one JSON object on standard output. The record also includes the selected technique, technique-specific detail, and affected byte count. If the output sink fails after the filesystem mutation, the command reports the failure but does not imply that the mutation was rolled back. Durable log location and fail-closed policy remain part of the shared custody contract.

Until a native filesystem implementation or image detector is registered, the corresponding mode fails with a clear unsupported/unrecognized error; it never falls back from one mode to the other.

## `nemo detect`

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

Output: a `TECHNIQUE  TARGET  LOCATION  SIZE` table, one row per finding — the technique, the entry it was found in, the location within that entry (stream name or slack offset range), and the size in bytes of the hidden data recovered. An empty result prints nothing and exits 0.

When `--technique` is given explicitly and no entry in the scan supports it, `detect` exits with an error naming the technique. The default all-three scan silently skips techniques a filesystem does not support (which always includes `timestomp`, since a stomped timestamp cannot be read back). `detect` never writes to the target and never touches the custody log.

## `nemo clear`

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

Restoration limits: clearing a `slack-space` payload restores the original residual bytes only if a manifest from the earlier `hide` is available; without it the frame is zero-filled. Clearing a `timestomp` requires the original timestamp to be supplied explicitly — nemo cannot read a prior timestamp back off the filesystem, so `detect` never reports `timestomp` findings.

## `nemo version`

Prints the tool's version and exits. Takes no arguments or options.

```
nemo version
```

The version string is embedded into the binary at compile time from `cmd/VERSION`.

## `nemo features`

Prints the feature-set matrix: which filesystems (`ntfs`, `apfs`, `ext4`) support which techniques (`named-stream`, `slack-space`, `timestomp`). One row per filesystem/technique pair, with a supported/unsupported indicator. Takes no arguments or options.

```
nemo features
```

Useful for checking capabilities before running `hide` against a given image, without consulting the docs. The matrix is built from the same `internal/filesystem` and `internal/technique` registrations used at runtime, so it can't drift from actual behavior.

## `nemo help`

Prints usage information for the tool or for a specific command: the command's description, arguments, and options.

```
nemo help
nemo help <command>
```

`--help` / `-h` works as an equivalent on any command, e.g. `nemo hide --help`.

## Out of Scope

Any interface beyond the command line (no GUI).
