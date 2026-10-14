---
title: Validation
nav_group: "Contributing"
nav_order: 4
has_children: true
---

# Validation

Nemo ships a validation harness (`tools/validate` over `internal/validation`). It scores `detect` against a manifest of known cases.

- It never downloads images.
- It opens every image read-only.
- It compares findings as exact multisets of location and byte size. An unexpected, missing, duplicate, or wrong-sized finding fails the case.

Run it from the repository root:

```sh
go run ./tools/validate -manifest docs/validation/synthetic-ext4-v1.json /path/to/dataset
```

The [harness reference]({{ site.src }}/docs/validation/README.md) has the full manifest format, the status codes (`pass`, `fail`, `missing`, `unsupported`, `error`), and the exit-status rules.

## Reproducible synthetic sample

The [`create-sample.sh`]({{ site.src }}/docs/validation/create-sample.sh) script builds a real ext4 image. It holds a positive `user.nemo` xattr and a clean file. It needs no mount and no root privileges. The positive, clean-target, and clean-slack cases run anywhere e2fsprogs is available.

```sh
sh docs/validation/create-sample.sh /tmp/nemo-validation-demo
go run ./tools/validate -manifest docs/validation/synthetic-ext4-v1.json /tmp/nemo-validation-demo
```

The [recorded output]({{ site.src }}/docs/validation/synthetic-result-v1.json) is 3 pass and nothing else. This is the successful harness demonstration. The image SHA-256 is the same before and after scanning.

## Public corpus benchmark

The NTFS ADS subset of [fkie-cad/hide-and-seek-dataset](https://github.com/fkie-cad/hide-and-seek-dataset) is pinned as a reproducible public benchmark. Its status is **0 pass, 3 fail**.

Independent ntfs-3g tools show that the advertised streams are separate POSIX files with literal colons in their names. They are not NTFS named DATA attributes. So these are recorded as corpus discrepancies. They are not turned into passes, and they are not blamed on the parser.

APFS and ext4 public-corpus coverage is not measured yet.

See [Public NTFS ADS benchmark](public-ntfs-ads.html) for the full evidence and reproduction steps.
