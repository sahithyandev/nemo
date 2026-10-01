# Changelog

## 0.1.0-rc.1 — semester release candidate

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
