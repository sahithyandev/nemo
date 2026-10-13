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

Recorded source: `026a30583cd5aee85f8ffae9395088879f5297b1`. Toolchain: `go version go1.26.9 linux/amd64`.
This was a fresh local clone with cached toolchain/dependencies, not a cold-cache
or offline-install test. All checks below succeeded; the clone remained free of
tracked modifications and untracked files afterward.

| Check | Result | Seconds |
| --- | --- | ---: |
| source | pass | 0.004 |
| clean-before | pass | 0.023 |
| toolchain | pass | 0.015 |
| go-test | pass | 11.075 |
| go-vet | pass | 0.228 |
| release-format | pass | 0.022 |
| release-notes-tests | pass | 0.066 |
| prepare-tests | pass | 0.148 |
| pack-1 | pass | 2.345 |
| pack-2 | pass | 2.354 |
| packaged-version | pass | 0.004 |
| demo | pass | 1.168 |
| clean-after | pass | 0.004 |

Two complete packaging runs produced byte-identical archives and checksum
manifests. Each archive hash was independently recomputed with Python SHA-256:

```text
7bb5cf8ea546fdbd5726759410adb52f9ca0d8bca0e3a7c75acc9652ada411b8  nemo-0.1.0-rc.1-linux-amd64.tar
cc79c717b9cfafecc180518dbbc9fa1969b37e2e2ffefad001a55d5c6566d314  nemo-0.1.0-rc.1-darwin-amd64.tar
d9746372d6862117685f8ffaff75b8c8f92b1e3597d66d00acd25f5d6673fe49  nemo-0.1.0-rc.1-darwin-arm64.tar
6e0fe6b20793ecec296db8b392b9b555bc25bc3ab412a11f9853a89a7581aa1b  nemo-0.1.0-rc.1-windows-amd64.zip
```

The unpacked Linux binary reported `0.1.0-rc.1`. Its archived demo completed in
**0.999 seconds** under the 180-second command budget on
an ext4 temporary parent. Native xattrs/timestamps/extraction, skipped symlink/FIFO
warnings, permission denial, source immutability, exact restoration and **10
independently checked custody records** passed. The generated sample passed all
three validation cases with identical before/after image hashes.

The source-preparation safety suite passed five offline tests. Go tests/vet,
release-format/release-note tests, reached-vulnerability scan and SBOM generation
passed. The public corpus results are tracked separately as three genuine
metadata mismatches, not three successful detections.

These hashes belong to the named immutable rehearsal commit. The final evidence
commit changes documentation only; packages include documentation and commit
provenance, so archives built from later commits need their own `SHA256SUMS`.
Historical reports in `rehearsal.md` retain their original source IDs. GitHub CI
will test the final PR/main source and publish that source's own archive checksums.

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
