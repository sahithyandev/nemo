---
title: Home
nav_group: "User Guide"
nav_order: 1
---

# nemo

Nemo is a command line tool for hiding, finding, and removing hidden data on NTFS, APFS, and ext4. It is written in Go and runs on Linux, macOS, and Windows. Live writes on Windows are not supported yet.

It has two modes:

- **Image mode** works offline on a raw, unencrypted disk image.
- **Live mode** works on the filesystem of the machine you are running on.

[Modes](modes.html) lists what each platform supports. To try nemo, grab a test image below.

## Install

On Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/sahithyandev/nemo/main/scripts/install.sh | sh
```

On Windows (PowerShell):

```powershell
powershell -c "irm https://raw.githubusercontent.com/sahithyandev/nemo/main/scripts/install.ps1 | iex"
```

Each script downloads the latest release for your OS and architecture from the [releases page](https://github.com/sahithyandev/nemo/releases). If you'd rather not pipe a script into your shell, read it first.

## A quick example

No disk image of your own? Download `apfs-bare` and unpack it:

```sh
curl -fsSLO https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-bare.img.gz
gunzip apfs-bare.img.gz
```

The file `xattr.txt` on that image already has data hidden in it. Find it:

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

Read it back out:

```sh
nemo extract hello.txt \
  --technique named-stream \
  --stream-name notes \
  --image apfs-bare.img
```

Remove it, which puts `hello.txt` back the way it was:

```sh
nemo clear hello.txt \
  --technique named-stream \
  --stream-name notes \
  --image apfs-bare.img
```

To see which filesystem and technique pairs nemo supports:

```sh
nemo features
```

Every command and flag is in the [CLI reference](cli/).

## Test images

These are small, disposable APFS images. They live in the repo and nemo's own tests use them. Each one is gzipped, so run `gunzip <file>.gz` after downloading.

All but `apfs-encrypted` hold the same files:

- `hello.txt`
- `xattr.txt`, with a named stream
- `slack.bin`, with trailing slack
- `bigxattr.txt`
- `rsrc.txt`, with a resource fork

So every example above works on any of them.

<table class="test-images">
<thead><tr><th>Image</th><th>What's different about it</th><th></th></tr></thead>
<tbody>
<tr><td><code>apfs-bare</code></td><td>no partition map, the plainest container</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-bare.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-gpt</code></td><td>GPT-wrapped, the default macOS layout</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-gpt.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-casesensitive</code></td><td>case-sensitive volume</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-casesensitive.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-16k</code></td><td>16 KiB blocks instead of the usual 4 KiB</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-16k.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-manyfiles</code></td><td>2000+ files, forces a multi-level B-tree</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-manyfiles.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-multivol</code></td><td>two volumes in one container</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-multivol.img.gz" download>Download</a></td></tr>
<tr><td><code>apfs-encrypted</code></td><td>encrypted and empty, nemo should refuse it</td><td><a class="btn" href="https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/apfs-encrypted.img.gz" download>Download</a></td></tr>
</tbody>
</table>

To download from the command line, put the image name in this command:

```sh
curl -fsSLO https://raw.githubusercontent.com/sahithyandev/nemo/main/testdata/<image>.img.gz
```

## Scope

### In scope

- NTFS, APFS, and ext4
- Named-stream hiding, slack-space hiding, and timestomping
- Image mode and live mode
- Hide, detect, extract, and clear commands
- Chain-of-custody logging and SHA-256 hashing
- Validation against a public dataset

### Out of scope

- FAT32 and exFAT
- Deleted-file recovery and file carving
- Malware execution
- Privilege escalation or bypassing
- A GUI

Some of these may come later, but none are planned.

Several anti-forensic techniques close to nemo's three are also out of scope, from unallocated-space hiding to snapshot stashing. [Adjacent techniques](techniques/adjacent-techniques.html) explains what they are and why nemo skips them.

## Where to go next

- Use the tool: [CLI reference](cli/)
- Learn the techniques: [Techniques](techniques.html)
- Contribute a filesystem or technique: [How techniques work](techniques/), [Architecture](architecture/), and [File systems](file-systems/)
