# Changelog

## Unreleased

- Adds `extract`: reads a named-stream or slack-space payload back out to a file
  or stdout. Read-only, no custody log entry, no manifest needed (slack payloads
  are self-describing via the existing frame). `timestomp` has no payload to
  extract.
- Integrates latest main with ext4 timestamp, bounded slack-space and Linux live
  operations; shared detect/clear APIs and slack backup format are retained.
- Adds a deterministic local-dataset validation harness, explicit missing and
  unsupported outcomes, and a reproducible synthetic ext4 sample.
- Adds Linux/ext4 acceptance automation and independent custody-hash checks.
- Adds reproducible Linux, macOS and Windows packaging and a semester demo plan.

Validation evidence and release gates are recorded in `docs/release/`. The public
hide-and-seek subset is not bundled; public-dataset acceptance is pending a locally
supplied, verified corpus. Native Windows/macOS runtime acceptance and teammate
sign-off remain pending. Cross-builds do not establish runtime compatibility.

Known limitations: no reliable timestomp detection, Nemo-frame-only slack detection,
no Linux live slack or creation/change-time writes, read-only Windows NTFS live
mode, and filesystem-specific parser restrictions. The former unmerged ext4 slack
frame format is not compatible with the shared frame format. Native live custody
records do not include raw image write hashes.

Use disposable unmounted image copies and disposable live files only. Mutations
can destroy data. Preserve backups and manifests; clearing without the matching
slack backup zero-fills bytes. No final v0.1 tag has been approved or published.

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
