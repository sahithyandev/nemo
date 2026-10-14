---
title: CLI Reference
nav_group: "User Guide"
nav_order: 2
has_children: true
---

# CLI reference

| Command                     | Purpose                                              |
| --------------------------- | ---------------------------------------------------- |
| [`features`](features.html) | print which filesystems support which techniques     |
| [`hide`](hide.html)         | write payload data into a target using one technique |
| [`detect`](detect.html)     | scan a target or image for hidden data               |
| [`extract`](extract.html)   | read a hidden payload back out                       |
| [`clear`](clear.html)       | remove hidden data and restore the target            |
| `version`                   | print the tool's version                             |
| `help`                      | print usage information                              |

Every command except `version` and `help` has its own page.

## version

`nemo version` prints the version and exits. It takes no arguments or options. The version string is embedded at compile time from `cmd/VERSION`.

## help

`nemo help` prints general usage. `nemo help <command>` prints that command's description, arguments, and options.

`--help` or `-h` does the same on any command, for example `nemo hide --help`.
