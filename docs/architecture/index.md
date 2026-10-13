---
title: Architecture
nav_order: 4
has_children: true
---

# Architecture

## Goal

Adding a filesystem (FAT32 later, say) or a technique should mean writing a new package that satisfies existing interfaces, not touching code that already works. This lets contributors own a filesystem or technique independently and ship it incrementally, without stepping on each other or on the `cmd/` command files.

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

Each layer depends only on the interfaces of the layer below it, never on a concrete sibling. `internal/technique` code never imports `internal/filesystem/ext4` directly; it calls through the [`FileSystem`]({{ site.src }}/internal/filesystem/filesystem.go#L24) interface. That is what makes filesystems and techniques independently pluggable.

The contracts for each layer are split into their own pages:

- [Image and Custody](image.html): the [`Image`]({{ site.src }}/internal/image/image.go#L4) interface and the `internal/custody` write decorator.
- [Filesystem Interfaces](filesystem.html): [`FileSystem`]({{ site.src }}/internal/filesystem/filesystem.go#L24)/[`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31), the registry, and the three capability interfaces.
- [Technique Interfaces](technique.html): the [`Technique`]({{ site.src }}/internal/technique/technique.go#L92) interface, [`Request`]({{ site.src }}/internal/technique/technique.go#L71)/[`Finding`]({{ site.src }}/internal/technique/technique.go#L32)/[`Result`]({{ site.src }}/internal/technique/technique.go#L39), and the backup/manifest contract.
- [Modes](modes.html): how image mode and live mode split, and what each platform supports.
- [Architecture Decisions](decisions.html): the custody, framing, backup, and capability contracts.

## Why this shape

- Independent ownership: one person builds `internal/filesystem/apfs` while another builds `internal/filesystem/ext4`. Both only need to satisfy [`FileSystem`]({{ site.src }}/internal/filesystem/filesystem.go#L24)/[`Entry`]({{ site.src }}/internal/filesystem/filesystem.go#L31), so neither blocks the other and neither touches shared code.
- Incremental support: a filesystem can implement only [`NamedStreamCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L39). [`SlackSpaceCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L52) and [`TimestompCapable`]({{ site.src }}/internal/filesystem/filesystem.go#L81) are separate optional interfaces, so partial support just means a missing interface, not a broken implementation of one big interface.
- New filesystem equals a new package plus one registry line. No changes to `internal/technique` or the commands.
- Custody recording is structural, not a convention. Because it is a decorator over [`Image`]({{ site.src }}/internal/image/image.go#L4), a technique cannot skip it: every [`WriteAt`]({{ site.src }}/internal/image/image.go#L6) on the wrapped image is hashed and recorded regardless of the caller.

## Non-goals

This mirrors [Home](../#scope)'s scope: no runtime plugin loading (dynamic `.so`/`.dll`), no config-driven technique selection beyond the `--technique` flag. "Pluggable" here means new Go packages behind existing interfaces, compiled in.
