# Pinned public NTFS ADS benchmark

Recorded on 2026-10-12 on Linux using Go 1.26.9. Source:
[fkie-cad/hide-and-seek-dataset](https://github.com/fkie-cad/hide-and-seek-dataset/tree/decd14bd9cb39a978a3a2ce0d2a0d472ac88b703/scenario_4_additional_data_units/ntfs_ads),
commit `decd14bd9cb39a978a3a2ce0d2a0d472ac88b703`.
No corpus image is committed or modified. The upstream image is a raw NTFS
volume, with an NTFS boot signature at byte zero; no partition extraction is used.

## Explicit preparation and scoring

The preparation command explicitly downloads 1,707,064 bytes (image and metadata)
and creates a 1 GiB logical image. It requires a new output directory, verifies
pinned byte counts and SHA-256 checksums, bounds decompression, and preserves
zero regions as sparse ranges. The harness itself never downloads.

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

The decompressed image hash was identical before and after detection.
Recorded [harness output](public-ntfs-ads-result-v1.json): **0 pass, 3 fail,
0 missing, 0 unsupported, 0 error**, all in `ntfs/named-stream`.

## Independent ground truth and discrepancy

Expected stream names and payload lengths come from upstream `metadata.json`,
not Nemo output. The manifest uses its literal UTF-8 payload lengths (11, 19,
15 bytes). Actual paths were independently located with `ntfsls -l -R` from
ntfs-3g. Published metadata supplies basenames only.

Independent verification used:

```sh
ntfsls -l -R /tmp/nemo-public-corpus/ntfs_ads/image.img
ntfsinfo -F /Documents/TextFiles/file_011.txt /tmp/nemo-public-corpus/ntfs_ads/image.img
ntfsinfo -F /Documents/TextFiles/file_011.txt:secret.txt /tmp/nemo-public-corpus/ntfs_ads/image.img
ntfscat -n secret.txt /tmp/nemo-public-corpus/ntfs_ads/image.img /Documents/TextFiles/file_011.txt
ntfscat /tmp/nemo-public-corpus/ntfs_ads/image.img /Documents/TextFiles/file_011.txt:secret.txt
```

`file_011.txt` is MFT record 92 with an unnamed nonresident DATA attribute and
no named DATA attribute. `file_011.txt:secret.txt` is a separate MFT record 182,
whose FILE_NAME contains the literal colon in POSIX namespace, with an unnamed
resident DATA attribute. Asking ntfscat for stream `secret.txt` on the original
file fails with `Cannot find attribute type 0x80.`. Reading the separate file
returns `This is hidden data` followed by a newline (20 bytes), whereas metadata
declares `HIDDEN DATA` (11 bytes).

The two other advertised streams likewise appear as separate colon-named files:
`file_052.txt:secret2.txt` contains 20 bytes and `file_099.txt:secret3.txt` contains
16 bytes, each including a newline. Nemo reports no ADS on the original targets.
No changes were made to treat colon-named POSIX files as ADS or adjust expected
results to the detector. Upstream clarification is needed before this subset
can establish positive ADS detection accuracy.

This result does not measure arbitrary slack hiding, timestomp anomaly detection,
or APFS/ext4 corpus coverage. Generic non-Nemo slack is outside the detector's
framing contract, and timestamp anomaly detection remains unsupported.
