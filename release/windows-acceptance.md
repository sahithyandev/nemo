# Windows/NTFS acceptance evidence

Recorded 2026-10-12 on Windows 11 amd64 (OS 10.0.26300, DiskPart
10.0.26100.1150), using Python 3.11.9 and go1.26.7. The script used an
Administrator shell and disposable fixed VHD/raw NTFS images. No production
filesystem metadata or existing custody log was modified.

## Reproduce

Requires Go, Python 3.11+, and an elevated (Administrator) shell. DiskPart's
disk operations and native live detection's raw-volume open both require
elevation. Run from the repository root:

```powershell
python scripts/windows-acceptance.py
# Optionally use an already-built binary or keep fixtures under an NTFS parent:
python scripts/windows-acceptance.py --nemo C:\path\to\nemo.exe --work-parent .
```

The script builds into its temporary directory by default, uses a private
subprocess `USERPROFILE` for custody records, and prints the retained artifact
directory. It creates a 64 MiB fixed VHD, formats and populates its partition
with DiskPart, then extracts the NTFS partition as the raw filesystem image
Nemo expects. Each command has a timeout and any failed assertion terminates
the run; do not use Python's `-O` option, which disables assertions.

## Observed results

The complete script passed on the native Windows host. It independently
checked six mutation custody records using Python SHA-256 and compared the CLI
records with the persisted JSONL custody log.

| Check | Observed result |
| --- | --- |
| Version, features, root/hide/detect/clear help | PASS |
| Fixed NTFS VHD creation and population via DiskPart | PASS |
| Whole-image default detection | PASS; raw image SHA-256 unchanged |
| Image named-stream hide/detect/clear | PASS |
| Image slack hide/detect/manifest-backed clear | PASS; raw image restored byte-for-byte |
| Image modified timestamp hide/restore | PASS; independently read by mounting the synchronized VHD |
| Original file content after all image mutations | PASS; independently read from the mounted VHD |
| Native live hide for all techniques | PASS; each write refused as designed and host bytes remained unchanged |
| Native live named-stream detection | PASS; raw-volume read succeeded without changing host bytes |
| Independent custody verification | PASS; 6/6 records |

The successful run retained session-local evidence at
`C:\Users\viranske\AppData\Local\Temp\nemo-acceptance-d9wi10p8`. Rerun the
command above to generate fresh evidence.

## Fixes made during this rehearsal

The unexecuted draft needed these corrections before it passed:

- A VHD is a partitioned disk, while Nemo image mode expects a filesystem at
  byte zero. The script now creates a fixed VHD, parses its MBR, extracts the
  NTFS partition into a raw image, and synchronizes that image back before
  independent mount-based checks.
- Whole-image scanning reached NTFS metadata files whose allocation layouts
  are not safe user-file hiding candidates. NTFS named-stream and slack scans
  now skip the reserved root-level `$` metadata namespace, with regression
  tests for `$Boot` and `$Extend` descendants.
- Nemo canonicalizes image paths to forward-slash form, so the script now
  asserts against `/host.txt` instead of the input spelling `\host.txt`.
- The original tiny host file stayed resident inside its MFT record and had no
  cluster slack. The fixture is now large enough to force non-resident data
  while retaining a disposable final-cluster slack region.
- DiskPart does not retain a selected partition across VHD attachments. Every
  re-attach now selects partition 1 explicitly before assigning the drive
  letter.

## Limits

Windows NTFS live mode is intentionally read-only for Nemo mutations, so the
native checks verify refusal rather than a live hide/clear round trip. The
image-mode round trips cover named streams, slack frames, and modified-time
restoration. Timestomp detection intentionally reports no findings because no
historical baseline exists. Custody hashes describe logical mutation data,
not a complete independently persisted audit of every low-level write. A
libtsk cross-check remains deferred out of scope for this release.
