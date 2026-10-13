---
title: Techniques
nav_order: 2
has_children: true
---

# Techniques

These pages explain the anti-forensic techniques nemo implements: what they
are, how they work on disk, how much data they hold, how they get detected, and
where the code lives. It assumes you can read Go and a hex dump but have never
touched filesystem forensics.

The per-filesystem support matrix is on the [features](../cli/features.html#support-matrix) page.

## Shared vocabulary

### Cluster / block

The smallest unit a filesystem allocates. NTFS calls it a
cluster (default 4 KiB); ext4 and APFS call it a block (also 4 KiB by default). A
1-byte file still consumes a whole one.

### Allocated size

How many bytes the filesystem reserved, always a whole number of clusters.

### Used size

Aka. "valid data length". How many of those bytes hold the file's content.

The gap between allocated and used size is slack. See [slack-space.md](slack-space.html).

### Resident vs. non-resident

NTFS and APFS can store a small attribute value
inline inside the file's metadata record (resident) rather than in its own
allocated block (non-resident).

The threshold is roughly the free space left in
the record: about 700 bytes for a 1 KiB NTFS MFT record, about 3 KiB for an APFS
xattr. Resident data has no cluster slack of its own; it shares the record's
slack.

### MACB

The four timestamps a forensic examiner builds a timeline from:

- Modified (content changed)
- Accessed (content read)
- Changed (metadata changed the NTFS/ext4 `ctime`)
- Birth (created, `crtime`).

Not every filesystem stores
all four, and no two store them the same way. See
[timestomping.md](timestomping.html).

### Detectable

A technique is detectable when an examiner has a reliable
signal that data was hidden: a structural anomaly, a second copy of the same
information that was not updated in lockstep, or a journal entry. "nemo can't
detect it" (as with timestomp) describes nemo's current capabilities, not
whether the technique can be detected at all.

### Custody log

Every [`WriteAt`](https://github.com/sahithyandev/nemo/blob/main/internal/image/image.go#L6) nemo performs goes through the
`internal/custody` decorator, which SHA-256-hashes and logs it. A technique
cannot opt out; the logging is structural, not a convention. Detection is
read-only and never writes to the custody log.

## Implementation Notes

Every technique is a [`Technique`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L92) implementation in `internal/technique`, selected by name in [`technique.Get`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L105):

- [`namedStreamTechnique`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L126)
- [`slackSpaceTechnique`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L197)
- [`timestompTechnique`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L376)

A technique never imports a filesystem package. It type-asserts the [`Entry`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L31) it is handed against the one capability interface it needs, and returns [`technique.ErrUnsupported`](https://github.com/sahithyandev/nemo/blob/main/internal/technique/technique.go#L29) when the assertion fails. The capability interfaces are defined in [`internal/filesystem/filesystem.go`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go):

| Technique    | Capability interface | Methods |
| ------------ | -------------------- | ------- |
| named-stream | [`filesystem.NamedStreamCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L39) | [`WriteStream`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L40) / [`ReadStream`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L41) / [`DeleteStream`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L42) |
| slack-space  | [`filesystem.SlackSpaceCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L52) | [`SlackRegions`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L53) |
| timestomp    | [`filesystem.TimestompCapable`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L81) | [`Timestamp`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L82) / [`SupportsTimestamp`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L83) / [`SetTimestamp`](https://github.com/sahithyandev/nemo/blob/main/internal/filesystem/filesystem.go#L84) |
