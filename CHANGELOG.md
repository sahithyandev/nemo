
## Unreleased

- APFS live mode on macOS: named streams (xattr + resource fork) and timestomp work
  against a real path with no `--image`, no elevated privilege needed. Slack space in
  live mode opens the volume's raw device: a write always fails against a mounted
  volume (macOS refuses a read-write open of the buffered device while it's mounted),
  but `detect` opens the raw character device instead and works, needing root only
  when the device node itself isn't already readable by the current user. Every other
  OS still reports live mode as unsupported.
