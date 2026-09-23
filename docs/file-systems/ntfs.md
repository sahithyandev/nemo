---
title: NTFS
parent: File Systems
nav_order: 1
---

# Nemo's NTFS parser

This is about `internal/filesystem/ntfs`. It covers detection, mounting, MFT
record parsing, directory traversal, and the in-place named-stream write path.

For what named streams are and how detection works at the technique level,
see [Named-Stream Hiding](../techniques/named-streams.html). For where this
package sits in the overall layering, see [Architecture](../architecture/).

## Detection

`Sniff` (`ntfs.go`) checks whether the image starts with the NTFS OEM ID
string `"NTFS    "` (eight bytes, padded with spaces) at boot-sector offset
3. It handles a signature shorter than 11 bytes without panicking.

## Mounting

`New(img)` reads and validates the boot sector, then defers everything else
to lazy, per-call MFT record reads. There is no up-front tree walk.

**1. Read the boot sector (`bootsector.go`).**

`readBootSector` requires the image to be at least 512 bytes, re-checks the
`"NTFS    "` magic at offset 3, and pulls:

- `bytesPerSector` (offset `0x0b`), validated as a power of two between 256
  and 4096.
- `sectorsPerCluster` (offset `0x0d`), validated as a nonzero power of two.
- `mftCluster` and `mftMirrorCluster` (offsets `0x30` and `0x38`), the
  starting clusters of `$MFT` and `$MFTMirr`.
- `fileRecordSize` and `indexRecordSize` (offsets `0x40` and `0x44`),
  decoded by `decodeFileRecordSize` from NTFS's signed byte encoding: a
  positive value is a cluster count, a negative value `-n` means `1 << n`
  bytes. Zero, an exponent over 31, or a decoded size that doesn't fit a
  `uint32` are all rejected.

**2. Locate MFT records on demand (`mft.go`).**

The first read of any record beyond record 0 triggers `loadMFTBytes` to read
record 0 itself, parse its `$DATA` attribute, and cache the resulting data
runs (`f.mftRuns`) and size (`f.mftDataSize`). `$MFT`'s own `$DATA` must be
non-resident and uncompressed/unencrypted/non-sparse (`attribute.flags ==
0`); anything else is refused. Every later record read resolves its offset
through those cached runs via `readRunsAt`, the same run-walking code used
for stream data.

Every record read goes through `applyFixups`, which validates and reverses
NTFS's per-sector update-sequence protection: the last two bytes of each
512-byte sector are checked against, then replaced from, the update
sequence array named at offset 4 in the record. A mismatch there means the
record was read mid-write or is corrupt, and is a hard error rather than a
silent guess.

## MFT records and attributes (`attribute.go`)

`parseMFTRecord` reads the 48-byte record header (`parseMFTRecordHeader`,
validating `usedSize <= allocatedSize <= len(record)` and a sane first
attribute offset) and then walks attributes from `firstAttributeOffset`
until the `0xFFFFFFFF` end marker.

Each attribute is either resident (value bytes inline in the record) or
non-resident (a run list describing clusters elsewhere), distinguished by a
byte at offset 8 of the attribute header. `parseDataRuns` decodes the
run-list byte stream: each run is a length-then-offset pair of
variable-width signed/unsigned integers, and `LCN` deltas accumulate onto a
running absolute cluster number. VCN coverage, cluster-count overflow, and
a negative absolute LCN are all checked before a run is trusted.

The parser recognizes `$STANDARD_INFORMATION` (`0x10`), `$FILE_NAME`
(`0x30`), the unnamed `$DATA` (`0x80`), `$INDEX_ROOT` (`0x90`),
`$INDEX_ALLOCATION` (`0xA0`), and `$BITMAP` (`0xB0`). `$ATTRIBUTE_LIST`
(`0x20`) is refused outright: a record that needs one to describe itself
(more attributes than fit in one MFT record) is unsupported. A duplicate
unnamed `$DATA` or `$STANDARD_INFORMATION` is also an error, since real NTFS
never produces one.

## Directory traversal (`directory.go`)

A directory's children come from its `$INDEX_ROOT` attribute (always
resident, always keyed on `$FILE_NAME`) plus, for a large directory, its
`$INDEX_ALLOCATION` attribute and `$BITMAP`. `parseIndexHeader` walks the
fixed-size index-entry array in both structures the same way, returning
whether any entry pointed at a child node (`hasChildren`/`INDEX_ENTRY_NODE`).

When the root alone isn't enough, `directoryEntries` reads one `INDX` record
per set bit in the bitmap, applies fixups the same as an MFT record does
(with magic `"INDX"` instead of `"FILE"`), checks the record's stored VCN
against where it was expected to be found, and parses its entries.

A file with more than one directory entry, such as a short 8.3 name plus a
long name, is deduplicated by `preferDirectoryNames`, which keeps the
long-namespace entry (namespace 1 "Win32" or 3 "Win32 & DOS") over the
short-only namespace 2 "DOS" entry.

## Named streams (`namedstream.go`)

An NTFS file's named streams are its named `$DATA` attributes: every
`$DATA` attribute after the first unnamed one. `Entry` implements
`filesystem.NamedStreamCapable`.

`streamRecord` reloads and re-validates the entry's MFT record on every
call rather than caching it, and refuses two cases outright: a record whose
`in-use` flag is clear or whose `baseRecordReference` is nonzero (an
extension record needing `$ATTRIBUTE_LIST` support this parser doesn't
have), and any attribute whose name bytes overlap its own header or its
value/run-list region. `NamedStreams` then lists every named `$DATA`
attribute's name, sorted.

`ReadStream` returns a resident stream's value directly, or, for a
non-resident stream, walks its data runs with the same bounds-checked
`readRunsAt` used for MFT records. A compressed, encrypted, or sparse
stream (checked in `streamFlags`, from the attribute's flags and, for a
non-resident stream, its compression-unit field) is refused rather than
misread.

**Writing.** `WriteStream` builds a fresh resident `$DATA` attribute header,
packs the UTF-16 name and value into it, and calls `repackStream` to splice
it into the record at the position NTFS's ordered-by-type attribute layout
expects, replacing any resident stream of the same name. A `WriteStream`
against an existing non-resident stream instead calls `replaceNonresident`,
which can overwrite the stream's value only within its existing cluster
runs (`capacity` from `streamRuns`): growing a stream past its current
allocation, or creating a brand-new non-resident stream, needs cluster
allocation this parser doesn't do, and returns a clear error instead.
`DeleteStream` refuses a non-resident stream outright, for the same reason:
freeing its clusters needs deallocation support that doesn't exist yet.

Every mutation goes through `prepareStreamRecord` before any byte is
written, which:

- rejects duplicate attribute instance IDs in the record;
- requires 512-byte sectors and a record size that's itself a multiple of
  512 (`bytesPerSector != 512` is refused, since the fixup stride isn't
  inferred for other geometries);
- refuses to modify one of the first few MFT records at all (`recordNumber
  < mirrored`), since those are the ones `$MFTMirr` also covers, and this
  parser has no path to keep the mirror in sync;
- refuses a write whose target cluster run physically overlaps the boot
  region or `$MFTMirr`'s own on-disk bytes, for the same reason;
- regenerates the update-sequence array with a bumped sequence number, then
  applies it to a "protected" copy of the record for the actual write while
  keeping an "expected" copy (with fixups reversed) to verify against.

`writeStreamRecord`/`commitStreamRecord` write the planned physical extents,
then re-read the record and compare it byte-for-byte against the expected
result, failing loudly on any mismatch instead of leaving a record whose
on-disk state doesn't match what was intended.

## Timestomping (`timestomp.go`)

In image mode, `Entry` implements `filesystem.TimestompCapable` and reads
timestamps through `Timestamp`. It updates creation, modification, metadata
change, and access times in resident, unnamed `$STANDARD_INFORMATION` (`0x10`).
Values must fit unsigned FILETIME (100 ns ticks since 1601-01-01) without
rounding. `$FILE_NAME` timestamps, DATA streams, ADS, and all other attribute
bytes are preserved.

The operation holds the same MFT lock as named-stream edits and reuses their
record loading, validation, mapped writes, FILE fixups, and read-back verification.
All validation completes before any image write. Fragmented MFT records are
supported; each physical write passes through the existing custody recorder.
The same geometry and MFT mirror restrictions apply. I/O failures are reported;
a partially completed physical write is not rolled back.

## Slack space (`slack.go`)

In image mode, `Entry` implements `filesystem.SlackSpaceCapable`. Slack is
allocated cluster bytes minus the unnamed DATA attribute's initialized size.
Resident files and directories expose no cluster slack. Fragmented non-resident
DATA is mapped in logical order, with one physical slack region per affected run.
The existing slack technique reads, writes and clears framed payloads through the
custody-wrapped image. Each frame must fit in one physical region.

Before exposing any region, NTFS reloads the MFT record and validates the entire
runlist, allocation sizes, image bounds, and overlaps with metadata or other
attributes in the record. Sparse, compressed, encrypted and ATTRIBUTE_LIST-based
files are refused. No clusters are allocated, and initialized content, file sizes,
runlists and MFT bytes remain unchanged. Bytes between initialized size and logical
EOF are included in slack; NTFS continues to treat that uninitialized range as zeros.
As with named streams, volume-wide allocation ownership is not inferred.

## Limitations

- **`$ATTRIBUTE_LIST`-based records.** Both directory and file records that
  need an attribute list (too many attributes, or too many streams, for one
  MFT record) are refused.
- **Compressed, encrypted, or sparse streams**, for both `$MFT` itself and
  any named stream.
- **New non-resident streams, and growing an existing non-resident stream
  past its current allocation.** No cluster allocation.
- **Deleting a non-resident stream.** No cluster deallocation.
- **Writes to the first few MFT records, or to any run overlapping
  `$MFTMirr`.** Refused rather than leaving the mirror stale.
- **Sector sizes other than 512 bytes**, for the named-stream write path
  specifically (reading tolerates other sector sizes through the general
  fixup code; writing requires 512 to compute the fixup stride).
- **Live mode for NTFS is read-only and Windows-only.** It reuses the image
  parser against a volume opened with `GENERIC_READ`, so detection requires
  administrator access. Live writes return `filesystem.ErrUnsupported` before
  opening a device. Use an offline image for edits; image mode is unchanged.

## Live mode (Windows)

Targets may be absolute local file/directory paths, drive volumes (`\\.\C:`),
or volume GUID paths (`\\?\Volume{GUID}`). Physical drives, UNC paths, relative
paths, stream paths and arbitrary device names are rejected. Local paths are
resolved to their volume GUID, including junctions and nested mount points.
The NTFS boot sector is validated by the existing parser.

Volume size and sector geometry come from Windows device queries. Reads are
sector-aligned and bounded; both the device adapter and image wrappers reject
writes. Caller wrapping happens before parsing and the returned close function
owns the device, following APFS's custody/image lifetime pattern. Parse failures
close it immediately. No volume locks, dismounts or raw writes are performed.

Live reads are not a consistent snapshot: concurrent filesystem changes can
cause inconsistent results or parser errors. The existing parser limitations
apply. Native ADS and timestamp writes are not implemented in this phase.
Non-Windows `OpenLive` calls return `filesystem.ErrUnsupported` without touching
the target; NTFS image mode remains available on all supported hosts.

## Safety against crafted images

- Every size and offset pulled from a boot sector, MFT record header, or
  attribute header is range-checked against the buffer it was read from
  before use, so a crafted value can't drive an out-of-bounds slice.
- `decodeFileRecordSize`'s exponent path is capped at 31 and its result at
  `uint32` range, so a crafted encoded byte can't overflow into a huge
  allocation.
- Data-run decoding checks VCN and cluster-count overflow, and that runs
  cover exactly the `lowestVCN`..`highestVCN` range the attribute header
  claims, before any run is used for I/O.
- `applyFixups` validates the update sequence array's offset and count
  against the record size, and every protected sector's saved bytes against
  the array, before trusting the record's content.
- MFT and stream writes recompute the expected on-disk bytes and read the
  record back to verify them, so a partial or corrupted write is caught
  rather than silently accepted.

## Testing

`bootsector_test.go`, `attribute_test.go`, `mft_test.go`,
`traversal_test.go`, and `namedstream_test.go` build synthetic boot sectors
and MFT records byte by byte, so tests don't depend on an image produced by
a real Windows install and pin down exact on-disk layout assumptions:
fixup geometry, resident vs. non-resident attribute headers, `INDEX_ROOT`
vs. `INDEX_ALLOCATION` traversal, and resident vs. non-resident stream
writes.
