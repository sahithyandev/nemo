# Nemo: Work Breakdown

Numbered so each item maps 1:1 to a GitHub issue. Three owners, one per filesystem, plus a shared core that must land first.

- **Phase 0 (core)**: one owner, blocking — the interfaces everything else compiles against. Do this first.
- **Phase 1 (per filesystem)**: three tracks (NTFS / APFS / ext4), fully parallel once Phase 0 is merged. Same five-item shape per track.
- **Phase 2 (validation/polish)**: after Phase 1 has at least one filesystem working end-to-end.

`Depends on` refers to item numbers in this doc.

## Phase 0 — Shared core

### 1. `internal/image`: `Image` interface + raw file backend
**Owner:** core · **Depends on:** —
**Scope:** `internal/image/image.go` (`Image` interface: `ReadAt`, `WriteAt`, `Size`, `Path`), `internal/image/rawimage.go` (`os.File`-backed implementation).
**Acceptance:** unit test opens a temp file, reads/writes at offsets, checks `Size()`.

### 2. `internal/binutil`: binary helpers
**Owner:** core · **Depends on:** —
**Scope:** `internal/binutil/binutil.go` — bitfield extraction, endian conversion, fixed-width string reads.
**Acceptance:** unit tests covering each helper against known byte sequences.

### 3. `internal/custody`: write logging + hashing
**Owner:** core · **Depends on:** 1
**Scope:** `internal/custody/custody.go` (`Wrap(image.Image) image.Image` decorator, SHA-256 + log entry per `WriteAt`), `internal/custody/log.go`.
**Acceptance:** wrapping a test `Image` and calling `WriteAt` produces one log entry with the correct hash, timestamp, and offset; underlying `Image` still receives the write.

### 4. `internal/filesystem`: core interfaces
**Owner:** core · **Depends on:** —
**Scope:** `internal/filesystem/filesystem.go` — `FileSystem`, `Entry`, `Type` enum, and the three optional capability interfaces (`NamedStreamCapable`, `SlackSpaceCapable`, `TimestompCapable`) per `docs/architecture.md`.
**Acceptance:** compiles with no implementations yet; doc comments match the contracts in `docs/architecture.md`.

### 5. `internal/filesystem/registry.go`: detection + factory
**Owner:** core · **Depends on:** 1, 4
**Scope:** `Detector` struct (`Type`, `Sniff`, `New`, `Techniques`), `Register(Detector)`, `Open(image.Image) (FileSystem, error)` that walks registered detectors.
**Acceptance:** unit test registers two fake detectors, confirms `Open` picks the right one by signature and errors on no match.

### 6. `internal/technique`: technique interfaces + Finding/Result
**Owner:** core · **Depends on:** 4
**Scope:** `internal/technique/technique.go` — `Finding`, `Result`, `NamedStreamTechnique`/`SlackSpaceTechnique`/`TimestompTechnique`, `Get(name string) (Technique, error)`, the capability type-assertion + "unsupported on this filesystem" error path.
**Acceptance:** unit test calls each technique against a fake `Entry` that does/doesn't implement the needed capability interface, checks success and the error case.

### 7. `cmd_hide.go`
**Owner:** core · **Depends on:** 5, 6
**Scope:** flags per `docs/user-interface.md` (`--technique`, `--image`, `--data`, `--stream-name`, `--field`, `--timestamp`), mode selection (image mode iff `--image` given), assembling the custody log line from `Result` + the hash/timestamp `internal/custody` captured.
**Acceptance:** `nemo hide --help` shows correct flags; running against item 11's fake filesystem exercises the named-stream path end-to-end.

### 8. `cmd_detect.go`
**Owner:** core · **Depends on:** 5, 6
**Scope:** target-or-whole-image scanning; no-target case walks `Root()`/`Children()` recursively; `--technique` restricts to one technique, default scans all three; one output line per finding.
**Acceptance:** `nemo detect --help` correct; against item 11's fake filesystem, no-target scan visits every entry.

### 9. `cmd_clear.go`
**Owner:** core · **Depends on:** 5, 6
**Scope:** flags per `docs/user-interface.md` (`--technique` default all, `--stream-name`), custody logging same as `hide`.
**Acceptance:** `nemo clear --help` correct; exercises clear path against item 11's fake filesystem.

### 10. `nemo features`
**Owner:** core · **Depends on:** 5
**Scope:** new Cobra command reading `Techniques` off every registered `Detector`, printing the filesystem × technique matrix.
**Acceptance:** with only fake/no detectors registered, prints an (empty or fake) matrix with no image and no error.

### 11. In-memory fake `FileSystem`/`Entry` for testing
**Owner:** core · **Depends on:** 4
**Scope:** a package-internal (test-only, or `internal/filesystem/fakefs`) implementation of `FileSystem`/`Entry` plus all three capability interfaces, backed by an in-memory map — lets items 6–10 be tested before any real filesystem parser exists.
**Acceptance:** used as the test double in items 6, 7, 8, 9's acceptance tests.

## Phase 1 — Per filesystem (NTFS / APFS / ext4)

Each track is the same five items. Land them in order a → e; b/c/d can ship independently of each other (add the technique's name to `Detector.Techniques` in the same PR it lands in).

### NTFS (owner: NTFS dev)

**12a. NTFS core parser** — `internal/filesystem/ntfs/ntfs.go`, `mft.go`; satisfies `FileSystem`/`Entry` (`Open`, `Children`) against a test NTFS image; registers a `Detector` in `registry.go`. Depends on: 5.
**13b. NTFS named streams (ADS)** — `namedstream.go`, image-mode `NamedStreamCapable`. Depends on: 12a, 6.
**14c. NTFS timestomp** — `timestomp.go`, image-mode `TimestompCapable`. Depends on: 12a, 6.
**15d. NTFS slack space** — `slack.go`, image-mode `SlackSpaceCapable`. Depends on: 12a, 6.
**16e. NTFS live mode** — `live_windows.go` (syscalls for named-stream + timestomp; slack only when opened against a raw device), `live_stub.go` (non-Windows build tag, clean "unsupported" error). Depends on: 13b, 14c.

Acceptance for each: `nemo hide`/`detect`/`clear` with `--technique <x>` succeed against a real or synthetic NTFS test image (or, for 16e, against a live path on Windows / a stub error elsewhere).

### APFS (owner: APFS dev)

**17a. APFS core parser** — `apfs.go`, `btree.go`; registers `Detector`. Depends on: 5.
**18b. APFS named streams** — `namedstream.go` (xattr + resource fork), `NamedStreamCapable`. Depends on: 17a, 6.
**19c. APFS timestomp** — `timestomp.go`. Depends on: 17a, 6.
**20d. APFS slack space** — `slack.go`. Depends on: 17a, 6.
**21e. APFS live mode** — `live_darwin.go`, `live_stub.go`. Depends on: 18b, 19c.

Acceptance: same shape as NTFS, against an APFS test image / macOS live path.

### ext4 (owner: ext4 dev)

**22a. ext4 core parser** — `ext4.go`, `inode.go`; registers `Detector`. Depends on: 5.
**23b. ext4 named streams (xattr)** — `namedstream.go`. Depends on: 22a, 6.
**24c. ext4 timestomp** — `timestomp.go`. Depends on: 22a, 6.
**25d. ext4 slack space** — `slack.go`. Depends on: 22a, 6.
**26e. ext4 live mode** — `live_linux.go`, `live_stub.go`. Depends on: 23b, 24c.

Acceptance: same shape as NTFS, against an ext4 test image / Linux live path.

## Phase 2 — Validation and polish

### 27. `internal/tskcheck`: libtsk cgo adapter
**Owner:** TBD · **Depends on:** 1
**Scope:** `tskcheck.go`, `Validator.CrossCheck(image.Image) ([]Finding, error)`, build-tagged so the rest of the tree still builds with `CGO_ENABLED=0`.
**Acceptance:** `CGO_ENABLED=0 go build ./...` succeeds excluding this package; with cgo enabled, cross-check runs against a test image.

### 28. Wire `tskcheck` into `detect`
**Owner:** TBD · **Depends on:** 8, 27
**Scope:** optional flag/behavior in `cmd_detect.go` (image mode only) to run the cross-check and merge/report its findings.
**Acceptance:** `detect` still works with `tskcheck` unavailable; produces cross-check output when available.

### 29. `internal/validate`: dataset harness
**Owner:** TBD · **Depends on:** 8, at least one of 12–26 fully done
**Scope:** `harness.go` — runs `detect` against `fkie-cad/hide-and-seek-dataset` images, reports pass/fail per known-hidden item.
**Acceptance:** harness runs against a downloaded sample of the dataset and reports a score.

### 30. CI: build, vet, test, cross-compile
**Owner:** TBD · **Depends on:** 7, 8, 9, 16e/21e/26e (at least the stub files)
**Scope:** CI config running `make vet`, `make test`, and `GOOS`-matrixed builds (windows/darwin/linux) to catch a missing `live_stub.go` on any platform.
**Acceptance:** CI green on a clean clone; deliberately breaking one platform's stub fails the matrix build.

### 31. README refresh
**Owner:** TBD · **Depends on:** most of Phase 1
**Scope:** update README's "File structure" section (and anything else stale) to match the real, built-out tree instead of the planned one.
**Acceptance:** README's file tree matches `find . -name '*.go'` output.

## Doc fix folded in here

`docs/architecture.md`'s tree shows `cmd/main.go`; the repo actually has `main.go` at the repo root plus `cmd/root.go` and `cmd/VERSION`. Fixed in this pass (see the corresponding architecture.md edit).
