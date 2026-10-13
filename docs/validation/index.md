---
title: Validation
nav_group: "Contributing"
nav_order: 4
has_children: true
---

# Validation

nemo ships a validation harness (`tools/validate` over `internal/validation`)
that scores `detect` against a manifest of known cases. It never downloads
images, opens every image read-only, and compares findings as exact multisets of
location and byte size, so an unexpected, missing, duplicate, or wrong-sized
finding fails the case. Run it from the repository root:

```sh
go run ./tools/validate -manifest docs/validation/synthetic-ext4-v1.json /path/to/dataset
```

The full manifest format, status codes (`pass`, `fail`, `missing`,
`unsupported`, `error`), and exit-status rules are in the
[harness reference]({{ site.src }}/docs/validation/README.md).

## Reproducible synthetic sample

The [`create-sample.sh`]({{ site.src }}/docs/validation/create-sample.sh)
script builds a real ext4 image (a positive `user.nemo` xattr plus a clean file)
with no mount or root privileges, so the positive, clean-target, and clean-slack
cases run anywhere e2fsprogs is available:

```sh
sh docs/validation/create-sample.sh /tmp/nemo-validation-demo
go run ./tools/validate -manifest docs/validation/synthetic-ext4-v1.json /tmp/nemo-validation-demo
```

[Recorded output]({{ site.src }}/docs/validation/synthetic-result-v1.json): 3
pass, nothing else. This is the successful harness demonstration, and the image
SHA-256 is unchanged before and after scanning.

## Public corpus benchmark

The [fkie-cad/hide-and-seek-dataset](https://github.com/fkie-cad/hide-and-seek-dataset)
NTFS ADS subset is pinned as a reproducible public benchmark. Its current status
is **0 pass, 3 fail**: independent ntfs-3g tooling shows the advertised streams
are separate POSIX files with literal colons in their names, not NTFS named DATA
attributes, so these are recorded as corpus discrepancies rather than converted
into passes or blamed on the parser. APFS and ext4 public-corpus coverage is
still unmeasured.

See [Public NTFS ADS benchmark](public-ntfs-ads.html) for the full evidence and
reproduction steps.
