# Semester demonstration (target: 8 minutes)

Prerequisites: Linux, Go, Python 3.11+, `mke2fs` and `debugfs` (e2fsprogs), a disposable
working directory, and the release candidate binary. No elevated access is needed
for image demonstrations. Run all commands from the repository root.

| Time | Demonstration |
| --- | --- |
| 0:00–1:00 | Run `go run . version`, `go run . features`, and `go run . --help`; explain image/live limits. |
| 1:00–4:00 | Run `python3 scripts/linux-acceptance.py`; show named-stream, slack, timestamp restoration, and read-only detection checks on generated ext4 images. |
| 4:00–5:00 | Show independent custody-hash checks and original-image preservation in the acceptance report. Explain that live syscall records have different coverage. |
| 5:00–6:30 | Follow the local sample in the validation documentation. Show case outcomes and per-filesystem/technique totals; distinguish synthetic evidence from public-dataset acceptance. |
| 6:30–7:30 | Show release archives and verify `sha256sum -c SHA256SUMS`; explain cross-build versus native runtime evidence. |
| 7:30–8:00 | State pending public data, native-platform QA, teammate review and final release gates. |

Prepare downloads and build binaries before the presentation. Fixture generation
and package construction are setup tasks, not timed presentation promises. Rehearse
with the actual presentation machine, record elapsed time, and shorten explanation
if needed; an automated command rehearsal cannot certify a spoken eight-minute talk.

If the host temporary directory is not ext4, report native live CLI coverage as
skipped; do not demonstrate on a real user file just to satisfy that check. The
image demonstration is the portable fallback. Keep the generated manifests and
results for the session; remove only the printed temporary fixture directory after inspection.
