# Hiding Techniques

This directory explains the anti-forensic techniques nemo implements: what they
are, how they work on disk, how much data they hold, how they get detected, and
where the code lives. It assumes you can read Go and a hex dump but have never
touched filesystem forensics.

Read the shared vocabulary below first, then the technique you are working on:

- [named-streams.md](named-streams.md) covers NTFS Alternate Data Streams, ext4
  and APFS extended attributes, and APFS resource forks.
- [slack-space.md](slack-space.md) covers writing into allocated-but-unused bytes
  at the tail of a cluster, block, or metadata record.
- [timestomping.md](timestomping.md) covers rewriting a file's MACB timestamps.

## Where each technique sits in the codebase

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
| timestomp     | `filesystem.TimestompCapable`   | `SetTimestamp` (no reader, see below)         |

## Support matrix (current)

| Filesystem | named-stream | slack-space | timestomp |
| ---------- | ------------ | ----------- | --------- |
| ext4       | done, `internal/filesystem/ext4/ext4.go` and `xattr.go` | not built | done, `internal/filesystem/ext4/timestomp.go` |
| APFS       | parser only, `NamedStreams` stubbed | not built | not built |
| NTFS       | package not created | not built | not built |
| fakefs     | done | done | done (test double, `internal/filesystem/fakefs`) |

`fakefs` is an in-memory filesystem that implements all three capabilities
against a byte-slice image. It is how `internal/technique` gets tested without a
real NTFS, APFS, or ext4 image. A real filesystem's `Entry` implements the same
interfaces `fakefs.Entry` already does.

`nemo features` prints this matrix at runtime from the `Techniques` field each
filesystem declares on its `filesystem.Detector`, so it cannot drift from what is
actually registered. See `docs/architecture.md`.

## Shared vocabulary

**Cluster / block.** The smallest unit a filesystem allocates. NTFS calls it a
cluster (default 4 KiB); ext4 and APFS call it a block (also 4 KiB by default). A
1-byte file still consumes a whole one.

**Allocated size vs. used size.** The allocated size is how many bytes the
filesystem reserved, always a whole number of clusters. The used size (sometimes
"valid data length") is how many of those bytes hold the file's content. The gap
is slack. See [slack-space.md](slack-space.md).

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
[timestomping.md](timestomping.md).

**"Detectable."** A technique is detectable when an examiner has a reliable
signal that data was hidden: a structural anomaly, a second copy of the same
information that was not updated in lockstep, or a journal entry. "nemo can't
detect it" (as with timestomp) describes nemo's current capabilities, not
whether the technique can be detected at all.

**Custody log.** Every `WriteAt` nemo performs goes through the
`internal/custody` decorator, which SHA-256-hashes and logs it. A technique
cannot opt out; the logging is structural, not a convention. Detection is
read-only and never writes to the custody log.

## Techniques nemo does not implement

These are out of scope per `docs/overall-plan.md`. Recognise them so you do not
reach for one by accident, and so you know where nemo's guarantees stop.

**Unallocated-space hiding and file carving.** Writing a payload into blocks the
filesystem currently marks free, or recovering files from there. The next
allocation overwrites it, and recovering it is a data-recovery problem rather
than a filesystem-parsing one. nemo only touches space the filesystem considers
allocated.

**Reserved metadata regions.** NTFS keeps about 16 MFT records reserved
(`$MFT` through `$Extend`), and the `$Boot` file's tail is padding. ext4
reserves inodes 1 through 10 and pads its group descriptors. Data parked there
survives normal use, but any `fsck` or `chkdsk` flags or clobbers it, and
writing there risks corrupting the volume.

**`$BadClus` and bad-block-list marking.** Telling NTFS (`$BadClus`) or ext4 (the
bad-block inode) that a good cluster is bad so the filesystem routes around it,
then reading it directly. Every modern forensic suite checks for this first.

**Deleted-inode and deleted-MFT-record reuse.** Writing into the record of a
just-deleted file before the filesystem reuses it. Racy and short-lived.

**Steganography inside file formats.** Hiding a payload in the unused bits of a
JPEG, PNG, or PDF. This has no filesystem component and is a large field of its
own.

**Sparse-file holes.** A sparse file's unwritten ranges read as zeros and occupy
no blocks, so there is nothing to hide in, only a size-versus-allocation
mismatch to notice.

**Snapshot and VSS stashing.** Putting data in a Volume Shadow Copy or an APFS
snapshot and deleting the live copy. This is "hide a normal file where most tools
do not look," not a filesystem-structure technique, and it depends on snapshot
retention you do not control.
