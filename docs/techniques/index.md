---
title: How techniques work
nav_group: "Contributing"
nav_order: 1
has_children: true
---

# How techniques work

These pages explain the techniques nemo implements. For each one they cover how it works on disk, how much data it holds, how it gets detected, and where the code lives.

They assume you can read Go and a hex dump. You do not need filesystem forensics experience. For a plain summary of what each technique does, see [Techniques](../techniques.html).

The per-filesystem support matrix is on the [features](../cli/features.html#support-matrix) page.

## Shared vocabulary

### Cluster and block

The smallest unit a filesystem allocates. NTFS calls it a cluster. ext4 and APFS call it a block. All three default to 4 KiB. A 1-byte file still takes a whole one.

### Allocated size

The bytes the filesystem reserved for a file. It is always a whole number of clusters.

### Used size

Also called "valid data length". The part of the allocated bytes that holds the file's content.

The gap between allocated and used size is slack. See [slack-space](slack-space.html).

### Resident and non-resident

NTFS and APFS can store a small attribute value inside the file's metadata record. That is a resident value. Otherwise it gets its own allocated block and is non-resident.

The cutoff is roughly the free space left in the record. That is about 700 bytes for a 1 KiB NTFS MFT record and about 3 KiB for an APFS xattr. Resident data has no cluster slack of its own. It shares the record's slack.

### MACB

The four timestamps an examiner builds a timeline from:

- **M**odified: content changed.
- **A**ccessed: content read.
- **C**hanged: metadata changed (`ctime` on NTFS and ext4).
- **B**irth: created (`crtime`).

Not every filesystem stores all four, and no two store them the same way. See [timestomping](timestomping.html).

### Detectable

A technique is detectable when an examiner has a reliable sign that data was hidden. That sign might be a structural oddity, a second copy of the same information that did not change in step, or a journal entry.

"Nemo can't detect it", as with timestomp, describes nemo today. It does not say the technique is undetectable.

### Custody log

Every [`WriteAt`]({{ site.src }}/internal/image/image.go#L6) nemo performs goes through the `internal/custody` decorator, which SHA-256-hashes and logs it. A technique cannot opt out. The logging is built into the structure, not left to convention.

Detection is read-only and never writes to the custody log.

## Implementation Notes

Each technique is a [`Technique`]({{ site.src }}/internal/technique/technique.go#L92) implementation in `internal/technique`, selected by name in [`technique.Get`]({{ site.src }}/internal/technique/technique.go#L105):

- [`namedStreamTechnique`]({{ site.src }}/internal/technique/technique.go#L126)
- [`slackSpaceTechnique`]({{ site.src }}/internal/technique/technique.go#L197)
- [`timestompTechnique`]({{ site.src }}/internal/technique/technique.go#L376)

A technique never imports a filesystem package. It checks whether the [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31) it receives implements the one capability interface it needs. If not, it returns [`technique.ErrUnsupported`]({{ site.src }}/internal/technique/technique.go#L29).

The capability interfaces live in [`internal/filesystem/filesystem.go`]({{ site.src }}/internal/filesystem/filesystem.go):

| Technique    | Capability interface | Methods |
| ------------ | -------------------- | ------- |
| named-stream | [`filesystem.NamedStreamCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L39) | [`WriteStream`]({{ site.src }}/internal/filesystem/filesystem.go#L40) / [`ReadStream`]({{ site.src }}/internal/filesystem/filesystem.go#L41) / [`DeleteStream`]({{ site.src }}/internal/filesystem/filesystem.go#L42) |
| slack-space  | [`filesystem.SlackSpaceCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L52) | [`SlackRegions`]({{ site.src }}/internal/filesystem/filesystem.go#L53) |
| timestomp    | [`filesystem.TimestompCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L81) | [`Timestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L82) / [`SupportsTimestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L83) / [`SetTimestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L84) |

## Further reading

All specs, docs, and tools are collected on the [References](../references.html) page.
