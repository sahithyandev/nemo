# Local dataset validation

Run the harness from the repository root with an explicit local dataset directory:

```sh
go run ./tools/validate -manifest docs/validation/synthetic-ext4-v1.json /path/to/dataset
```

## How it behaves

- It never downloads images and opens each image read-only.
- Paths are relative to the dataset root. Traversal and symlinks that escape the root are rejected.
- Each case names one file inside a raw filesystem image. It cannot name a directory or a whole-disk partition container.
- The filesystem detected in the image must match the manifest.

## Manifest format (version 1)

The JSON needs `provenance` and a non-empty `cases` array. Each case has these fields:

| Field | Meaning |
| --- | --- |
| `id` | Unique case ID |
| `image` | Image path, relative to the dataset root |
| `filesystem` | `ext4`, `ntfs`, or `apfs` |
| `target` | Absolute path of the file inside the image |
| `technique` | The technique to detect |
| `expected` | Array of `{ "location": "stream name or slack range", "size": 11 }`. An empty array expects no findings |

These are rejected: unknown JSON fields, duplicate case IDs, invalid paths, and invalid techniques.

See [the synthetic manifest](synthetic-ext4-v1.json) for a full example. When you add public cases, record the dataset release or commit in `provenance`, along with independently verified labels.

## Scoring

Findings are compared as exact multisets of location and byte size. These fail a case:

- unexpected findings
- missing findings
- duplicate counts
- incorrect sizes

Cases run in manifest order. Actual findings and JSON group keys are sorted. Status totals are reported overall and by `filesystem/technique`.

| Status | Meaning |
| --- | --- |
| `pass` | Exact expected findings |
| `fail` | Detection succeeded but disagrees with the expected findings |
| `missing` | The image file is absent |
| `unsupported` | The detector returned the unsupported sentinel. Timestomp detection is always unsupported, because no timestamp anomaly detector exists and an empty result cannot establish a pass |
| `error` | Malformed image, wrong filesystem, missing target inside an existing image, inaccessible path, or another operational failure |

### Exit status

- `0`: every case passes.
- `1`: any case is incomplete or failing.
- `2`: a command, manifest, root-directory, or output error.

With `go run`, Go may wrap the non-zero exit status. For direct exit codes, build the binary first: `go build -o /tmp/nemo-validate ./tools/validate`.

Missing and unsupported cases never quietly improve the score. Detection measures reported stream names and sizes, and nemo-framed slack. It does not prove payload identity, and it does not detect arbitrary unframed hidden bytes.

## Reproducible sample

You need Go (the version in `go.mod`), a POSIX shell, `truncate`, and e2fsprogs (`mkfs.ext4`, `debugfs`). It needs no mount and no root privileges. The script refuses an existing output directory and only changes the disposable image it creates.

```sh
sh docs/validation/create-sample.sh /tmp/nemo-validation-demo
sha256sum /tmp/nemo-validation-demo/sample.ext4
go run ./tools/validate -manifest docs/validation/synthetic-ext4-v1.json /tmp/nemo-validation-demo
sha256sum /tmp/nemo-validation-demo/sample.ext4
go test ./internal/validation ./tools/validate
go vet ./internal/validation ./tools/validate
```

The script creates a real ext4 image with a positive `user.nemo` xattr and a clean file. Its three cases are:

- positive named-stream detection
- a clean named-stream target
- clean slack

Fresh filesystem UUIDs and creation times vary between runs, but the expected detections stay the same.

The integration test generates this fixture and checks:

- positive and negative scoring
- unsupported timestomp
- missing targets
- filesystem mismatch
- an unchanged SHA-256 after all scans

It skips explicitly if the fixture tools are unavailable.

The [recorded sample output](synthetic-result-v1.json) is 3 pass, with 0 fail, missing, unsupported, or error. It ran on Linux with e2fsprogs 1.47.4. That run's image SHA-256 was `05a7acc6e1053ef959645498a0a84264c93b61d6ec66d5da37319a4f1774954b` both before and after. The hash identifies that disposable run, not every generated fixture.

## Public corpus benchmark

The primary [fkie-cad/hide-and-seek-dataset repository](https://github.com/fkie-cad/hide-and-seek-dataset) was accessible as of 2026-10-12. The shorter `fkie-cad/hide-and-seek` URL from the assignment is not the verified source.

The pinned NTFS ADS subset was downloaded explicitly and checked against independently specified upstream expectations.

The recorded result is **0 pass, 3 fail**.

- Independent ntfs-3g tools show that the advertised streams are separate POSIX files with literal colons in their names. They are not NTFS named DATA attributes.
- The metadata also disagrees with the first payload's observed contents.
- These are reported as corpus discrepancies. They are not turned into successful detection, and they are not blamed on a nemo parser bug.

Files:

- [Benchmark evidence and reproduction](public-ntfs-ads.md)
- [Versioned expectations](public-ntfs-ads-v1.json)
- [Recorded output](public-ntfs-ads-result-v1.json)

This gives a reproducible public benchmark. It does not show successful detection of positive ADS cases. APFS and ext4 public corpus coverage is not measured. The synthetic sample above remains the successful harness demonstration.
