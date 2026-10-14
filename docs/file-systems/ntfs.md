---
title: NTFS
parent: File Systems
nav_order: 1
---

# Nemo's NTFS parser

This page covers `internal/filesystem/ntfs`: detection, mounting, MFT record parsing, directory traversal, and the in-place named-stream write path.

- For what named streams are and how detection works at the technique level, see [named-stream hiding](../techniques/named-streams.html).
- For where this package sits in the layering, see [Architecture](../architecture/).

## At a glance

- Image mode supports named streams, slack space, and timestomp.
- Live mode is read-only and Windows-only.
- Writes are in place. Nemo cannot allocate or free clusters, so it cannot grow a non-resident stream.
- It refuses `$ATTRIBUTE_LIST` records and compressed, encrypted, or sparse data. See [Limitations](#limitations).

## Detection

[`Sniff`]({{ site.src }}/internal/filesystem/ntfs/ntfs.go#L28) in [`ntfs.go`]({{ site.src }}/internal/filesystem/ntfs/ntfs.go) checks for the NTFS OEM ID string `"NTFS    "` (eight bytes, padded with spaces) at boot-sector offset 3. It handles a signature shorter than 11 bytes without panicking.

## Mounting

[`New(img)`]({{ site.src }}/internal/filesystem/ntfs/ntfs.go#L144) reads and validates the boot sector. Everything else waits for lazy, per-call MFT record reads. There is no up-front tree walk.

### 1. Read the boot sector

The code is in [`bootsector.go`]({{ site.src }}/internal/filesystem/ntfs/bootsector.go). [`readBootSector`]({{ site.src }}/internal/filesystem/ntfs/bootsector.go#L23) requires the image to be at least 512 bytes and re-checks the `"NTFS    "` magic at offset 3. Then it reads:

- `bytesPerSector` (offset `0x0b`): must be a power of two between 256 and 4096.
- `sectorsPerCluster` (offset `0x0d`): must be a non-zero power of two.
- `mftCluster` and `mftMirrorCluster` (offsets `0x30` and `0x38`): the starting clusters of `$MFT` and `$MFTMirr`.
- `fileRecordSize` and `indexRecordSize` (offsets `0x40` and `0x44`): decoded by [`decodeFileRecordSize`]({{ site.src }}/internal/filesystem/ntfs/bootsector.go#L71) from NTFS's signed byte encoding.

In that encoding, a positive value is a cluster count. A negative value `-n` means `1 << n` bytes. Zero, an exponent over 31, or a size that does not fit a `uint32` is rejected.

### 2. Locate MFT records on demand

The code is in [`mft.go`]({{ site.src }}/internal/filesystem/ntfs/mft.go).

The first read of any record past record 0 calls [`loadMFTBytes`]({{ site.src }}/internal/filesystem/ntfs/mft.go#L82). It reads record 0, parses its `$DATA` attribute, and caches the data runs (`f.mftRuns`) and size (`f.mftDataSize`).

`$MFT`'s own `$DATA` must be non-resident, uncompressed, unencrypted, and non-sparse (`attribute.flags == 0`). Anything else is refused.

Every later record read finds its offset through those cached runs with [`readRunsAt`]({{ site.src }}/internal/filesystem/ntfs/mft.go#L158). That is the same run-walking code used for stream data.

### 3. Apply fixups

Every record read goes through [`applyFixups`]({{ site.src }}/internal/filesystem/ntfs/mft.go#L107). It validates and reverses NTFS's per-sector update-sequence protection.

- The last two bytes of each 512-byte sector are checked against the update sequence array named at offset 4 in the record.
- Then they are replaced from that array.
- A mismatch means the record was read mid-write or is corrupt. It is a hard error, not a guess.

## MFT records and attributes (`attribute.go`)

[`parseMFTRecord`]({{ site.src }}/internal/filesystem/ntfs/attribute.go#L94) reads the 48-byte record header. [`parseMFTRecordHeader`]({{ site.src }}/internal/filesystem/ntfs/attribute.go#L200) validates `usedSize <= allocatedSize <= len(record)` and a sane first attribute offset. Then the parser walks attributes from `firstAttributeOffset` until the `0xFFFFFFFF` end marker.

Each attribute is resident (value bytes inline in the record) or non-resident (a run list describing clusters elsewhere). A byte at offset 8 of the attribute header says which.

[`parseDataRuns`]({{ site.src }}/internal/filesystem/ntfs/attribute.go#L283) decodes the run-list byte stream. Each run is a length-then-offset pair of variable-width integers, and `LCN` deltas add up to a running absolute cluster number. Before a run is trusted, nemo checks VCN coverage, cluster-count overflow, and a negative absolute LCN.

The parser recognizes these attributes:

| Attribute | Type |
| --------- | ---- |
| `$STANDARD_INFORMATION` | `0x10` |
| `$FILE_NAME` | `0x30` |
| unnamed `$DATA` | `0x80` |
| `$INDEX_ROOT` | `0x90` |
| `$INDEX_ALLOCATION` | `0xA0` |
| `$BITMAP` | `0xB0` |

It refuses `$ATTRIBUTE_LIST` (`0x20`) outright. A record that needs one to describe itself, because it has more attributes than fit in one MFT record, is unsupported. A duplicate unnamed `$DATA` or `$STANDARD_INFORMATION` is also an error, since real NTFS never produces one.

## Directory traversal (`directory.go`)

A directory's children come from its `$INDEX_ROOT` attribute. That attribute is always resident and keyed on `$FILE_NAME`. A large directory also has an `$INDEX_ALLOCATION` attribute and a `$BITMAP`.

[`parseIndexHeader`]({{ site.src }}/internal/filesystem/ntfs/directory.go#L105) walks the fixed-size index-entry array the same way in both structures. It reports whether any entry pointed at a child node (`hasChildren`, `INDEX_ENTRY_NODE`).

When the root is not enough, [`directoryEntries`]({{ site.src }}/internal/filesystem/ntfs/directory.go#L22) reads one `INDX` record per set bit in the bitmap. For each record it:

- applies fixups as for an MFT record, with magic `"INDX"` instead of `"FILE"`
- checks that the record's stored VCN matches where it was expected
- parses its entries

A file can have more than one directory entry, such as a short 8.3 name plus a long name. [`preferDirectoryNames`]({{ site.src }}/internal/filesystem/ntfs/directory.go#L147) removes the duplicate. It keeps the long-namespace entry (namespace 1 "Win32" or 3 "Win32 & DOS") over the short-only namespace 2 "DOS" entry.

## Named streams

The code is in `namedstream.go`. A file's named streams are its named `$DATA` attributes, which means every `$DATA` after the first unnamed one. [`Entry`]({{ site.src }}/internal/filesystem/ntfs/ntfs.go#L92) implements [`filesystem.NamedStreamCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L39).

### Reading

[`streamRecord`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L35) reloads and re-validates the entry's MFT record on every call. It does not cache it. It refuses two cases:

- A record whose `in-use` flag is clear, or whose `baseRecordReference` is non-zero. That is an extension record, which needs `$ATTRIBUTE_LIST` support this parser does not have.
- Any attribute whose name bytes overlap its own header or its value or run-list region.

[`NamedStreams`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L74) lists every named `$DATA` attribute's name, sorted.

[`ReadStream`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L123) returns a resident stream's value directly. For a non-resident stream it walks the data runs with the same bounds-checked [`readRunsAt`]({{ site.src }}/internal/filesystem/ntfs/mft.go#L158) used for MFT records.

A compressed, encrypted, or sparse stream is refused, not misread. [`streamFlags`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L109) checks the attribute's flags and, for a non-resident stream, its compression-unit field.

### Writing

[`WriteStream`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L161) builds a fresh resident `$DATA` attribute header and packs the UTF-16 name and value into it. Then [`repackStream`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L266) splices it into the record where NTFS's type-ordered attribute layout expects it. It replaces any resident stream of the same name.

For an existing non-resident stream, [`WriteStream`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L161) calls [`replaceNonresident`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L479). That can overwrite the value only within the stream's existing cluster runs (`capacity` from [`streamRuns`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L296)).

Two cases need cluster allocation, which this parser does not do. Growing a stream past its allocation, and creating a new non-resident stream, both return a clear error.

[`DeleteStream`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L228) refuses a non-resident stream, because freeing its clusters needs deallocation support that does not exist.

### Checks before every change

Every change goes through [`prepareStreamRecord`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L396) before any byte is written. It:

- rejects duplicate attribute instance IDs in the record
- requires 512-byte sectors and a record size that is a multiple of 512 (`bytesPerSector != 512` is refused, because the fixup stride is not inferred for other geometries)
- refuses to modify one of the first few MFT records (`recordNumber < mirrored`). Those are the ones `$MFTMirr` also covers, and this parser cannot keep the mirror in sync.
- refuses a write whose target cluster run overlaps the boot region or `$MFTMirr`'s own on-disk bytes, for the same reason
- regenerates the update-sequence array with a bumped sequence number. It applies that to a "protected" copy of the record for the actual write, and keeps an "expected" copy (with fixups reversed) to verify against.

[`writeStreamRecord`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L457) and [`commitStreamRecord`]({{ site.src }}/internal/filesystem/ntfs/namedstream.go#L465) write the planned physical extents. Then they re-read the record and compare it byte for byte with the expected result. Any mismatch fails loudly, so a record never ends up in a state that differs from what was intended.

## Timestomping (`timestomp.go`)

In image mode, [`Entry`]({{ site.src }}/internal/filesystem/ntfs/ntfs.go#L92) implements [`filesystem.TimestompCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L81). It reads timestamps through [`Timestamp`]({{ site.src }}/internal/filesystem/ntfs/timestomp.go#L91).

It updates the creation, modification, metadata change, and access times in the resident, unnamed `$STANDARD_INFORMATION` (`0x10`). Values must fit an unsigned FILETIME (100 ns ticks since 1601-01-01) without rounding. `$FILE_NAME` timestamps, DATA streams, ADS, and all other attribute bytes are preserved.

The operation holds the same MFT lock as named-stream edits. It reuses their record loading, validation, mapped writes, FILE fixups, and read-back verification.

- All validation finishes before any image write.
- Fragmented MFT records are supported. Each physical write passes through the existing custody recorder.
- The same geometry and MFT mirror restrictions apply.
- I/O failures are reported. A partly completed physical write is not rolled back.

## Slack space (`slack.go`)

In image mode, [`Entry`]({{ site.src }}/internal/filesystem/ntfs/ntfs.go#L92) implements [`filesystem.SlackSpaceCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L52).

Slack is the allocated cluster bytes minus the unnamed DATA attribute's initialized size. Resident files and directories expose no cluster slack. Fragmented non-resident DATA is mapped in logical order, with one physical slack region per affected run.

The slack technique reads, writes, and clears framed payloads through the custody-wrapped image. Each frame must fit in one physical region.

Before exposing a region, NTFS reloads the MFT record and validates:

- the entire runlist
- allocation sizes
- image bounds
- overlaps with metadata or other attributes in the record

Sparse, compressed, encrypted, and `ATTRIBUTE_LIST`-based files are refused.

Nothing else changes. No clusters are allocated, and initialized content, file sizes, runlists, and MFT bytes stay as they were.

Bytes between the initialized size and the logical EOF count as slack. NTFS keeps treating that uninitialized range as zeros. As with named streams, nemo does not infer volume-wide allocation ownership.

## Limitations

- **`$ATTRIBUTE_LIST`-based records.** Directory and file records that need an attribute list are refused. That means too many attributes or streams for one MFT record.
- **Compressed, encrypted, or sparse streams.** This covers `$MFT` itself and any named stream.
- **New non-resident streams, or growing one past its allocation.** Nemo has no cluster allocation.
- **Deleting a non-resident stream.** Nemo has no cluster deallocation.
- **Writes to the first few MFT records, or to any run overlapping `$MFTMirr`.** They are refused so the mirror does not go stale.
- **Sector sizes other than 512 bytes, for the named-stream write path.** Reading tolerates other sizes through the general fixup code. Writing needs 512 to compute the fixup stride.
- **Writable live mode.** NTFS live mode is read-only and Windows-only. It reuses the image parser on a volume opened with `GENERIC_READ`, so detection needs administrator access. Live writes return [`filesystem.ErrUnsupported`]({{ site.src }}/internal/filesystem/filesystem.go#L11) before any device is opened. Use an offline image for edits. Image mode is unchanged.

## Live mode (Windows)

Accepted targets:

- absolute local file or directory paths
- drive volumes (`\\.\C:`)
- volume GUID paths (`\\?\Volume{GUID}`)

Rejected targets: physical drives, UNC paths, relative paths, stream paths, and arbitrary device names.

Local paths resolve to their volume GUID, including junctions and nested mount points. The existing parser validates the NTFS boot sector.

- Volume size and sector geometry come from Windows device queries.
- Reads are sector-aligned and bounded. Both the device adapter and the image wrappers reject writes.
- The caller wraps the device before parsing, and the returned close function owns it. This follows APFS's custody and image lifetime pattern. A parse failure closes it immediately.
- Nemo takes no volume locks, does no dismounts, and does no raw writes.

Live reads are not a consistent snapshot. Concurrent filesystem changes can cause inconsistent results or parser errors. The existing parser limitations apply. Native ADS and timestamp writes are not implemented.

On non-Windows hosts, [`OpenLive`]({{ site.src }}/internal/filesystem/ntfs/live_unsupported.go#L13) returns [`filesystem.ErrUnsupported`]({{ site.src }}/internal/filesystem/filesystem.go#L11) without touching the target. NTFS image mode works on every supported host.

## Safety against crafted images

- Every size and offset read from a boot sector, MFT record header, or attribute header is range-checked against its buffer before use. A crafted value cannot cause an out-of-bounds slice.
- [`decodeFileRecordSize`]({{ site.src }}/internal/filesystem/ntfs/bootsector.go#L71) caps the exponent at 31 and the result at `uint32` range. A crafted byte cannot overflow into a huge allocation.
- Data-run decoding checks VCN and cluster-count overflow. It also checks that runs cover exactly the `lowestVCN` to `highestVCN` range the attribute header claims, before any run is used for I/O.
- [`applyFixups`]({{ site.src }}/internal/filesystem/ntfs/mft.go#L107) validates the update sequence array's offset and count against the record size. It also checks every protected sector's saved bytes against the array, before trusting the record.
- MFT and stream writes recompute the expected on-disk bytes and read the record back to verify them. A partial or corrupted write is caught, not silently accepted.

## Testing

These tests build synthetic boot sectors and MFT records byte by byte:

- `bootsector_test.go`
- `attribute_test.go`
- `mft_test.go`
- `traversal_test.go`
- `namedstream_test.go`

They do not depend on an image from a real Windows install. They pin down the exact on-disk layout assumptions: fixup geometry, resident and non-resident attribute headers, `INDEX_ROOT` and `INDEX_ALLOCATION` traversal, and resident and non-resident stream writes.
