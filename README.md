# nemo

a CLI tool for hiding files.

## Docs

- [docs/overall-plan.md](docs/overall-plan.md) - goals, scope, targeted filesystems
- [docs/user-interface.md](docs/user-interface.md) - modes and commands
- [docs/architecture.md](docs/architecture.md) - package layout, interfaces
- [docs/work-breakdown.md](docs/work-breakdown.md)

## Development

### Prerequisites

- Go 1.26+
- GNU Make

### Setup

After cloning the repo, run `make hooks` which will setup the git-managed hooks for the project.

Currently there is a pre-commit hook that formats staged `.go` files with `gofmt` and re-stages for the commit.

### Dependencies

| Name                                          | Description                                             |
| --------------------------------------------- | ------------------------------------------------------- |
| [spf13/cobra](https://github.com/spf13/cobra) | CLI framework (commands, flags, help/usage)             |
| spf13/pflag                                   | transitive dep of Cobra, POSIX-style flags              |
| inconshreveable/mousetrap                     | transitive dep of Cobra, Windows double-click detection |

### Commands

```
make build   # compile ./bin/nemo
make run     # go run .
make test    # run tests
make vet     # static checks
make fmt     # gofmt all files
```

Version is read from `cmd/VERSION` and embedded into the binary at compile time (`nemo version`).

## Authors

- Bandara S.A.N.K 
- Sahithyan K.
- Senanayake H.P.V.R
