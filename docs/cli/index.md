---
title: CLI Reference
nav_order: 6
has_children: true
---

# CLI Reference

| Command                     | Purpose                                              |
| --------------------------- | ---------------------------------------------------- |
| [`features`](features.html) | print the filesystem × technique support matrix      |
| [`hide`](hide.html)         | write payload data into a target using one technique |
| [`detect`](detect.html)     | scan a target or image for hidden data               |
| [`extract`](extract.html)   | read a previously hidden payload back out            |
| [`clear`](clear.html)       | remove previously hidden data and restore the target |
| `version`                   | print the tool's version                             |
| `help`                      | print usage information                              |

Commands other than `version` and `help` are explained in their own pages.

## version

`nemo version` prints the tool's version and exits. It takes no arguments or options. The version string is embedded into the binary at compile time from `cmd/VERSION`.

## help

`nemo help` prints usage for the tool, and `nemo help <command>` prints a command's description, arguments, and options. `--help` / `-h` works as an equivalent on any command, e.g. `nemo hide --help`.
