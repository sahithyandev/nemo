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

Every technique is one `Technique` implementation in `internal/technique`
(`namedStreamTechnique`, `slackSpaceTechnique`, `timestompTechnique`), selected
by name in `technique.Get`. A technique never imports a filesystem package. It
type-asserts the `Entry` it is handed against the one capability interface it
needs and returns `technique.ErrUnsupported` when the assertion fails
(`internal/technique/technique.go`). The capability interfaces are defined in
`internal/filesystem/filesystem.go`:

| Technique     | Capability interface           | Methods                                       |
| ------------- | ------------------------------ | -------------------------------------------- |
| named-stream  | `filesystem.NamedStreamCapable` | `WriteStream` / `ReadStream` / `DeleteStream` |
| slack-space   | `filesystem.SlackSpaceCapable`  | `SlackRegions`                                |
| timestomp     | `filesystem.TimestompCapable`   | `Timestamp` / `SupportsTimestamp` / `SetTimestamp`         |

## Support matrix

This table covers image-mode capabilities; filesystem layout restrictions apply.
See [Modes](../architecture/modes.html) for native-platform limits.

| Filesystem | named-stream | slack-space | timestomp |
| ---------- | ------------ | ----------- | --------- |
| ext4       | `internal/filesystem/ext4/ext4.go` and `xattr.go` | bounded image layouts | `internal/filesystem/ext4/timestomp.go` |
| APFS       | `internal/filesystem/apfs/namedstream.go` (in-place B-tree leaf rewrite, no allocation) | bounded image layouts | yes |
| NTFS       | `internal/filesystem/ntfs/namedstream.go` (resident and non-resident, MFT-mirror aware) | bounded image layouts | yes |
| fakefs     | yes | yes | yes (test double, `internal/filesystem/fakefs`) |

`fakefs` is an in-memory filesystem that implements all three capabilities
against a byte-slice image. It is how `internal/technique` gets tested without a
real NTFS, APFS, or ext4 image. A real filesystem's `Entry` implements the same
interfaces `fakefs.Entry` already does.

`nemo features` prints this matrix at runtime from the `Techniques` field each
filesystem declares on its `filesystem.Detector`, so it cannot drift from what is
actually registered. See [Filesystem Interfaces](../architecture/filesystem.html).

## Shared vocabulary

**Cluster / block.** The smallest unit a filesystem allocates. NTFS calls it a
cluster (default 4 KiB); ext4 and APFS call it a block (also 4 KiB by default). A
1-byte file still consumes a whole one.

**Allocated size vs. used size.** The allocated size is how many bytes the
filesystem reserved, always a whole number of clusters. The used size (sometimes
"valid data length") is how many of those bytes hold the file's content. The gap
is slack. See [slack-space.md](slack-space.html).

**Resident vs. non-resident.** NTFS and APFS can store a small attribute value
inline inside the file's metadata record (resident) rather than in its own
allocated block (non-resident). The threshold is roughly the free space left in
the record: about 700 bytes for a 1 KiB NTFS MFT record, about 3 KiB for an APFS
xattr. Resident data has no cluster slack of its own; it shares the record's
slack.

**MACB.** The four timestamps a forensic examiner builds a timeline from:
Modified (content changed), Accessed (content read), Changed (metadata changed,
the NTFS/ext4 `ctime`), Birth (created, `crtime`). Not every filesystem stores
all four, and no two store them the same way. See
[timestomping.md](timestomping.html).

**"Detectable."** A technique is detectable when an examiner has a reliable
signal that data was hidden: a structural anomaly, a second copy of the same
information that was not updated in lockstep, or a journal entry. "nemo can't
detect it" (as with timestomp) describes nemo's current capabilities, not
whether the technique can be detected at all.

**Custody log.** Every `WriteAt` nemo performs goes through the
`internal/custody` decorator, which SHA-256-hashes and logs it. A technique
cannot opt out; the logging is structural, not a convention. Detection is
read-only and never writes to the custody log.
