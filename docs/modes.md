---
title: Modes
nav_group: "User Guide"
nav_order: 4
---

# Modes

Nemo runs in one of two modes, chosen per command:

- **Image mode** operates offline against a raw, unencrypted disk image. Pass
  `--image <file>` and nemo detects the filesystem from the image itself. Nothing
  touches your running system, so it is the safe way to practise and the only way
  to work a filesystem your machine cannot mount.
- **Live mode** operates directly on a file on the machine nemo runs on, through
  the OS's own filesystem calls. Omit `--image` and nemo uses the native backend
  for your platform. This is the mode you reach for to hide, read, or clear data
  on your own machine.

Nemo never falls back from one mode to the other. If the backend for a mode is
not available, the command fails with a clear error rather than silently doing
something else.

## What each platform supports

Live mode is bounded by what each OS lets a program do without reaching for the
raw disk. Image mode has the full feature set everywhere, because it parses the
image directly; see [`nemo features`](cli/features.html) for the image-mode
matrix.

| Platform | named-stream | timestomp | slack-space |
| -------- | ------------ | --------- | ----------- |
| macOS (APFS) | yes, via xattrs (resource fork included) | yes | read-only, needs raw-device access |
| Linux (ext4) | yes, `user.*` xattrs on verified mounts | access and modified times only | detect/extract only, needs the backing block device |
| Windows (NTFS) | read-only | read-only | read-only |

Two things set live mode apart from image mode:

- **Slack-space live needs elevated access.** The unused tail of an allocated
  cluster is not reachable through any normal file API, so nemo has to open the
  raw block device, which requires administrator (Windows) or root (macOS,
  Linux). Without it, a slack command in live mode fails with a missing-privilege
  error instead of degrading. See [Slack-Space Hiding](techniques/slack-space.html#privilege).
- **Writes are narrower than reads.** Windows live mode is read-only across the
  board, and Linux live slack is detect/extract only. A `supported` row still
  depends on the target's format, layout, and your permissions.

Running a command against a platform or target the live backend does not support
is refused, never guessed at.

For how the two modes are wired in the code, see
[Mode implementation](architecture/modes.html).
