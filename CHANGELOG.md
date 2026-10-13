# Changelog

## Unreleased

## 0.1.0

Added:

- `nemo extract` reads a hidden payload back out of named streams and slack
  space.
- `nemo features --mode live` prints what live mode supports on the current
  operating system.
- ext4 slack-space hiding in image mode, using safe slack in allocated files.
- ext4 directories that use HTree indexes can now be walked.
- Linux live mode for ext4: user xattrs, access and modify timestamps, optional
  birth-time and ctime reads, diagnostics for skipped children, and verified
  read-only raw slack discovery.
- New `NEM2` slack frames, bound to a nonce. Old `NEMO` frames can still be
  read. Slack backups are tied to the opened file, the ext4 UUID and the exact
  frame digest.
- Local validation with versioned manifests and clear totals for pass, fail,
  missing, unsupported and error. Downloads only happen in the separate public
  preparation script, which pins checksums.
- Demo packs for four targets, with Nemo and validator binaries, runners,
  disposable fixture generation, release notes, build provenance and checksums.
  GoReleaser attaches them to the GitHub release.
- One-line install scripts for Linux, macOS (`scripts/install.sh`) and Windows
  (`scripts/install.ps1`).

Fixed:

- ext4 xattr writes that don't fit now fail without touching the other xattrs.
- ext4 directory records are read correctly on 64 KiB block sizes.
- ext4 inode layouts that Nemo can't handle (inline, encrypted, non-extent,
  unwritten extents) now report an "unsupported" error.
- Windows and macOS acceptance runners now account for the versioned frame
  sizes.

Changed:

- Go is now 1.26.9, which fixes the `os.Root` Windows-junction vulnerability
  GO-2026-6604 that Nemo reaches.
- CI runs govulncheck and native Linux, macOS and Windows acceptance checks
  before the release tag is created.
- Mismatched slack backups now fail. Legacy or foreign manifests aren't restored
  silently, and by default unmatched backups warn and zero-fill.

Known limitations:

- The pinned public NTFS ADS subset reports 0 pass and 3 fail. Checking with
  ntfs-3g showed separate colon-named files instead of the streams the metadata
  lists, and one payload differs. This is recorded as evidence, not as a passing
  ADS accuracy claim. The generated ext4 sample passes all three.
- Linux can't write creation or change times, or raw slack on mounted
  filesystems. Raw reads need suitable device access and may see stale or
  concurrently changing data on a mounted filesystem.
- ext4 external-xattr allocation and shared-block copy-on-write and freeing
  still fail with an error on purpose.

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
