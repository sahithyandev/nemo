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
| `completion`                | print a shell completion script                      |
| `version`                   | print the tool's version                             |
| `help`                      | print usage information                              |

Every command except `completion`, `version`, and `help` has its own page.

## version

`nemo version` prints the version and exits. It takes no arguments or options. The version string is embedded at compile time from `cmd/VERSION`.

## help

`nemo help` prints general usage. `nemo help <command>` prints that command's description, arguments, and options.

`--help` or `-h` does the same on any command, for example `nemo hide --help`.

## Shell completion

`nemo completion <shell>` prints a completion script for `bash`, `zsh`, `fish`, or `powershell`. Once loaded, Tab completes commands and flags.

The install script sets this up for you when your shell is zsh. To do it by hand, run this once:

```sh
mkdir -p ~/.nemo && nemo completion zsh > ~/.nemo/_nemo && echo '[ -s ~/.nemo/_nemo ] && source ~/.nemo/_nemo' >> ~/.zshrc
```

Open a new terminal afterwards. The script is saved to a file, so shell startup does not run `nemo`. Run the first half of the command again after upgrading nemo to refresh it.

`nemo` must be on your `PATH` for completion to trigger. The `source` line has to come after `compinit` in `~/.zshrc`, which is the case when you append it to the end.

For other shells, run `nemo completion <shell> --help` for the setup steps.
