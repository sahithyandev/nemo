# Named-Stream Hiding

## What it is

A normal file has one nameless data stream, which is its contents. Every
filesystem nemo targets also lets a file carry extra, named chunks of data
attached to the same directory entry. NTFS calls them Alternate Data Streams
(ADS). ext4 and APFS call them extended attributes (xattrs), and APFS also has
the older resource fork. In each case the extra data hangs off a file that still
opens and checksums exactly as before.

Try it now:

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

The named stream is not counted in the file's reported size, so most tools that
list or measure files never show it:

- `ls -l`, `dir`, Explorer, and Finder show the main stream's size only.
- `du` and `df` do not attribute an NTFS stream's clusters to the file. On ext4 a
  `user.` xattr under about 4 KiB lives in inode or xattr-block metadata that
  `du` never counts.
- `sha256sum`, `Get-FileHash`, and `md5` hash the main stream only, so the file
  verifies clean.
- Antivirus and content scanners historically read the main stream only. Most now
  check ADS; xattrs are still commonly skipped.

The payload does not travel with the file unless the transport preserves it.
Copying to FAT32 or exFAT drops it, as does uploading through most cloud sync
clients, emailing as an attachment, `tar` without `--xattrs`, or `cp` without
`-p` or `-a`.

## On-disk mechanics

### NTFS: Alternate Data Streams

An NTFS file is a record in the Master File Table (MFT), 1024 bytes by default,
holding a sequence of typed attributes. File contents are the `$DATA` attribute
(type `0x80`). The spec allows more than one `$DATA` attribute per record, and
every `$DATA` after the first must have a non-empty name. That named `$DATA`
attribute is the alternate data stream. Reference: `[MS-FSCC]` §2.4.4 and the
NTFS on-disk layout documentation.

- The naming syntax at the API is `file:stream` (and `file:stream:$DATA`). A
  stream named `Zone.Identifier` is what marks a download as coming from the
  internet.
- Directories can carry named streams too, alongside their `$INDEX_ROOT` and
  `$INDEX_ALLOCATION` attributes: `dir::$DATA` or `dir:stream`.
- A resident stream (value up to about 700 bytes) fits inside the MFT record
  itself, with no cluster allocated. A non-resident stream gets its own cluster
  run, described by a data-run list in the attribute header, exactly like a
  normal file's contents.
- There is no per-file cap on stream count or total size beyond free space on the
  volume. A non-resident ADS can be gigabytes.

### ext4: extended attributes

ext4 stores xattrs in up to two places, and nemo reads and writes both
(`internal/filesystem/ext4/xattr.go`):

1. In-inode, in the gap between the end of the fixed 128-byte inode and the end
   of the inode as sized by the filesystem, which is typically 256 bytes. The gap
   begins at `128 + i_extra_isize` and, when it holds xattrs, starts with the
   32-bit magic `0xEA020000` (little-endian; the `xattrMagic` constant in
   `xattr.go`). Room here runs from tens to about 150 bytes.
2. An external xattr block, one filesystem block (4 KiB by default) pointed to by
   `i_file_acl` (`raw[104:]` plus a 16-bit high half at `raw[118:]` in the code).
   It begins with a 32-byte header: magic, `refcount`, block count (must be 1),
   then a hash. Identical xattr blocks get deduplicated and reference-counted
   across inodes, which is why `storeXattrRegion` refuses to mutate a block whose
   `refcount` is above 1. That case needs copy-on-write allocation nemo does not
   do yet.

Each attribute is an `ext4_xattr_entry`:

```
offset  size  field
  0      1    e_name_len       length of the name (without namespace prefix)
  1      1    e_name_index     namespace, see table below
  2      2    e_value_offs     offset of the value within the value area (LE)
  4      4    e_value_inum     0 for inline values; nemo rejects non-zero (ea-inode)
  8      4    e_value_size     value length in bytes (LE)
 12      4    e_hash           per-entry hash (external block only)
 16    name_len  name          not NUL-terminated
```

Entries are 4-byte aligned (`(16 + name_len + 3) &^ 3`) and packed from the
front. Values are 4-byte aligned and packed from the back. A 4-byte zero
terminates the entry list. The namespace prefix is stripped from the stored name
and encoded as `e_name_index` (from `xattrPrefixes` in `xattr.go`):

| Prefix       | index | privilege to write |
| ------------ | ----- | ------------------ |
| `user.`      | 1     | none, for an ordinary user on a regular file |
| `system.posix_acl_access` | 2 | ACL tooling |
| `system.posix_acl_default` | 3 | ACL tooling |
| `trusted.`   | 4     | `CAP_SYS_ADMIN` (root) |
| `security.`  | 6     | LSM or system setup only |
| `system.`    | 7     | kernel |
| `system.richacl` | 8 | ACL tooling |

`user.` is the practical hiding namespace: no privilege needed, ignored by
almost all tooling. `trusted.` is invisible to non-root users, which is stealthier
but needs root to plant.

### APFS: xattrs and the resource fork

APFS keeps everything, xattrs included, as records in the volume's file-system
B-tree (Apple's *APFS Reference*, "Extended Fields" and `j_xattr_val_t`). An
xattr record's flags say whether the value is:

- embedded (`XATTR_DATA_EMBEDDED`), with the value bytes stored inline in the
  B-tree record, up to `XATTR_MAX_EMBEDDED_SIZE` (3804 bytes); or
- a stream (`XATTR_DATA_STREAM`), where the record holds a `j_xattr_dstream_t`
  pointing at a regular data stream with its own extents, for values larger than
  that.

The resource fork is the xattr named `com.apple.ResourceFork` (the
`XATTR_RESOURCEFORK_NAME` constant), which historically held a structured
resource map. For hiding it is an opaque blob like any other xattr. `xattr -l`
lists both.

## Capacity and limits

| Filesystem | practical per-stream limit | privilege | survives cross-FS copy | shows in default tooling |
| ---------- | ------------------------- | --------- | ---------------------- | ----------------------- |
| NTFS ADS   | free volume space (non-resident) | none | no | `dir /r`, PowerShell `Get-Item -Stream`, not Explorer |
| ext4 `user.` xattr | ~4 KiB (one xattr block, shared with other attrs) | none | no | `getfattr`, not `ls` or `du` |
| ext4 `trusted.` xattr | ~4 KiB | root | no | root-only `getfattr` |
| APFS xattr | ~3.7 KiB embedded, larger as a stream | none for `com.*` or user names | no | `xattr -l`, not Finder |

On all three, a hostile append or rewrite of the main file leaves the stream
alone, but tooling that rewrites the file by create-temp-then-rename (many
editors) drops it.

## Detection

- NTFS: enumerate every `$DATA` attribute in each MFT record. Any with a non-empty
  name is an ADS. The Sleuth Kit (`fls -a`, `istat`) and every forensic suite do
  this; `dir /r` and `Get-Item -Stream *` do it live.
- ext4: walk the in-inode xattr area and the `i_file_acl` block for every inode.
  Unexpected `user.` or `trusted.` names, or values that are high-entropy binary
  rather than short text, are what an examiner keys on.
- APFS: enumerate xattr records in the B-tree. Large `com.apple.*`-looking names
  that are not real system attributes, or `ResourceFork` xattrs on file types
  that never have one, are the flags.

nemo's `namedStreamTechnique.Detect` (`internal/technique/technique.go`) lists
`entry.NamedStreams()` and reports each with its byte size. It does not judge
whether a stream is suspicious; it reports every non-default stream it finds.

## Traces left behind

- The stream itself is recoverable by any tool that parses the metadata record.
  This technique hides from casual inspection, not from forensics.
- NTFS: the `$UsnJrnl` change journal and `$LogFile` record the stream's creation
  with a timestamp.
- ext4: the `jbd2` journal records the inode or xattr-block write, and `debugfs`
  shows the xattrs directly.
- Allocating a non-resident NTFS stream or an external ext4 xattr block changes
  the volume free-space count and the block bitmap.

## How nemo implements it

`filesystem.NamedStreamCapable` (`internal/filesystem/filesystem.go`):

```go
WriteStream(name string, data []byte) error
ReadStream(name string) ([]byte, error)
DeleteStream(name string) error
```

`namedStreamTechnique` in `internal/technique/technique.go` requires a
`--stream-name`. It calls `WriteStream` for `hide`, lists `NamedStreams()` plus
`ReadStream` for `detect`, and `DeleteStream` for `clear`.

- ext4 is implemented. `internal/filesystem/ext4/ext4.go` (`NamedStreams`,
  `ReadStream`, `WriteStream`, `DeleteStream`) delegates to `xattr.go`. Write
  placement policy: update in place if the name exists, else pack into the
  in-inode area, else use the external block. It refuses shared blocks
  (`refcount > 1`) and refuses to empty an external block, since both need block
  allocation or deallocation that is not built yet.
- APFS is pending. The parser exists, but `apfs.Entry.NamedStreams` returns
  `nil, nil` as a stub (`internal/filesystem/apfs/apfs.go`). Implementing it
  means reading `j_xattr` records from the B-tree and adding the three methods.
- NTFS is pending. There is no `internal/filesystem/ntfs/` package yet. See
  `docs/architecture.md` for the intended file breakdown (`ntfs.go`, `mft.go`,
  `namedstream.go`, and the rest).

When adding a filesystem, mirror `fakefs.Entry`
(`internal/filesystem/fakefs/fakefs.go`), the test double that already
implements all three capabilities. The technique layer needs no changes.
