
## Unreleased

- APFS live mode on macOS: named streams (xattr + resource fork) and timestomp work
  against a real path with no `--image`, no elevated privilege needed. Slack space in
  live mode needs root and opens the volume's raw device; a write there always fails
  against a mounted volume (macOS refuses a read-write device open while it's
  mounted), but a read-only `detect` works. Every other OS still reports live mode as
  unsupported.
