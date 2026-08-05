# nemo

a CLI tool for hiding files.

## Development

### Prerequisites

- Go 1.26+
- GNU Make

### Dependencies

| Name | Description |
| --- | --- |
| [spf13/cobra](https://github.com/spf13/cobra) | CLI framework (commands, flags, help/usage) |
| spf13/pflag | transitive dep of Cobra, POSIX-style flags |
| inconshreveable/mousetrap | transitive dep of Cobra, Windows double-click detection |

### Commands

```
make build   # compile ./bin/nemo
make run     # go run .
make test    # run tests
make vet     # static checks
make fmt     # gofmt all files
```

Version is read from `cmd/VERSION` and embedded into the binary at compile time (`nemo version`).

