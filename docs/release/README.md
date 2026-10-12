# v0.1 semester release candidate

Current version: **0.1.0-rc.1**, selected through `cmd/VERSION`. This is the
semester candidate for review/demo, not the final stable v0.1 release. GitHub
marks it as a prerelease. The one-line stable installers use `/releases/latest`
and therefore do not automatically install this candidate.

## Build and package

Use the patched Go 1.26.9 toolchain, Python 3.11+, and Git. First-time toolchain
and dependency downloads need network access. From a clean source checkout:

```sh
export GOTOOLCHAIN=go1.26.9
go test ./...
go vet ./...
make vulncheck
python3 scripts/package-release.py --output /tmp/nemo-release-candidate
cd /tmp/nemo-release-candidate
sha256sum -c SHA256SUMS
```

The output directory must be new. Packages target Linux amd64, macOS amd64/arm64
and Windows amd64 with CGO disabled. Each includes Nemo and validator binaries,
release/validation instructions, disposable demo runners, sample creation assets,
license, changelog and `BUILD.json`. Go paths/VCS stamping/build IDs and archive
metadata are normalized. Byte reproducibility requires identical source, Go,
Python/zlib and dependencies; repeat packaging into another new directory and
compare `SHA256SUMS`. Packaging does not tag or upload anything.

The CI release separately builds its standard platform archives, an SBOM and
these semester demo packs. Main publication requires formatting/release checks,
vet, reached-vulnerability scanning, Linux/Windows/macOS tests/builds and native
acceptance jobs. `cmd/VERSION` plus its nonempty changelog section selects the
automatic candidate tag. No manual pre-created tag is needed.

## Run an unpacked Linux demo pack

Install Python 3.11+, e2fsprogs and ordinary POSIX tools before presenting. Unpack
the Linux `.tar`, change into its `nemo-0.1.0-rc.1-linux-amd64` directory, then run:

```sh
python3 scripts/demo-release.py --max-seconds 180
```

The runner uses prebuilt binaries and only newly created disposable fixtures. It
prints retained evidence and exits nonzero on a failed check or time-budget
failure. `--work-parent /path/on/ext4` also exercises native Linux operations;
otherwise a non-ext4 temporary filesystem is explicitly reported as skipped.
The three-minute command budget plus the five-minute speaker plan is described
in [demo.md](demo.md). Spoken presentation timing is not measured by the runner.

## Evidence and scope

See [clean-clone rehearsal](rehearsal.md), [Linux acceptance](linux-acceptance.md),
[macOS acceptance](macos-acceptance.md), and [Windows acceptance](windows-acceptance.md).
Historical reports name their actual tested source/platform; cross-builds alone
are not runtime evidence. DOC-01 #31 and QA-01 #32 were closed before this candidate
preparation, and the latest main acceptance contribution (PR #78) is integrated.

The public NTFS benchmark retains upstream expected ADS labels and reports three
mismatches: independent tools locate separate POSIX colon-named files, not named
streams. This discrepancy is documented in the source repository's validation
reports; it is not relabeled as passing accuracy. Public images are not bundled.
The deterministic generated ext4 sample and controlled native/image fixtures
remain the positive functional evidence.

## Safety and limitations

- Image parsing/writes support documented NTFS/APFS/ext4 layouts; check `features`.
- Linux native user xattrs/access/modify writes and optional birth/ctime reads;
  explicit live slack reads are device-verified but not a consistent snapshot.
  Linux creation/change-time and mounted slack writes remain refused.
- Windows native NTFS access is read-only; APFS image writes are in-place with
  no verified clone/snapshot isolation. Use new unshared APFS fixtures only.
- Detection recognizes Nemo frames, not arbitrary slack or timestomp anomalies.
  Historical `NEMO` frames remain readable; new `NEM2` frames use a per-hide nonce.
- Backups are bound to the backing image and exact frame. A stale/explicit foreign
  manifest fails; the default unmatched backup warns and zero-fills.
- Audit errors do not roll back an earlier syscall/write. Logical live records
  differ from raw image wrapper events. Optional libtsk corroboration is deferred.

Mutations can destroy data. Demonstrate only on disposable files and unmounted
image copies, retain originals/manifests, and never use valuable files or physical
production disks. Candidate publication does not assert a stable final release or
unmeasured native/human acceptance.
