# Local dataset validation

Run the harness from the repository root with an explicit local dataset directory:

```sh
go run ./tools/validate -manifest docs/validation/synthetic-ext4-v1.json /path/to/dataset
```

The harness never downloads images and opens each image read-only. Paths are relative to the dataset root; traversal and symlinks escaping that root are rejected. Each case names one file inside a raw filesystem image, not a directory or whole-disk partition container. The filesystem detected in the image must match the manifest.

The version 1 JSON format requires `provenance` and a nonempty `cases` array. Each case has a unique `id`, relative `image`, `filesystem` (`ext4`, `ntfs`, or `apfs`), absolute image `target`, `technique`, and explicit `expected` array of `{ "location": "stream name or slack range", "size": 11 }`. An empty array expects no findings. Unknown JSON fields, duplicate case IDs, invalid paths and invalid techniques are rejected. See [the synthetic manifest](synthetic-ext4-v1.json) for a complete example. Record the dataset release or commit and independently verified labels in provenance when adding public cases.

Findings are compared as exact multisets of location and byte size: unexpected findings, missing findings, duplicate counts and incorrect sizes fail. Cases execute in manifest order; actual findings and JSON group keys are sorted. Status totals are reported overall and by `filesystem/technique`:

- `pass`: exact expected findings.
- `fail`: successful detection disagrees with expected findings.
- `missing`: image file is absent.
- `unsupported`: the detector returns the unsupported sentinel. Timestomp detection is explicitly unsupported because no timestamp anomaly detector exists; an empty result cannot establish a pass.
- `error`: malformed image, wrong filesystem, missing target inside an existing image, inaccessible path, or other operational failure.

Exit status is 0 only when every case passes, 1 for any incomplete or failing case, and 2 for command, manifest, root-directory or output errors. With `go run`, Go may wrap the nonzero exit status; build `go build -o /tmp/nemo-validate ./tools/validate` for direct exit codes. Missing and unsupported cases never silently improve the score. Detection measures reported stream names/sizes and Nemo-framed slack; it does not prove payload identity or detect arbitrary unframed hidden bytes.

## Reproducible sample

Prerequisites: Go from `go.mod`, a POSIX shell, `truncate`, and e2fsprogs (`mkfs.ext4`, `debugfs`). No mount or root privileges are needed. The script refuses an existing output directory and only mutates its newly created disposable image.

```sh
sh docs/validation/create-sample.sh /tmp/nemo-validation-demo
sha256sum /tmp/nemo-validation-demo/sample.ext4
go run ./tools/validate -manifest docs/validation/synthetic-ext4-v1.json /tmp/nemo-validation-demo
sha256sum /tmp/nemo-validation-demo/sample.ext4
go test ./internal/validation ./tools/validate
go vet ./internal/validation ./tools/validate
```

The script creates a real ext4 image containing a positive `user.nemo` xattr and a clean file. Its three cases cover positive named-stream detection, a clean named-stream target, and clean slack. Fresh filesystem UUIDs and creation times vary, but expected detections remain identical. The integration test generates this fixture, verifies positive and negative scoring, unsupported timestomp, missing targets, filesystem mismatch, and an unchanged SHA-256 after all scans. It skips explicitly if fixture tools are unavailable.

[Recorded sample output](synthetic-result-v1.json): 3 pass, 0 fail/missing/unsupported/error. Executed on Linux with e2fsprogs 1.47.4. The particular run's before/after image SHA-256 both equal `05a7acc6e1053ef959645498a0a84264c93b61d6ec66d5da37319a4f1774954b`. This hash identifies that disposable run, not every generated fixture.

## Public corpus benchmark

The primary [fkie-cad/hide-and-seek-dataset repository](https://github.com/fkie-cad/hide-and-seek-dataset) is accessible as of 2026-10-12. The assignment's shorter `fkie-cad/hide-and-seek` URL is not the verified source. The pinned NTFS ADS subset has now been downloaded explicitly and evaluated against independently specified upstream expectations.

The recorded result is **0 pass, 3 fail**: independent ntfs-3g tools show that the advertised streams are separate POSIX files with literal colon characters in their names, rather than NTFS named DATA attributes. The metadata also disagrees with the first payload's observed contents. These are reported as corpus discrepancies, not converted to successful detection or attributed to a Nemo parser bug.

See [public benchmark evidence and reproduction](public-ntfs-ads.md), [versioned expectations](public-ntfs-ads-v1.json), and [recorded output](public-ntfs-ads-result-v1.json). This establishes a reproducible public benchmark, not successful detection of positive ADS cases. APFS/ext4 public corpus coverage remains unmeasured. The synthetic sample above remains the successful harness demonstration.
