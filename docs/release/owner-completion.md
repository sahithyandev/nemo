# Nirmal's semester release completion

Prepared 2026-10-12 for **0.1.0-rc.1**. The current implementation includes the
owner branches plus latest main's PR #78 native acceptance contribution.

## Owner scope

| Plan ID | Issue | Delivery |
| --- | --- | --- |
| CORE-01 | #1 | Offset-preserving raw/read-only images and temporary-file tests |
| CORE-04 | #4 | Optional filesystem capabilities and documented shared decisions |
| CLI-01 | #8 | Validated image/live hide execution and logical custody output |
| CLI-04 | #11 | Deterministic image matrix and host-specific live limits |
| FS-EXT4-01 | #14 | Geometry/extents/path walking and validated HTree directories |
| FS-EXT4-02 | #21 | Xattr round trips, checksum updates and capacity preservation |
| FS-EXT4-03 | #22 | Timestamp encoding/checksums, original-value restoration and Linux reads |
| FS-EXT4-04 | #23 | Bounded framed slack, source preservation and safe restoration |
| FS-EXT4-05 | #26 | Native Linux xattrs/timestamps, unsupported-child diagnostics and read-only device discovery |
| VAL-02 | #30 | Deterministic local scoring, generated sample and pinned public benchmark |
| REL-01 | #33 | Versioned candidate, release notes, four-target demo packs and bounded rehearsal |

GitHub owner issues #1/#4/#8/#11/#14/#21/#22/#23/#26/#30 were already closed when
this completion pass began. DOC-01 #31 and QA-01 #32 are closed. The public corpus
benchmark does not claim a passing ADS score: three upstream expected streams
are absent, and independent ntfs-3g inspection finds separate colon-named files.
The tracked validation report preserves all three mismatches.

## Release checks

The patched Go 1.26.9 toolchain is used for this candidate. The reached Windows
`os.Root` vulnerability (GO-2026-6604) is addressed; the local vulnerability scan
reports no vulnerabilities. Unit tests, vet, release-format/script checks and the
five offline corpus-preparation safety tests pass.

CI checks vulnerabilities before tagging. The native build matrix runs macOS and
Windows acceptance scripts, and the Linux gate requires ext4 rather than silently
skipping native coverage. Candidate publication waits for these checks, marks the
release as a prerelease, and attaches SBOM, checksum files and standalone demo packs.

## Clean-clone and package rehearsal

Fresh immutable source, archive hashes, native demo timing and source cleanliness
are recorded here after the final functional source commit is rehearsed. Historical
reports in `rehearsal.md` retain their original source IDs; their hashes are not
asserted to match this candidate.

The automatic demonstration has a 180-second command budget and records its
actual elapsed time in JSON. The eight-minute speaker plan allocates the remaining
five minutes to explanation. No recorded human spoken timing is claimed.

## Limits retained

Only disposable fixtures are mutated. Public input is read-only. Physical-disk
writes, arbitrary hidden-slack detection, timestamp anomaly detection and optional
libtsk corroboration are outside the tested candidate workflows. APFS image
editing is in-place with unverified clone/snapshot sharing, Windows live writes
are unavailable, and ext4 allocation/COW restrictions remain explicit.

Final stable-release approval and a human presentation rehearsal are separate
from preparing this semester candidate and bounded demo plan.
