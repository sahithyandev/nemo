---
title: features, version, help
parent: CLI Reference
nav_order: 4
---

# `nemo features`, `nemo version`, `nemo help`

The three commands that take no arguments or options.

## `nemo features`

Prints the feature-set matrix: which filesystems (`ntfs`, `apfs`, `ext4`) support which techniques (`named-stream`, `slack-space`, `timestomp`). One row per filesystem/technique pair, with a supported/unsupported indicator.

```
nemo features
```

Useful for checking capabilities before running `hide` against a given image, without consulting the docs. The matrix is built from the same `internal/filesystem` and `internal/technique` registrations used at runtime, so it can't drift from actual behavior. See [Filesystem Interfaces](../architecture/filesystem.html#technique-selection-and-the-features-matrix) for how it's assembled, and [Techniques](../techniques/#support-matrix-current) for the current matrix.

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
