---
title: References
nav_group: "Contributing"
nav_order: 5
---

# References

Specs, docs, and tools to read when you work on nemo. Each section says which nemo package or page it backs.

## Filesystem specifications

### NTFS

Used by [`internal/filesystem/ntfs`](file-systems/ntfs.html).

- [NTFS Documentation](https://flatcap.github.io/linux-ntfs/ntfs/) by Russon and Fledel. The best free description of the on-disk layout.
- [Data runs](https://flatcap.github.io/linux-ntfs/ntfs/concepts/data_runs.html): how non-resident attributes map to clusters.
- [`$DATA` attribute](https://flatcap.github.io/linux-ntfs/ntfs/attributes/data.html): where alternate data streams live.
- [`$STANDARD_INFORMATION`](https://flatcap.github.io/linux-ntfs/ntfs/attributes/standard_information.html) and [`$FILE_NAME`](https://flatcap.github.io/linux-ntfs/ntfs/attributes/file_name.html): the two timestamp copies behind timestomp detection.
- [libfsntfs format notes](https://github.com/libyal/libfsntfs/blob/main/documentation/New%20Technologies%20File%20System%20%28NTFS%29.asciidoc): a second, more formal write-up to cross-check against.
- [MS-FSCC, File System Control Codes](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fscc/): Microsoft's spec for streams and FILETIME.
- [File streams](https://learn.microsoft.com/en-us/windows/win32/fileio/file-streams) on Microsoft Learn: the `file:stream` syntax from the API side.

### ext4

Used by [`internal/filesystem/ext4`](file-systems/ext4.html).

- [ext4 documentation index](https://docs.kernel.org/filesystems/ext4/index.html) in the Linux kernel docs.
- [Superblock](https://docs.kernel.org/filesystems/ext4/super.html): feature flags and geometry.
- [Inodes](https://docs.kernel.org/filesystems/ext4/inodes.html): fields, extra area, and timestamp encoding.
- [Dynamic structures](https://docs.kernel.org/filesystems/ext4/dynamic.html): the extent tree and inline data.
- [Directory entries](https://docs.kernel.org/filesystems/ext4/directory.html): linear and HTree directories.
- [Extended attributes](https://docs.kernel.org/filesystems/ext4/attributes.html): the in-inode and external block layouts.

### APFS

Used by [`internal/filesystem/apfs`](file-systems/apfs.html).

- [Apple File System Reference](https://developer.apple.com/support/downloads/Apple-File-System-Reference.pdf): Apple's official spec.
- [libfsapfs format notes](https://github.com/libyal/libfsapfs/blob/main/documentation/Apple%20File%20System%20%28APFS%29.asciidoc): community notes that fill gaps in the official spec.

## Hiding techniques

Background for the [technique pages](techniques/).

- [MITRE ATT&CK T1564.004, NTFS File Attributes](https://attack.mitre.org/techniques/T1564/004/): how attackers use alternate data streams.
- [MITRE ATT&CK T1070.006, Timestomp](https://attack.mitre.org/techniques/T1070/006/): how timestomping is used and detected.
- [Slack space](https://en.wikipedia.org/wiki/Slack_space) on Wikipedia: a short intro to the concept.

## OS interfaces

Used by the live-mode code. See [Modes](architecture/modes.html).

- [`xattr(7)`](https://man7.org/linux/man-pages/man7/xattr.7.html): Linux extended attribute namespaces.
- [`utimensat(2)`](https://man7.org/linux/man-pages/man2/utimensat.2.html): setting times with `UTIME_OMIT`.
- [`statx(2)`](https://man7.org/linux/man-pages/man2/statx.2.html): birth time and mount ID.
- [`setattrlist(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/setattrlist.2.html): how macOS sets creation time.
- [`SetFileTime`](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-setfiletime): the Win32 timestamp call.

## Tools and datasets

- [The Sleuth Kit](https://github.com/sleuthkit/sleuthkit) and its [documentation](https://www.sleuthkit.org/sleuthkit/docs.php): the reference forensic toolkit. Use `fls`, `istat`, and `blkls` to cross-check nemo's output.
- [hide-and-seek-dataset](https://github.com/fkie-cad/hide-and-seek-dataset): the public corpus nemo benchmarks against. See [Validation](validation/).
