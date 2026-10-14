---
title: Public NTFS ADS benchmark
parent: Validation
nav_order: 1
---

# Pinned public NTFS ADS benchmark

Recorded on 2026-10-12 on Linux with Go 1.26.9.

- **Source:** [fkie-cad/hide-and-seek-dataset](https://github.com/fkie-cad/hide-and-seek-dataset/tree/decd14bd9cb39a978a3a2ce0d2a0d472ac88b703/scenario_4_additional_data_units/ntfs_ads), commit `decd14bd9cb39a978a3a2ce0d2a0d472ac88b703`.
- **Image:** the upstream image is a raw NTFS volume with an NTFS boot signature at byte zero. No partition extraction is used.
- **No corpus image is committed or modified.**

## Preparation and scoring

The preparation command downloads 1,707,064 bytes (image and metadata) and creates a 1 GiB logical image. It needs a new output directory. It verifies the pinned byte counts and SHA-256 checksums, bounds decompression, and keeps zero regions as sparse ranges. The harness itself never downloads.

```sh
python3 scripts/prepare-public-validation.py /tmp/nemo-public-corpus
go build -o /tmp/nemo-validate ./tools/validate
sha256sum /tmp/nemo-public-corpus/ntfs_ads/image.img
/tmp/nemo-validate -manifest docs/validation/public-ntfs-ads-v1.json /tmp/nemo-public-corpus
# Expected benchmark status: 1 (three mismatches), not a passing assertion.
sha256sum /tmp/nemo-public-corpus/ntfs_ads/image.img
```

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| metadata.json | 626 | `749ebe42809dcd2fdffd9be8dfad8ecf1ef790ef8dce9836a7249374d9bddb7a` |
| image.img.gz | 1706438 | `9d90e539ea2d40aabf3153773afb2cfc8e1a5ad74f1643aa49a67fa5bfdb8a33` |
| image.img | 1073741824 | `ac31a4bb2d07b0b0d23c31a8501f389758b17df7739ac92fa13edd2e0d7da790` |

The decompressed image hash was the same before and after detection.

The recorded [harness output](public-ntfs-ads-result-v1.json) is **0 pass, 3 fail, 0 missing, 0 unsupported, 0 error**. All three are in `ntfs/named-stream`.

## Independent ground truth

Expected stream names and payload lengths come from the upstream `metadata.json`, not from nemo's output. The manifest uses the literal UTF-8 payload lengths: 11, 19, and 15 bytes.

The published metadata gives basenames only. The actual paths were found independently with `ntfsls -l -R` from ntfs-3g.

These commands did the verification:

```sh
ntfsls -l -R /tmp/nemo-public-corpus/ntfs_ads/image.img
ntfsinfo -F /Documents/TextFiles/file_011.txt /tmp/nemo-public-corpus/ntfs_ads/image.img
ntfsinfo -F /Documents/TextFiles/file_011.txt:secret.txt /tmp/nemo-public-corpus/ntfs_ads/image.img
ntfscat -n secret.txt /tmp/nemo-public-corpus/ntfs_ads/image.img /Documents/TextFiles/file_011.txt
ntfscat /tmp/nemo-public-corpus/ntfs_ads/image.img /Documents/TextFiles/file_011.txt:secret.txt
```

## The discrepancy

`file_011.txt` is MFT record 92. It has an unnamed non-resident DATA attribute and no named DATA attribute.

`file_011.txt:secret.txt` is a separate MFT record, 182. Its FILE_NAME holds the literal colon in the POSIX namespace, and it has an unnamed resident DATA attribute.

- Asking `ntfscat` for stream `secret.txt` on the original file fails with `Cannot find attribute type 0x80.`.
- Reading the separate file returns `This is hidden data` plus a newline (20 bytes). The metadata declares `HIDDEN DATA` (11 bytes).

The other two advertised streams also appear as separate colon-named files:

- `file_052.txt:secret2.txt` holds 20 bytes.
- `file_099.txt:secret3.txt` holds 16 bytes.

Each includes a newline.

Nemo reports no ADS on the original targets. Nothing was changed to treat colon-named POSIX files as ADS, and the expected results were not adjusted to fit the detector. Upstream needs to clarify before this subset can show positive ADS detection accuracy.

## What this does not measure

- Arbitrary slack hiding. Generic non-nemo slack is outside the detector's framing contract.
- Timestomp anomaly detection, which remains unsupported.
- APFS and ext4 corpus coverage.
