---
title: Home
nav_group: "User Guide"
nav_order: 1
---

# nemo

Nemo is a cross-platform CLI tool, written in Go, for hiding, detecting, and clearing hidden data on NTFS, APFS, and ext4. It works in two modes: image mode, offline against a raw, unencrypted disk image, and live mode, directly on the filesystem of the machine it runs on (see [Modes](modes.html) for what each platform supports). Grab a disk image and try it out below.

## Install

On Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/sahithyandev/nemo/main/scripts/install.sh | sh
```

On Windows (PowerShell):

```powershell
powershell -c "irm https://raw.githubusercontent.com/sahithyandev/nemo/main/scripts/install.ps1 | iex"
```

Each script downloads the latest release for your OS and architecture from the [releases page](https://github.com/sahithyandev/nemo/releases). Read a script before piping it into a shell if you'd rather not take that on faith.

## A quick example

No disk image of your own? Grab `apfs-bare`, below, and unpack it:

```sh
curl -fsSLO https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-bare.img.gz
gunzip apfs-bare.img.gz
```

`xattr.txt` on that image already has data hidden in it. Find it:

```sh
nemo detect xattr.txt \
  --image apfs-bare.img
```

```text
TECHNIQUE      TARGET       LOCATION         SIZE
named-stream   xattr.txt    user.nemo.test   4
```

Now hide something of your own in `hello.txt`:

```sh
nemo hide hello.txt \
  --technique named-stream \
  --data secret.bin \
  --stream-name notes \
  --image apfs-bare.img
```

Read it back out, without needing to know it was ever hidden as `secret.bin`:

```sh
nemo extract hello.txt \
  --technique named-stream \
  --stream-name notes \
  --image apfs-bare.img
```

Remove it again, restoring `hello.txt` to how it looked before:

```sh
nemo clear hello.txt \
  --technique named-stream \
  --stream-name notes \
  --image apfs-bare.img
```

And check which filesystem/technique pairs nemo supports before trying one:

```sh
nemo features
```

See [CLI Reference](cli/) for every command and flag.

## Test images

Real, disposable APFS images, committed to the repo and used by nemo's own tests. Each one is gzipped; unpack with `gunzip <file>.gz` after downloading. All but `apfs-encrypted` have the same small file set written to them (`hello.txt`, `xattr.txt` with a named stream, `slack.bin` with trailing slack, `bigxattr.txt`, `rsrc.txt` with a resource fork), so any example above works against any of them.

<table class="test-images">
<thead><tr><th>Image</th><th>What's different about it</th><th></th></tr></thead>
<tbody>
<tr><td><code>apfs-bare</code></td><td>no partition map, the plainest container</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-bare.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-gpt</code></td><td>GPT-wrapped, the default macOS layout</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-gpt.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-casesensitive</code></td><td>case-sensitive volume</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-casesensitive.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-16k</code></td><td>16 KiB blocks instead of the usual 4 KiB</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-16k.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-manyfiles</code></td><td>2000+ files, forces a multi-level B-tree</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-manyfiles.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-multivol</code></td><td>two volumes in one container</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-multivol.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-encrypted</code></td><td>encrypted, unpopulated; nemo should refuse it</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-encrypted.img.gz" download>Download</a></td></tr>
</tbody>
</table>

Prefer the command line? Swap the filename into:

```sh
curl -fsSLO https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/<image>.img.gz
```

## Scope

### In scope

NTFS, APFS, and ext4 support for named-stream hiding, slack-space hiding, and timestomping, in both image mode and live mode; CLI hide/detect/clear workflows; chain-of-custody logging; SHA-256 integrity hashing; validation against a public dataset.

### Out of scope

FAT32/exFAT support, deleted-file recovery, file carving, malware execution, privilege escalation or bypassing, and a GUI. Some of these could be implemented in the near future, but none are planned for now.

Also out of scope are the related anti-forensic techniques below. Recognise them
so you do not reach for one by accident, and so you know where nemo's guarantees
stop.

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

## Where to go next

- Use the tool: [CLI Reference](cli/).
- Understand the techniques: [Techniques](techniques/).
- Contribute a filesystem or technique: [Architecture](architecture/) and [File Systems](file-systems/).
