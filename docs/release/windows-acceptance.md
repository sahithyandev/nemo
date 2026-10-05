# Windows/NTFS acceptance evidence

**Not yet run on real hardware.** `scripts/windows-acceptance.py` is a draft
written from reading `internal/filesystem/ntfs`'s live and image code on a
machine with no Windows access. It has never executed. Treat everything
below as a plan, not a result, until someone runs it on an actual Windows
host and this file is rewritten from that output (the way
[macOS acceptance](macos-acceptance.md) was rewritten after its own first
real run surfaced five fixes the draft alone couldn't have caught).

## What it's expected to cover

Mirrors [Linux](linux-acceptance.md) and [macOS acceptance](macos-acceptance.md)'s
shape: version/features/help, an image-mode round trip (named-stream,
slack-space, timestomp) against a disposable NTFS volume, and independent
custody-hash verification. The NTFS volume is a `diskpart`-backed VHD file
rather than a physical disk, formatted and populated by the script itself.

Native live mode is different on Windows than on Linux or macOS: per
`internal/filesystem/ntfs/live_windows.go`, every live write refuses
unconditionally with "live writes require offline image mode", regardless
of technique. There is no live round trip to test. The script instead
asserts that refusal for all three techniques, then checks that live
`detect` (read-only) still works.

## Reproduce (once this stops being a draft)

Requires Go, Python 3.11+, and an elevated (Administrator) shell: `diskpart`'s
disk operations and live `detect`'s raw volume open both need that
privilege, per the same file's `OpenLive` comment.

```sh
python3 scripts/windows-acceptance.py
python3 scripts/windows-acceptance.py --nemo C:\path\to\nemo.exe --work-parent .
```

## Known-risky parts of the draft

These are the places most likely to need correction against a real machine,
flagged in advance rather than discovered silently:

- The `diskpart` script text (`create vdisk` / `attach vdisk` / `create
  partition primary` / `format fs=ntfs quick` / `assign letter=`) is built
  from diskpart's documented syntax, not a verified transcript.
- `free_letter()` picks the first unused letter from `Z` down to `Q` by
  checking `Path(f'{letter}:\\').exists()`; this hasn't been checked against
  how diskpart reports failure when every candidate is somehow taken.
- The custody log path assumes `os.UserHomeDir()` resolves through
  `USERPROFILE` on Windows (confirmed by reading `internal/custody/log.go`,
  not by running it) and that setting that environment variable before
  `go build`/`nemo.exe` is enough to redirect it.
- The exact wording of the live-refusal error (`live writes require offline
  image mode`) is checked case-insensitively against the source string in
  `live_windows.go`; a wrapped or reworded error at a higher layer could
  still break the assertion.

## Limits

Same scope limits as the Linux and macOS evidence: timestomp detection
reports no findings by design, and custody hashes describe logical mutation
data, not a full independent audit of every low-level write. A libtsk
cross-check (INT-01) is deferred out of scope for this release.

This file does not certify anything until it is replaced with real output.
