---
title: Modes
nav_group: "User Guide"
nav_order: 4
---

# Modes

Each command runs in one of two modes.

- **Image mode** works offline on a raw, unencrypted disk image. Pass `--image <file>` and nemo detects the filesystem from the image. Nothing touches your running system. It is the safe way to practise, and the only way to work on a filesystem your machine cannot mount.
- **Live mode** works on a file on the machine nemo runs on, through the OS's own filesystem calls. Leave out `--image` and nemo uses the native backend for your platform. Use it to hide, read, or clear data on your own machine.

Nemo never falls back from one mode to the other. If a mode's backend is not available, the command fails with a clear error.

## What each platform supports

Image mode has the full feature set everywhere, because nemo parses the image itself. [`nemo features`](cli/features.html) shows the image-mode matrix.

Live mode is limited to what each OS lets a program do without touching the raw disk:

| Platform | named-stream | timestomp | slack-space |
| -------- | ------------ | --------- | ----------- |
| macOS (APFS) | yes, via xattrs (resource fork included) | yes | read-only, needs raw-device access |
| Linux (ext4) | yes, `user.*` xattrs on verified mounts | access and modified times only | detect and extract only, needs the backing block device |
| Windows (NTFS) | read-only | read-only | read-only |

## Live mode limits

**Slack-space needs elevated access.** No normal file API can reach the unused tail of a cluster. Nemo has to open the raw block device, which takes administrator on Windows or root on macOS and Linux. Without it, the command fails with a missing-privilege error. It does not degrade. See [privilege in slack-space hiding](techniques/slack-space.html#privilege).

**Writes are narrower than reads.** Windows live mode is read-only. Linux live slack-space can only detect and extract. Even a supported row depends on the target's format, layout, and your permissions.

If the live backend does not support a platform or target, nemo refuses the command. It never guesses.

For how the modes are wired in the code, see [Mode implementation](architecture/modes.html).
