
## Unreleased

## 0.0.1

First release.

- `hide`/`detect`/`clear`: work against a raw disk image via `--image`.
- `hide`/`detect`/`clear`: for APFS on macOS, also work directly against a live path
  with no image and no elevated privilege needed.
- `features`: prints the filesystem x technique support matrix. `version` and `help`
  round out the CLI.
- NTFS, APFS, ext4: detection, directory traversal, and named-stream support (NTFS
  ADS, APFS xattr/resource fork, ext4 xattr). Each parser refuses any on-disk layout
  it doesn't fully understand rather than guessing.
- APFS, ext4: timestomp support.
- NTFS, APFS, ext4: slack-space hiding in image mode. In APFS live mode this works
  read-only (`detect`, against the volume's raw device); a live write always fails
  since macOS refuses a read-write open of a mounted volume's buffered device.
- `hide`/`clear`: every successful run writes a chain-of-custody record (hashed,
  logged) with no way to opt out.
- `hide`: backs up overwritten slack-space bytes to a JSON Lines manifest (default
  `nemo-manifest.jsonl`) so `clear` can restore them.
- CI: builds, vets, and tests on all platforms. Tagged releases run through
  goreleaser with an SBOM attached.
