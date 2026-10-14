---
title: Timestomping
parent: How techniques work
nav_order: 3
---

# Timestomping

## What it is

Timestomping rewrites a file's timestamps so it looks older, newer, or contemporary with other files. It defeats timeline analysis, which is the first thing an examiner reaches for.

Unlike named-stream and slack-space hiding, it stores nothing. It overwrites an existing field with a chosen value.

```sh
# Linux (ext4): set mtime and atime
touch -d '2019-01-01T00:00:00Z' report.txt

# macOS
SetFile -m 01/01/2019 00:00:00 report.txt   # from the Xcode command-line tools

# Windows (PowerShell)
(Get-Item report.txt).LastWriteTime = '2019-01-01'
```

## The MACB timestamps

An examiner rebuilds "what happened when" from four timestamps, remembered as MACB:

| Letter | Meaning | NTFS | ext4 | APFS |
| ------ | ------- | ---- | ---- | ---- |
| M | content last modified | `$SI` and `$FN` MtimeMs | `i_mtime` | `mod_time` |
| A | content last accessed | `$SI` and `$FN` AtimeMs | `i_atime` | `access_time` |
| C | metadata last changed | `$SI` and `$FN` CtimeMs (MFT change) | `i_ctime` | `change_time` (`ctime`) |
| B | born, or created | `$SI` and `$FN` CrtimeMs | `i_crtime` | `create_time` |

Each filesystem stores these differently, at different precision, in different structures. Some fields cannot be set through any normal API. That mismatch is what makes timestomping detectable.

## Detection by a third party

Timestomping is one of the more detectable techniques, because the same information exists in more than one place.

- **NTFS:** cross-check `$SI` against `$FN` and check sub-second precision. Parse the `$LogFile` transaction log and the `$UsnJrnl:$J` change journal. They record the real times of operations apart from the file's own fields.
- **ext4:** compare `ctime` with `mtime` and `atime`. The `jbd2` journal records the actual write times of recent metadata changes.
- **APFS:** compare against snapshot copies and Spotlight. `fsck_apfs` and forensic tools diff the inode record against snapshot deltas.
- **Everywhere:** look for outside evidence. That includes application logs, `$Recycle.Bin` and `.Trash` records, Prefetch (`.pf` file mtimes), browser history, backup catalogs, and MRU registry keys. Timestomping the file touches none of them.

## Why nemo's `detect` reports nothing for timestomp

[`timestompTechnique.Detect`]({{ site.src }}/internal/technique/technique.go#L405) (`internal/technique/technique.go`) confirms the entry implements the capability, then returns `nil, nil`.

A current timestamp cannot show whether it was altered. Nemo has no historical baseline and no timestamp anomaly detector. It does not cross-check independent records, such as NTFS `$SI` against `$FN`. So it cannot report a finding from the current value alone.

`hide` does not read the original timestamp and save it to a manifest. So `nemo clear --technique timestomp` needs the original timestamp passed in, and it rejects a zero value. The shared reader would make an automatic backup possible, but the technique and manifest workflow do not do it.

## Implementation Notes

### On-disk mechanics

#### NTFS: the `$SI` and `$FN` pair

Every MFT record carries the four timestamps twice.

- `$STANDARD_INFORMATION` (`$SI`, attribute `0x10`) is the set that Explorer, `dir`, and the Win32 API (`GetFileTime`, `SetFileTime`) read and write.
- `$FILE_NAME` (`$FN`, attribute `0x30`) is a second copy that the kernel updates on rename or move. The Win32 API cannot write it.

All eight values are Windows FILETIME: a 64-bit count of 100-nanosecond intervals since 1601-01-01 UTC.[^fscc]

What an examiner checks:

- **`$SI` against `$FN`.** A user-space timestomp tool sets `$SI` and leaves `$FN` alone, so `$FN` still shows the real creation time. Forensic suites flag the mismatch automatically.
- **Sub-second digits.** Real timestamps have noise in the low digits. A value like `2019-01-01T00:00:00.0000000`, with every 100 ns bit zero, looks typed, not recorded. Many tools set whole seconds only.
- **Ordering.** M earlier than B, or a timestamp from before the volume was formatted.

#### ext4: split second and nanosecond fields

The 128-byte "good old" inode holds `i_atime`, `i_mtime`, and `i_ctime` as 32-bit signed seconds since 1970. Larger inodes, typically 256 bytes, add two things in the extra area past `i_extra_isize`:

- `i_crtime` (creation), a 32-bit seconds field at inode offset 144.
- `i_{mtime,atime,ctime,crtime}_extra`, 32-bit fields that pack nanoseconds into the upper 30 bits and a 2-bit epoch extension into the low bits. The extension pushes the range past 2038.

`internal/filesystem/ext4/timestomp.go` implements exactly this.

- [`encodeExt4Timestamp`]({{ site.src }}/internal/filesystem/ext4/timestomp.go#L162) splits a `time.Time` into `(low uint32, extra uint32)` with `extra = nanoseconds<<2 | epoch`.
- [`timestampLayout`]({{ site.src }}/internal/filesystem/ext4/timestomp.go#L115) maps `created`, `modified`, and `accessed` to offsets 144, 16, and 8. It also reports whether the inode is large enough for the `_extra` field and whether it has `i_crtime` at all.

Offsets from the code:

```text
i_atime            8
i_mtime           16
i_extra_isize    128   (16-bit; xattr magic 0xEA020000 may follow)
i_mtime_extra    136
i_atime_extra    140
i_crtime         144
i_crtime_extra   148
```

What an examiner checks: no userland API can set `i_ctime`. The kernel stamps it with the current time on every inode change, including the write that stomps `i_mtime`. So an `mtime` far in the past with a `ctime` of a few seconds ago is the ext4 timestomp signature.

Nemo can write `i_ctime`, because it edits the inode bytes directly in an image. A live attacker on a mounted filesystem cannot.

#### APFS: nanosecond fields in the inode record

APFS stores the four timestamps in the `j_inode_val_t` inode record in the filesystem B-tree. Each is a 64-bit count of nanoseconds since 1970-01-01 UTC (`create_time`, `mod_time`, `change_time`, `access_time`).[^apfs] The precision is higher than NTFS or ext4, so a whole-second value is an even stronger sign that a human typed it.

APFS also keeps copies of the dates that a timestomp does not reach:

- Spotlight metadata (`.Spotlight-V100`) indexes `kMDItemContentModificationDate`, `kMDItemDateAdded`, and others from when the file was indexed.
- `com.apple.metadata:*` xattrs, such as `_kMDItemUserTags`, and quarantine (`com.apple.quarantine`) carry their own dates.
- Local `tmutil` snapshots keep the inode record as it was.

### In nemo

[`filesystem.TimestompCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L81) exposes the current value and field support, as well as the write:

```go
type TimestompCapable interface {
    Timestamp(field TimeField) (time.Time, error)
    SupportsTimestamp(field TimeField) (bool, error)
    SetTimestamp(field TimeField, t time.Time) error
}
```

[`timestompTechnique`]({{ site.src }}/internal/technique/technique.go#L376) (`internal/technique/technique.go`) works like this:

- `hide` requires `--field` (`created`, `modified`, `accessed`, or `changed`) and a non-zero `--timestamp` in RFC 3339. It then calls [`SetTimestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L84).
- `detect` always returns no findings, as explained above.
- `clear` requires `--field` and the original `--timestamp`. It calls [`SetTimestamp`]({{ site.src }}/internal/filesystem/filesystem.go#L84) to put it back.

#### The `changed` field

`changed` ([`filesystem.TimeChanged`]({{ site.src }}/internal/filesystem/filesystem.go#L67)) maps to `i_ctime` on ext4 and `change_time` on APFS. The kernel sets this MACB field on its own, and no live userland API lets a process write it.

Editing the image directly bypasses that. So `hide --field changed` sets exactly what a live attacker cannot. It closes the "old mtime, fresh ctime" gap that normally gives a stomp away.

#### Per filesystem

- **ext4.** `internal/filesystem/ext4/timestomp.go` writes the whole inode in one [`WriteAt`]({{ site.src }}/internal/image/image.go#L6), so [`custody.Wrap`]({{ site.src }}/internal/custody/custody.go#L35) sees the change. It refreshes the inode checksum when `metadata_csum` is on.
- **APFS.** `internal/filesystem/apfs/timestomp.go` patches the four `uint64` nanosecond fields of the `j_inode_val_t` INODE record in place. It uses the same in-place B-tree leaf rewrite as `namedstream.go` (see [`btree_write.go`]({{ site.src }}/internal/filesystem/apfs/btree_write.go)). The record size does not change, so the existing checksum and `btree_info_t` handling covers it.
- **NTFS.** `internal/filesystem/ntfs/timestomp.go` reads and writes the chosen `$STANDARD_INFORMATION` timestamp with 100 ns precision. It regenerates the record fixups and verifies the write. It does not update the `$FILE_NAME` timestamps or compare the two copies for detection.
- **fakefs.** `fakefs.Entry.SetTimestamp` (`internal/filesystem/fakefs/fakefs.go`) is the test double.

A timestomp still leaves independent copies of the old dates behind. On APFS, the directory entry's own `date_added` (`j_drec_val_t`), Spotlight metadata, and local snapshots all keep the value from before the stomp.

#### macOS live mode

The live path is in `internal/filesystem/apfs/live_darwin.go`.

- `modified` and `accessed` go through `os.Chtimes`.
- `created` goes through `setattrlist(ATTR_CMN_CRTIME)`, because the standard library has no equivalent.
- `changed` is unsupported, for the same reason it is unsupported live everywhere.

See [APFS live mode](../file-systems/apfs.html#live-mode-macos).

## References

[^fscc]: Microsoft, [\[MS-FSCC\]: File System Control Codes](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fscc/), §2.4.7 and §2.4.4.

[^apfs]: Apple, [*Apple File System Reference*](https://developer.apple.com/support/downloads/Apple-File-System-Reference.pdf), `j_inode_val_t`.
