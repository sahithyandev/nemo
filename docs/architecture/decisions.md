---
title: Architecture Decisions
parent: Architecture
nav_order: 6
---

# Shared contract decisions

These decisions are the shared boundaries used by the filesystem, technique,
CLI, and live-mode implementations.

## Custody logging

Image mutations receive a custody-wrapped image before technique execution.
The wrapper records successful write offsets, SHA-256 hashes and UTC timestamps
in memory. The CLI then persists one logical operation record to
`~/.nemo/logs/custody.jsonl` and echoes it on stdout: operation, technique, target,
detail, payload byte count, hash and UTC timestamp. Native writes use that same
logical audit path around their syscall.

Audit/echo failure returns an error and is never reported as complete success.
Logging occurs after mutation and does not roll back a write; these records are
not a transactional write-ahead log, and per-offset wrapper events are not the
persisted JSON Lines schema. Slack backups are persisted before overwriting bytes.

See [Image and Custody](image.md) and [Modes](modes.md).

## Slack framing

New slack payloads use a 28-byte `NEM2` header: magic, payload length, CRC32
and a random 16-byte per-hide nonce. Historical 12-byte `NEMO` frames remain
readable. The nonce prevents a repeated identical payload from qualifying a stale
backup. Detection recognizes valid frames, not arbitrary non-zero residual bytes.
Hide writes one frame in the first region that fits; it does not distribute a
frame across regions. Insufficient capacity is rejected before a write.

See [Technique Interfaces](technique.md) and
[Slack-Space Hiding](../techniques/slack-space.md).

## Clear backup manifest

`hide` persists original slack bytes before mutation. Records carry the internal
target, offset range, original bytes, backing-image identity and exact frame hash.
File identity includes device/inode or Windows volume/file index and size; ext4
also includes its filesystem UUID. `clear` validates identity and frame digest
before restoring. An explicit manifest requires a match; the default manifest
warns and zero-fills when only legacy/foreign records are available. A stale frame
is rejected without mutation.

Timestamp hiding does not automatically save the original value. Timestamp clear
requires the user-supplied original; detection has no historical anomaly baseline.

See [Hide](../cli/hide.md), [Clear](../cli/clear.md), and
[Technique Interfaces](technique.md).

## Capability boundaries

Filesystem entries expose named-stream, slack-space, and timestamp operations
through separate optional capabilities. A technique requests only the
capability it needs and receives a stable unsupported error when an entry or
platform cannot provide it. This keeps unsupported live and filesystem cases
explicit rather than silently falling back to another mode.

See [Filesystem Interfaces](filesystem.md) and [Modes](modes.md).