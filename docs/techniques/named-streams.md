---
title: Named-Stream Hiding
parent: How techniques work
nav_order: 1
---

# Named-stream hiding

## What it is

A normal file has one unnamed data stream, which is its contents. Many filesystems also let a file carry extra named chunks of data on the same directory entry.

- NTFS calls them Alternate Data Streams (ADS).
- ext4 and APFS call them extended attributes (xattrs).
- APFS also has the older resource fork.

The file still opens and checksums exactly as before.

Try it:

```sh
# Windows (NTFS)
echo secret > report.txt:notes          # write the "notes" stream of report.txt
more < report.txt:notes                  # read it back
dir /r report.txt                        # the only built-in way to see it

# Linux (ext4)
setfattr -n user.notes -v secret report.txt
getfattr -d report.txt

# macOS (APFS)
xattr -w com.example.notes secret report.txt
xattr -l report.txt
```

## Why it hides

A named stream is not counted in the file's reported size. Most tools that list or measure files never show it.

- `ls -l`, `dir`, Explorer, and Finder show the main stream's size only.
- `du` and `df` do not charge an NTFS stream's clusters to the file. On ext4, a `user.` xattr under about 4 KiB lives in inode or xattr-block metadata, which `du` never counts.
- `sha256sum`, `Get-FileHash`, and `md5` hash the main stream only, so the file verifies clean.
- Antivirus and content scanners once read only the main stream. Most now check ADS. Xattrs are still often skipped.

The payload does not travel with the file unless the transport keeps it. These drop it:

- Copying to FAT32 or exFAT
- Most cloud sync clients
- Email attachments
- `tar` without `--xattrs`
- `cp` without `-p` or `-a`

## Capacity and limits

| Filesystem            | practical per-stream limit                        | privilege                      | survives cross-FS copy | shows in default tooling                              |
| --------------------- | ------------------------------------------------- | ------------------------------ | ---------------------- | ----------------------------------------------------- |
| NTFS ADS              | free volume space (non-resident)                  | none                           | no                     | `dir /r`, PowerShell `Get-Item -Stream`, not Explorer |
| ext4 `user.` xattr    | ~4 KiB (one xattr block, shared with other attrs) | none                           | no                     | `getfattr`, not `ls` or `du`                          |
| ext4 `trusted.` xattr | ~4 KiB                                            | root                           | no                     | root-only `getfattr`                                  |
| APFS xattr            | ~3.7 KiB embedded, larger as a stream             | none for `com.*` or user names | no                     | `xattr -l`, not Finder                                |

On all three, appending to or rewriting the main file leaves the stream alone. Tools that save by writing a temp file and renaming it, as many editors do, drop the stream.

## Detection

- **NTFS:** list every `$DATA` attribute in each MFT record. Any with a non-empty name is an ADS. The Sleuth Kit (`fls -a`, `istat`) and every forensic suite do this. `dir /r` and `Get-Item -Stream *` do it live.
- **ext4:** walk the in-inode xattr area and the `i_file_acl` block of every inode. Examiners look for unexpected `user.` or `trusted.` names, and for values that are high-entropy binary instead of short text.
- **APFS:** list the xattr records in the B-tree. Red flags are `com.apple.*`-looking names that are not real system attributes, and `ResourceFork` xattrs on file types that never have one.

Nemo's [`namedStreamTechnique.Detect`]({{ site.src }}/internal/technique/technique.go#L144) (`internal/technique/technique.go`) lists `entry.NamedStreams()` and reports each with its byte size. It does not judge whether a stream looks suspicious. It reports every non-default stream.

## Traces left behind

- Any tool that parses the metadata record can recover the stream. This technique hides from casual inspection, not from forensics.
- **NTFS:** the `$UsnJrnl` change journal and `$LogFile` record the stream's creation with a timestamp.
- **ext4:** the `jbd2` journal records the inode or xattr-block write. `debugfs` shows the xattrs directly.
- A non-resident NTFS stream or an external ext4 xattr block changes the volume's free-space count and block bitmap.

## Implementation Notes

### NTFS (Alternate Data Streams)

An NTFS file is a record in the Master File Table (MFT). Records are 1024 bytes by default and hold a list of typed attributes. File contents are the `$DATA` attribute (type `0x80`).

The spec allows more than one `$DATA` attribute per record. Every one after the first must have a name. A named `$DATA` attribute is an alternate data stream.[^fscc][^ntfs]

- The API syntax is `file:stream` or `file:stream:$DATA`. A stream named `Zone.Identifier` marks a download as coming from the internet.
- Directories can carry named streams too, next to their `$INDEX_ROOT` and `$INDEX_ALLOCATION` attributes. The syntax is `dir::$DATA` or `dir:stream`.
- A resident stream (up to about 700 bytes) fits inside the MFT record, so no cluster is allocated.
- A non-resident stream gets its own cluster run, listed as data runs in the attribute header. This is how a normal file's contents are stored.
- Stream count and total size are capped only by free space. A non-resident ADS can be gigabytes.

### ext4 (extended attributes)

ext4 stores xattrs in up to two places. Nemo reads and writes both (`internal/filesystem/ext4/xattr.go`).

1. **In the inode.** This is the gap between the fixed 128-byte inode and the end of the inode as the filesystem sizes it, typically 256 bytes. The gap begins at `128 + i_extra_isize`. When it holds xattrs, it starts with the 32-bit magic `0xEA020000` (little-endian, the [`xattrMagic`]({{ site.src }}/internal/filesystem/ext4/xattr.go#L13) constant in [`xattr.go`]({{ site.src }}/internal/filesystem/ext4/xattr.go)). Room here runs from tens to about 150 bytes.
2. **In an external block.** This is one filesystem block (4 KiB by default) pointed to by `i_file_acl` (`raw[104:]` plus a 16-bit high half at `raw[118:]` in the code). It begins with a 32-byte header: magic, `refcount`, block count (must be 1), then a hash.

Identical xattr blocks are deduplicated and reference-counted across inodes. That is why [`storeXattrRegion`]({{ site.src }}/internal/filesystem/ext4/xattr.go#L380) refuses to change a block whose `refcount` is above 1. Doing so would need copy-on-write allocation, which nemo does not do.

Each attribute is an `ext4_xattr_entry`:

| Offset | Size       | Field          | Meaning                                               |
| ------ | ---------- | -------------- | ----------------------------------------------------- |
| 0      | 1          | `e_name_len`   | length of the name (without namespace prefix)         |
| 1      | 1          | `e_name_index` | namespace, see table below                            |
| 2      | 2          | `e_value_offs` | offset of the value within the value area (LE)        |
| 4      | 4          | `e_value_inum` | 0 for inline values; nemo rejects non-zero (ea-inode) |
| 8      | 4          | `e_value_size` | value length in bytes (LE)                            |
| 12     | 4          | `e_hash`       | per-entry hash (external block only)                  |
| 16     | `name_len` | `name`         | not NUL-terminated                                    |

Entries are 4-byte aligned (`(16 + name_len + 3) &^ 3`) and packed from the front. Values are 4-byte aligned and packed from the back. A 4-byte zero ends the entry list.

The namespace prefix is stripped from the stored name and encoded as `e_name_index` (from [`xattrPrefixes`]({{ site.src }}/internal/filesystem/ext4/xattr.go#L22) in [`xattr.go`]({{ site.src }}/internal/filesystem/ext4/xattr.go)):

| Prefix                     | index | privilege to write                           |
| -------------------------- | ----- | -------------------------------------------- |
| `user.`                    | 1     | none, for an ordinary user on a regular file |
| `system.posix_acl_access`  | 2     | ACL tooling                                  |
| `system.posix_acl_default` | 3     | ACL tooling                                  |
| `trusted.`                 | 4     | `CAP_SYS_ADMIN` (root)                       |
| `security.`                | 6     | LSM or system setup only                     |
| `system.`                  | 7     | kernel                                       |
| `system.richacl`           | 8     | ACL tooling                                  |

`user.` is the practical hiding namespace. It needs no privilege and almost no tooling looks at it. `trusted.` is invisible to non-root users, which is stealthier, but it takes root to write.

### APFS (xattrs and the resource fork)

APFS keeps everything, xattrs included, as records in the volume's file-system B-tree (`j_xattr_val_t`).[^apfs] An xattr record's flags say how the value is stored:

- **Embedded** (`XATTR_DATA_EMBEDDED`): the bytes sit inline in the B-tree record, up to `XATTR_MAX_EMBEDDED_SIZE` (3804 bytes).
- **Stream** (`XATTR_DATA_STREAM`): for larger values. The record holds a `j_xattr_dstream_t` that points at a regular data stream with its own extents.

The resource fork is the xattr named `com.apple.ResourceFork` (the `XATTR_RESOURCEFORK_NAME` constant). It once held a structured resource map. For hiding, it is an opaque blob like any other xattr. `xattr -l` lists both.

### In nemo

[`filesystem.NamedStreamCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L39) (`internal/filesystem/filesystem.go`):

```go
WriteStream(name string, data []byte) error
ReadStream(name string) ([]byte, error)
DeleteStream(name string) error
```

[`namedStreamTechnique`]({{ site.src }}/internal/technique/technique.go#L126) in `internal/technique/technique.go` requires a `--stream-name`. It calls [`WriteStream`]({{ site.src }}/internal/filesystem/filesystem.go#L40) for `hide`, lists [`NamedStreams()`]({{ site.src }}/internal/filesystem/filesystem.go#L35) plus
[`ReadStream`]({{ site.src }}/internal/filesystem/filesystem.go#L41) for `detect`, and [`DeleteStream`]({{ site.src }}/internal/filesystem/filesystem.go#L42) for `clear`.

**ext4.** `internal/filesystem/ext4/ext4.go` ([`NamedStreams`]({{ site.src }}/internal/filesystem/filesystem.go#L35),
[`ReadStream`]({{ site.src }}/internal/filesystem/filesystem.go#L41), [`WriteStream`]({{ site.src }}/internal/filesystem/filesystem.go#L40), [`DeleteStream`]({{ site.src }}/internal/filesystem/filesystem.go#L42)) delegates to [`xattr.go`]({{ site.src }}/internal/filesystem/ext4/xattr.go). Where a write goes:

1. Update in place if the name already exists.
2. Otherwise pack it into the in-inode area.
3. Otherwise use the external block.

It refuses shared blocks (`refcount > 1`) and refuses to empty an external block. Both need block allocation or deallocation, which nemo does not implement.

**APFS.** `internal/filesystem/apfs/namedstream.go` reads `j_xattr` records from the volume's B-tree. For stream-backed values it follows the data stream's file extents.

A write rewrites the affected B-tree leaf in place at its existing address and recomputes its Fletcher-64 checksum. No block is allocated and no checkpoint is written. The volume stays mountable and `fsck_apfs -n` passes, but this is not a real copy-on-write transaction.

That sets the limits on a write:

- An embedded value can be replaced with anything up to 3804 bytes.
- A stream-backed value can be overwritten with anything that fits its existing extents.
- A value that would need to grow past either limit fails with a clear error.
- Deleting a stream-backed xattr drops the record but leaves its extents allocated.

**NTFS.** `internal/filesystem/ntfs/namedstream.go` reads and writes ADS by editing the MFT record directly.

- [`NamedStreams`]({{ site.src }}/internal/filesystem/filesystem.go#L35) lists every named `$DATA` attribute.
- [`ReadStream`]({{ site.src }}/internal/filesystem/filesystem.go#L41) returns a resident value or follows non-resident data runs.
- [`WriteStream`]({{ site.src }}/internal/filesystem/filesystem.go#L40) and [`DeleteStream`]({{ site.src }}/internal/filesystem/filesystem.go#L42) repack the record in place.

A resident stream can be replaced or created if it fits the record's free space. A non-resident stream can only be overwritten within its existing cluster runs, because nemo has no run allocation.

Every write is verified by reading the record back. Nemo refuses a write to one of the first few MFT records, or to a data run that overlaps `$MFTMirr`'s on-disk bytes. Otherwise the mirror would go stale. See [NTFS](../file-systems/ntfs.html#named-streams) for the full write path.

**macOS live mode.** APFS also has a live path (`internal/filesystem/apfs/live_darwin.go`). A named stream there is an xattr syscall on the target's real path, resource fork included (as `com.apple.ResourceFork`). It needs no volume parsing and no elevated privilege. See [APFS live mode](../file-systems/apfs.html#live-mode-macos).

When adding a filesystem, mirror [`fakefs.Entry`]({{ site.src }}/internal/filesystem/fakefs/fakefs.go#L18) (`internal/filesystem/fakefs/fakefs.go`). That test double already implements all three capabilities. The technique layer needs no changes.

## References

[^fscc]: Microsoft, [\[MS-FSCC\]: File System Control Codes](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fscc/), §2.4.4.

[^ntfs]: Russon and Fledel, [NTFS Documentation](https://flatcap.github.io/linux-ntfs/ntfs/), on-disk layout.

[^apfs]: Apple, [_Apple File System Reference_](https://developer.apple.com/support/downloads/Apple-File-System-Reference.pdf), "Extended Fields" and `j_xattr_val_t`.
