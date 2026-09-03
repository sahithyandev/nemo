# Timestomping

## What it is

Timestomping is rewriting a file's timestamps so it looks older or newer than it
is, or contemporaneous with other files. It defeats timeline analysis, which is
the technique an examiner reaches for first. Unlike named-stream and slack-space
hiding, nothing gets stored. An existing field is overwritten with a chosen
value.

```sh
# Linux (ext4): set mtime and atime
touch -d '2019-01-01T00:00:00Z' report.txt

# macOS
SetFile -m 01/01/2019 00:00:00 report.txt   # from the Xcode command-line tools

# Windows (PowerShell)
(Get-Item report.txt).LastWriteTime = '2019-01-01'
```

## The MACB timestamps, and why they are not one thing

An examiner reconstructs "what happened when" from four timestamps, remembered as
MACB:

| Letter | Meaning | NTFS | ext4 | APFS |
| ------ | ------- | ---- | ---- | ---- |
| M | content last modified | `$SI` and `$FN` MtimeMs | `i_mtime` | `mod_time` |
| A | content last accessed | `$SI` and `$FN` AtimeMs | `i_atime` | `access_time` |
| C | metadata last changed | `$SI` and `$FN` CtimeMs (MFT change) | `i_ctime` | `change_time` (`ctime`) |
| B | born, or created | `$SI` and `$FN` CrtimeMs | `i_crtime` | `create_time` |

Each filesystem stores these differently, at different precision, in different
structures. Some fields cannot be set through any normal API at all. That
asymmetry is what makes timestomping detectable.

## On-disk mechanics

### NTFS: the `$SI` and `$FN` pair

Every MFT record carries the four timestamps twice:

- `$STANDARD_INFORMATION` (`$SI`, attribute `0x10`) is the set that Explorer,
  `dir`, and the Win32 API (`GetFileTime` and `SetFileTime`) read and write.
- `$FILE_NAME` (`$FN`, attribute `0x30`) is a second copy the kernel updates on
  rename or move. It is not writable through the Win32 API at all.

All eight values are Windows FILETIME: a 64-bit count of 100-nanosecond intervals
since 1601-01-01 UTC. Reference: `[MS-FSCC]` §2.4.7 and §2.4.4.

What an examiner checks:

- `$SI` against `$FN`. A user-space timestomp tool sets `$SI` and leaves `$FN`
  alone, so `$FN` still shows the real creation time. Forensic suites flag the
  mismatch automatically.
- Sub-second digits. Real timestamps have noise in the low digits;
  `2019-01-01T00:00:00.0000000`, with every 100 ns sub-second bit zero, shows a
  value that was typed rather than recorded. Many tools set only whole seconds.
- Ordering. M earlier than B, or a timestamp before the volume was formatted.

### ext4: split second and nanosecond fields

The 128-byte "good old" inode holds `i_atime`, `i_mtime`, and `i_ctime` as
32-bit signed seconds since 1970. Larger inodes, typically 256 bytes, add two
things in the extra area past `i_extra_isize`:

- `i_crtime` (creation), a 32-bit seconds field at inode offset 144.
- `i_{mtime,atime,ctime,crtime}_extra`, 32-bit fields packing nanoseconds in the
  upper 30 bits and a 2-bit epoch extension in the low bits, which pushes the
  range past 2038.

`internal/filesystem/ext4/timestomp.go` implements exactly this.
`encodeExt4Timestamp` splits a `time.Time` into `(low uint32, extra uint32)` with
`extra = nanoseconds<<2 | epoch`. `timestampLayout` maps `created`, `modified`,
and `accessed` to offsets 144, 16, and 8, and reports whether the inode is large
enough to carry the `_extra` field, and whether it carries `i_crtime` at all.
Offsets from the code:

```
i_atime            8
i_mtime           16
i_extra_isize    128   (16-bit; xattr magic 0xEA020000 may follow)
i_mtime_extra    136
i_atime_extra    140
i_crtime         144
i_crtime_extra   148
```

What an examiner checks: `i_ctime` cannot be set by any userland API. The kernel
stamps it `now` on every inode change, including the write that stomps
`i_mtime`. So `mtime` far in the past while `ctime` reads "a few seconds ago" is
the ext4 timestomp signature. nemo can write `i_ctime`, because it edits the
inode bytes directly in an image, which is a capability a live attacker on a
mounted filesystem does not have.

### APFS: nanosecond fields in the inode record

APFS stores the four timestamps in the `j_inode_val_t` inode record in the
filesystem B-tree, each a 64-bit count of nanoseconds since 1970-01-01 UTC
(Apple's *APFS Reference*, `j_inode_val_t`: `create_time`, `mod_time`,
`change_time`, `access_time`). The precision is higher than NTFS or ext4, so a
whole-second value is an even stronger sign a human typed it.

APFS also keeps copies of the dates that a timestomp does not reach:

- Spotlight metadata (`.Spotlight-V100`) indexes
  `kMDItemContentModificationDate`, `kMDItemDateAdded`, and others from when the
  file was indexed.
- `com.apple.metadata:*` xattrs, for example `_kMDItemUserTags` and quarantine
  (`com.apple.quarantine`), carry their own dates.
- Local `tmutil` snapshots preserve the inode record as it was.

## Detection by a third party

Timestomping is among the more detectable techniques, because the information
exists in more than one place:

- NTFS: cross-check `$SI` against `$FN`, check sub-second precision, and parse the
  `$LogFile` transaction log and `$UsnJrnl:$J` change journal, which record the
  real times of operations independently of the file's own fields.
- ext4: compare `ctime` against `mtime` and `atime`. The `jbd2` journal records
  the actual write times of recent metadata changes.
- APFS: compare against snapshot copies and Spotlight. `fsck_apfs` and forensic
  tools diff the inode record against snapshot deltas.
- Everywhere: external corroboration such as application logs, `$Recycle.Bin` and
  `.Trash` records, Prefetch (`.pf` file mtimes), browser history, backup
  catalogs, and MRU registry keys. Timestomping the file touches none of these.

## Why nemo's `detect` reports nothing for timestomp

`filesystem.TimestompCapable` exposes only `SetTimestamp`, with no reader:

```go
type TimestompCapable interface {
    SetTimestamp(field TimeField, t time.Time) error
}
```

So `timestompTechnique.Detect` (`internal/technique/technique.go`) always returns
`nil, nil`, after confirming the filesystem supports the capability at all.

This is a real limitation, not just an unfinished feature, and the reason is
specific: reading a timestamp back tells you its current value, not whether it
was altered. Detecting a stomp needs a second, independent source to disagree
with, such as the `$FN` copy, the `ctime` the kernel controls, a journal entry,
or a snapshot. nemo does not parse any of those yet, so it has nothing to compare
against and cannot honestly report a finding. Giving `TimestompCapable` a reader
plus a cross-check source, starting with NTFS `$SI` and `$FN`, is the follow-up
`docs/architecture.md` records against CORE-04.

The same gap constrains `clear`. With no way to recover a prior value, `nemo
clear --technique timestomp` requires the original timestamp supplied explicitly
and errors on a zero value (`timestompTechnique.Clear`). There is no manifest
path for timestomp the way there is for slack-space, because `hide` has no
original value to record.

`internal/filesystem/ext4/timestomp.go` already implements `Timestamp` (a reader)
and `SupportsTimestamp` for its own use. They are just not part of the shared
capability interface yet, so the technique layer cannot call them. Wiring a
reader into `TimestompCapable` is step one of that follow-up.

## How nemo implements it

`timestompTechnique` (`internal/technique/technique.go`):

- `hide` requires `--field` (`created`, `modified`, or `accessed`) and a non-zero
  `--timestamp` in RFC 3339, then calls `SetTimestamp`.
- `detect` always returns no findings, as above.
- `clear` requires `--field` and the original `--timestamp`, then calls
  `SetTimestamp` to put it back.

ext4 is implemented. `internal/filesystem/ext4/timestomp.go` writes the full
inode in one `WriteAt`, so `custody.Wrap` sees the mutation, and it refreshes the
inode checksum when `metadata_csum` is enabled.

APFS and NTFS are pending. APFS needs the inode-record fields written back to the
B-tree. NTFS needs `$SI`, and to be thorough `$FN`, in the MFT record.

`fakefs.Entry.SetTimestamp` (`internal/filesystem/fakefs/fakefs.go`) is the test
double.
