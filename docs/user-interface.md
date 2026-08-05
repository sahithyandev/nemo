# Nemo: User Interface

## Overview

Nemo runs in one of two modes, chosen per command:

- **Image mode** (default when `--image` is given): operates offline against a raw disk image. Filesystem type is detected from the image, not specified by the user.
- **Live mode** (default when `--image` is omitted): operates directly on a file on the local, running machine, using the OS's native filesystem calls. This is the mode an everyday user reaches for to hide a file on their own machine.

Five commands make up the interface: `hide`, `detect`, `clear`, `version`, and `help`.

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

Every successful `hide` writes an entry to the chain-of-custody log (operation, target, SHA-256 hash, timestamp), in both modes. This logging is automatic and has no corresponding flag to disable.

## `nemo detect`

Scans a target, or an entire image, for hidden data and reports what it finds. `detect` is read-only: it never writes to the target.

Usage:

```
nemo detect <target> --technique <technique> [--image <path>]
```

Arguments:

- `[target]`: path to a specific file or directory to check. In live mode, a path on the local filesystem. In image mode, a path inside the given disk image; if omitted, every file in the image is scanned.

Options:

- `--technique, -t`: restrict the scan to a single technique (`named-stream`, `slack-space`, or `timestomp`). Default: scan for all three.
- `--image, -i`: path to a raw disk image. If given, `detect` runs in image mode against that image instead of the live filesystem.

Output: one line per finding, giving the technique, the target's location, and the size of the hidden data recovered. An empty result means nothing was found for the given target and technique.

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

## `nemo version`

Prints the tool's version and exits. Takes no arguments or options.

```
nemo version
```

The version string is embedded into the binary at compile time from `cmd/VERSION`.

## `nemo help`

Prints usage information for the tool or for a specific command: the command's description, arguments, and options.

```
nemo help
nemo help <command>
```

`--help` / `-h` works as an equivalent on any command, e.g. `nemo hide --help`.

## Out of Scope

Any interface beyond the command line (no GUI).
