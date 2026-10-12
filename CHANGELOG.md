# Changelog

## Unreleased

## 0.1.0-rc.1

Semester release candidate: a reproducible demo/review build, published as a
prerelease. The stable installer continues to select the latest stable release.

Features:

- Forensics CLI with image/live mode, hide, detect, selected payload extraction,
  clear, feature matrices, version and help.
- Ext4 image xattrs, four inode timestamps, safe allocated-file slack and validated
  HTree directory walking. Capacity failures preserve unrelated xattrs.
- Linux live user xattrs and access/modify timestamps, optional birth-time/ctime
  reads, skipped-child diagnostics and verified read-only raw slack discovery.
- New nonce-bound `NEM2` frames; historical `NEMO` frames remain readable. Slack
  backups bind to opened-file identity, ext4 UUID and exact frame digest.
- APFS/NTFS image support and native APFS operations; Windows live NTFS is read-only.
- Local validation with versioned manifests and explicit pass/fail/missing/
  unsupported/error totals. Downloads only occur in the separately invoked,
  checksum-pinned public preparation script.
- Four-target demo packs include Nemo/validator binaries, runners, disposable
  fixture generation, release notes, build provenance and checksums.

Validation and safeguards:

- Go unit tests, vet, native Linux acceptance, exact restoration/extraction,
  independently checked custody hashes and read-only-source checks.
- Clean-clone packaging/rehearsal evidence and the timed command-segment report
  are recorded in `docs/release/`; the eight-minute speaker plan does not claim
  a measured human presentation.
- Native macOS/Windows acceptance evidence was contributed in PR #78. Updated
  native runners account for versioned frame sizes; native jobs gate publication.
- Go 1.26.9 fixes the reached `os.Root` Windows-junction vulnerability
  GO-2026-6604. Vulnerability and native acceptance checks run before tag creation.
- The pinned public NTFS ADS subset reports 0 pass / 3 fail. Independent ntfs-3g
  inspection found separate colon-named files instead of the metadata's advertised
  streams, including a payload discrepancy. This is recorded evidence, not a
  passing ADS accuracy claim. The generated ext4 sample reports three passes.

Known limitations and safety:

- Timestomp anomaly detection has no historical baseline; timestamp restoration
  requires the supplied original. Arbitrary non-Nemo slack is not detected.
- Linux creation/change-time and mounted raw slack writes are unavailable; raw
  reads require suitable device access and may see stale/concurrent mounted data.
- Ext4 external-xattr allocation, shared-block CoW/freeing and unsupported inode
  layouts remain conservative errors. Windows live writes are refused.
- APFS mutation is in-place, not a transactional CoW editor. Clone/snapshot
  sharing is not verified; use only new, unshared disposable image fixtures.
- Legacy/foreign manifests are not silently restored. Explicit mismatches and
  stale frames fail; default unmatched backups warn and zero-fill.
- Custody-log/echo errors after mutation do not imply rollback; native syscall
  records do not include the raw image wrapper's offset events.
- Mutations can destroy data. Use disposable files and unmounted image copies,
  preserve original images and manifests, and never demonstrate on valuable data.
  Optional libtsk cross-checking remains deferred.

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
