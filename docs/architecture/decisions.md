---
title: Architecture Decisions
parent: Architecture
nav_order: 6
---

# Shared contract decisions

These decisions set the boundaries shared by the filesystem, technique, CLI, and live-mode code.

## Custody logging

Image changes get a custody-wrapped image before the technique runs. The wrapper records each successful write's offset, SHA-256 hash, and UTC timestamp in memory.

The CLI then saves one record per logical operation to `~/.nemo/logs/custody.jsonl` and echoes it on stdout. The record holds the operation, technique, target, detail, payload byte count, hash, and UTC timestamp. Native writes use the same audit path around their syscall.

- A failure to audit or echo returns an error. It is never reported as full success.
- Logging happens after the change and does not roll it back.
- The records are not a transactional write-ahead log.
- Per-offset wrapper events are not the saved JSON Lines schema.
- Slack backups are saved before bytes are overwritten.

See [Image and custody](image.html) and [Modes](modes.html).

## Slack framing

New slack payloads use a 28-byte `NEM2` header: magic, payload length, CRC32, and a random 16-byte nonce per hide. Older 12-byte `NEMO` frames stay readable.

- The nonce stops a repeated identical payload from matching a stale backup.
- Detection recognizes valid frames, not arbitrary non-zero leftover bytes.
- `hide` writes one frame into the first region that fits. It does not split a frame across regions.
- Too little capacity is rejected before any write.

See [Technique interfaces](technique.html) and [slack-space hiding](../techniques/slack-space.html).

## Clear backup manifest

`hide` saves the original slack bytes before changing anything. Each record holds:

- the internal target
- the offset range
- the original bytes
- the backing image's identity
- the exact frame hash

Image identity is device and inode, or Windows volume and file index, plus size. For ext4 it also includes the filesystem UUID.

`clear` checks the identity and frame digest before restoring.

- An explicit manifest must match.
- With the default manifest, if only legacy or foreign records exist, `clear` warns and zero-fills.
- A stale frame is rejected without any change.

Timestamp hiding does not save the original value. Timestamp `clear` needs the original from the user. Detection has no historical baseline to flag anomalies against.

See [hide](../cli/hide.html), [clear](../cli/clear.html), and [Technique interfaces](technique.html).

## Capability boundaries

Filesystem entries offer named-stream, slack-space, and timestamp operations as separate optional capabilities. A technique asks for only the one it needs. If the entry or platform cannot provide it, the technique gets a stable unsupported error.

That keeps unsupported live and filesystem cases explicit. Nemo never silently falls back to another mode.

See [Filesystem interfaces](filesystem.html) and [Modes](modes.html).
