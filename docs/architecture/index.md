---
title: Architecture
nav_order: 6
has_children: true
---

# Architecture

## Goal

Adding a filesystem (FAT32 later, say) or a technique should mean writing a new package that satisfies existing interfaces, not touching code that already works. This lets contributors own a filesystem or technique independently and ship it incrementally, without stepping on each other or on the `cmd/` command files.

## File structure

What exists today is marked "built"; the rest is the planned layout. `nemo clear`, live mode, `internal/tskcheck`, and `internal/validate` are not written yet.

```
nemo/
  main.go                  // built: calls cmd.Execute
  cmd/                      // package cmd, one file per command
    root.go                // built: root command + "nemo version" (embeds VERSION)
    VERSION                // built
    hide.go                // built
    detect.go              // built
    features.go            // built: "nemo features" support matrix
    filesystems.go         // built: blank-imports the filesystem packages so they register
    clear.go               // planned
  internal/
    image/
      image.go             // built: Image interface (ReadAt, WriteAt, Size, Path)
      rawimage.go          // built: os.File backend, Open + OpenReadOnly
      readonly.go          // built: ReadOnly wrapper, fails every write (detect's guard)
    binutil/
      binutil.go           // built: Uint/Int, Bits, String/UTF16String
      reader.go            // built: sequential cursor with a sticky first-error
      doc.go                // built
    filesystem/
      filesystem.go        // built: FileSystem, Entry, Type, the three capability interfaces
      registry.go          // built: Detector, Register, Detectors, signature-based Open
      fakefs/
        fakefs.go          // built: in-memory FileSystem/Entry with all three capabilities
        image.go           // built: in-memory Image, for tests
      ntfs/
        ntfs.go             // built: parser + Detector
        mft.go              // built
        attribute.go        // built
        bootsector.go       // built
        directory.go        // built
        namedstream.go     // built: ADS, resident and non-resident, MFT-mirror aware
        slack.go            // planned
        timestomp.go        // planned
        live_windows.go    // planned: live-mode Entry, direct syscalls, no MFT parsing
        live_stub.go        // planned: "unsupported on this OS" on non-Windows builds
      apfs/
        apfs.go            // built: parser + Detector
        namedstream.go     // built: xattr + resource fork (in-place B-tree leaf rewrite)
        btree_write.go     // built: single-leaf in-place rewrite helpers
        btree.go           // built
        slack.go            // planned
        timestomp.go        // planned
        live_darwin.go      // planned
        live_stub.go        // planned
      ext4/
        ext4.go            // built: parser + Detector (named-stream, timestomp)
        inode.go           // built
        xattr.go           // built: named streams via xattr
        timestomp.go       // built
        slack.go            // planned
        live_linux.go       // planned
        live_stub.go        // planned
    technique/
      technique.go         // built: Technique interface, Finding/Result/Backup/Request, Get, ErrUnsupported
      slackframe.go        // built: slack frame encode/decode
      manifest.go          // built: JSON Lines backup manifest for clear
    custody/
      custody.go           // built: Wrap decorator, SHA-256 + record per WriteAt
      log.go                // built: Record type, append to the custody log
    tskcheck/              // planned
      tskcheck.go           // cgo adapter over libtsk, isolated so only this package needs cgo
    validate/             // planned
      harness.go            // runs against fkie-cad/hide-and-seek-dataset
  go.mod
```

## Layers

```
cmd/ (hide.go, detect.go, features.go, clear.go)   Cobra commands
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

Each layer depends only on the interfaces of the layer below it, never on a concrete sibling. `internal/technique` code never imports `internal/filesystem/ext4` directly; it calls through the `FileSystem` interface. That is what makes filesystems and techniques independently pluggable.

The contracts for each layer are split into their own pages:

- [Image and Custody](image.html): the `Image` interface and the `internal/custody` write decorator.
- [Filesystem Interfaces](filesystem.html): `FileSystem`/`Entry`, the registry, and the three capability interfaces.
- [Technique Interfaces](technique.html): the `Technique` interface, `Request`/`Finding`/`Result`, and the backup/manifest contract.
- [Live Mode](live-mode.html): the planned split between image mode and live mode.

`internal/tskcheck` is planned and isolated in its own package, since it is the only place cgo is required (a libtsk binding). Nothing else in the tree may import it, so the rest of the codebase stays buildable with `CGO_ENABLED=0`.

## Why this shape

- Independent ownership: one person builds `internal/filesystem/apfs` while another builds `internal/filesystem/ext4`. Both only need to satisfy `FileSystem`/`Entry`, so neither blocks the other and neither touches shared code.
- Incremental shipping: a filesystem can land with only `NamedStreamCapable`. `SlackSpaceCapable` and `TimestompCapable` are separate optional interfaces, so partial support is just the unbuilt pieces missing, not a broken implementation of one big interface.
- New filesystem equals a new package plus one registry line. No changes to `internal/technique` or the commands.
- Custody recording is structural, not a convention. Because it is a decorator over `Image`, a technique cannot skip it: every `WriteAt` on the wrapped image is hashed and recorded regardless of the caller.

## Non-goals

This mirrors [Overview](../overview.html)'s scope: no runtime plugin loading (dynamic `.so`/`.dll`), no config-driven technique selection beyond the `--technique` flag. "Pluggable" here means new Go packages behind existing interfaces, compiled in.
