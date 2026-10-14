---
title: Architecture
nav_group: "Contributing"
nav_order: 3
has_children: true
---

# Architecture

## Goal

Adding a filesystem (FAT32 later, say) or a technique should mean writing a new package that fits existing interfaces. It should not mean editing code that already works.

That lets contributors own a filesystem or technique on their own and ship it step by step. They do not step on each other or on the `cmd/` command files.

## File structure

```text
nemo/
  main.go
  cmd/                      // Cobra commands, one file per command, plus the live-mode router
  internal/
    image/                  // Image interface, raw file backend, read-only and aligned wrappers
    binutil/                // bounds-safe binary readers
    filesystem/             // FileSystem/Entry interfaces, registry, in-memory fakefs
      ntfs/ apfs/ ext4/     // parser, named streams, slack, timestomp, live mode
    technique/              // Technique interface, slack framing, backup manifest
    custody/                // write-recording Image decorator and custody log
    validation/             // local-dataset scoring
  tools/validate/           // CLI entry point for internal/validation
```

## Layers

```text
cmd/ (hide.go, detect.go, clear.go, extract.go, features.go, live.go)   Cobra commands
            |
      internal/technique                     Finding/Result, the Technique interface
            |
      internal/filesystem                    FileSystem/Entry interfaces, registry
        ntfs/ apfs/ ext4/                     one implementation package per FS
            |
      internal/custody                       Image decorator: hash + record every write
            |
      internal/image                         Image interface: ReadAt/WriteAt/Size/Path
```

Each layer depends only on the interfaces of the layer below it, never on a concrete sibling. For example, `internal/technique` never imports `internal/filesystem/ext4`. It calls through the [`FileSystem`]({{ site.src }}/internal/filesystem/filesystem.go#L24) interface. That is what lets filesystems and techniques plug in independently.

Each layer's contract has its own page:

- [Image and custody](image.html): the [`Image`]({{ site.src }}/internal/image/image.go#L4) interface and the `internal/custody` write decorator.
- [Filesystem interfaces](filesystem.html): [`FileSystem`]({{ site.src }}/internal/filesystem/filesystem.go#L24) and [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31), the registry, and the three capability interfaces.
- [Technique interfaces](technique.html): the [`Technique`]({{ site.src }}/internal/technique/technique.go#L92) interface, [`Request`]({{ site.src }}/internal/technique/technique.go#L71), [`Finding`]({{ site.src }}/internal/technique/technique.go#L32), [`Result`]({{ site.src }}/internal/technique/technique.go#L39), and the backup and manifest contract.
- [Mode implementation](modes.html): how image mode and live mode split in the code, and the two `Entry` implementations per filesystem.
- [Architecture decisions](decisions.html): the custody, framing, backup, and capability contracts.

## Why this shape

- **Independent ownership.** One person can build `internal/filesystem/apfs` while another builds `internal/filesystem/ext4`. Both only need to satisfy [`FileSystem`]({{ site.src }}/internal/filesystem/filesystem.go#L24) and [`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31). Neither blocks the other, and neither touches shared code.
- **Incremental support.** A filesystem can implement only [`NamedStreamCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L39). [`SlackSpaceCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L52) and [`TimestompCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L81) are separate optional interfaces. Partial support means a missing interface, not a half-built implementation of one big interface.
- **Cheap to extend.** A new filesystem is a new package plus one registry line. `internal/technique` and the commands stay unchanged.
- **Custody by structure.** Custody recording is a decorator over [`Image`]({{ site.src }}/internal/image/image.go#L4), so a technique cannot skip it. Every [`WriteAt`]({{ site.src }}/internal/image/image.go#L6) on the wrapped image is hashed and recorded, whoever the caller is.

## Non-goals

This matches the scope on [Home](../#scope). There is no runtime plugin loading (dynamic `.so` or `.dll`). There is no config-driven technique selection beyond the `--technique` flag. "Pluggable" here means new Go packages behind existing interfaces, compiled in.
