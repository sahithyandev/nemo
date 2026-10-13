# Semester demonstration (8-minute speaker plan)

Use disposable fixtures only. Never point demonstration commands at a user disk,
mounted image, production file, or public forensic dataset. Detection can flag
legitimate metadata; review findings before extracting a selected payload.

## Preparation before the presentation

On the Linux presentation machine, install Python 3.11+ and e2fsprogs (`mke2fs`,
`mkfs.ext4`, `debugfs`); `sh`, `stat` and `truncate` must also be available.
No Go compiler, downloads, sudo or disk mounting are needed for the archived demo.
Build the release archives beforehand using the release instructions in `RELEASE.md`
(the source checkout equivalent is `release/README.md`). Verify the archive with
`sha256sum -c SHA256SUMS`, unpack the matching Linux archive, and change into its
`nemo-VERSION-linux-amd64` directory. The archive contains these runnable assets:

- `nemo` and `validate` (matching target binaries)
- `scripts/demo-release.py` and `scripts/linux-acceptance.py`
- `scripts/macos-acceptance.py` and `scripts/windows-acceptance.py` for native QA
- `docs/validation/create-sample.sh` and `docs/validation/synthetic-ext4-v1.json`
- `BUILD.json`, `LICENSE`, `CHANGELOG.md`, `RELEASE.md`, and `demo.md`

Run an automated rehearsal before presenting:

```sh
python3 scripts/demo-release.py --max-seconds 180
```

Each invocation creates a new temporary child directory and prints its path. It
prepares the sample before starting the three-minute timer, then runs only prebuilt
binaries. Fixture generation inside the acceptance suite is included in the timer.
The timer includes version/features, image hide/detect/extract/clear, timestamp
restoration, independently checked custody hashes, and synthetic sample validation.
If native temporary files are not on ext4, live checks are reported as skipped;
use `--work-parent /path/to/ext4/temp-parent` for a disposable ext4 directory.
The parent must exist. The runner never reuses an existing fixture directory.

Inspect `demo-result.json`, `validation.json`, command logs, the acceptance child
folder and its custody log. `result: passed` means the automated command segment
finished within `--max-seconds`; it does **not** certify an eight-minute spoken
presentation. Retain these files as rehearsal evidence. Setup failure, failed
assertions or an exceeded budget produce exit status 1 and a failed JSON report.

## Speaker plan

This allocates five minutes of narration and three minutes of automated commands.
The presenter must rehearse the spoken sections separately on the actual machine;
spoken timing is not measured by the runner.

| Time | Action and speaking points |
| --- | --- |
| 0:00–0:45 | Explain Nemo's forensic purpose, disposable-only demonstration, and candidate version. |
| 0:45–1:30 | Show `BUILD.json`, supported features and the release notes. Explain image versus live support and native OS limits. |
| 1:30–4:30 | Run `python3 scripts/demo-release.py --max-seconds 180`. Wait for the bounded command segment; do not add unbounded commands. If it finishes early, inspect the printed report within this slot. |
| 4:30–5:30 | Open the printed `demo-result.json` and acceptance log. Show image named-stream/slack extraction, source immutability, exact slack restoration and independently checked custody hashes. Explain skipped live checks explicitly. |
| 5:30–6:15 | Show `validation.json`: per-case and per-technique results, false positives/negatives, and sample hashes. Explain that this is synthetic evidence, not public-dataset acceptance. |
| 6:15–7:00 | Show archive checksum verification prepared beforehand. Distinguish all-four-target cross-builds from native Linux/macOS/Windows runtime evidence. |
| 7:00–8:00 | Explain limitations: live raw slack writes unsupported, conservative ext4 xattr capacity, timestamp anomaly detection is unavailable, detection false positives, privileged/APFS/NTFS constraints, and the public corpus metadata discrepancy; native reports name their tested revisions. |

Do not rerun failed operations against real user files to fill a demonstration slot.
If the automated segment fails or exceeds three minutes, show the retained failure
report and shorten the demonstration; do not report it as a successful rehearsal.
Only delete the printed temporary fixture directory after inspecting the evidence.
