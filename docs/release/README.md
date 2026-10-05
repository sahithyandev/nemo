> Integration note (2026-10-02): the candidate evidence below refers to the
> pre-integration `0.1.0-rc.1` build. `cmd/VERSION` remains `0.0.1` until release
> acceptance is complete, preventing main CI from publishing the candidate.
> Packaging the integrated branch uses its current version.

# v0.1 semester release candidate

The prepared version is **0.1.0-rc.1**. This is a candidate, not an approved final
release. Public-dataset scoring, teammate review, DOC-01 and QA-01 sign-off remain
release gates. No final tag should be published until those gates are complete.

## Build and package

Install the Go version required by `go.mod` or newer, Python 3.11+, and Git. Dependency
downloads require network access on a fresh machine. From a clean checkout:

```sh
go test ./...
go vet ./...
python3 scripts/package-release.py --output /tmp/nemo-release-candidate
cd /tmp/nemo-release-candidate
sha256sum -c SHA256SUMS
```

The output directory must not exist. The script builds Linux amd64, macOS amd64
and arm64, and Windows amd64 with CGO disabled. Archives include the binary,
license, release notes, release instructions, and demo plan. It never tags or
publishes. Archive metadata is fixed; Go build paths, VCS stamping, and build IDs
are removed. Byte reproducibility requires the same Go toolchain, dependencies,
and source. Cross-compilation proves buildability, not native runtime acceptance.

For a clean-clone rehearsal, clone the work branch into a new temporary directory,
run the commands above there, and retain the commit ID, `go version`, test output,
and SHA256SUMS. Repeat packaging to another new directory and compare SHA256SUMS.
See [rehearsal evidence](rehearsal.md), [Linux acceptance](linux-acceptance.md), and
[macOS acceptance](macos-acceptance.md). Windows acceptance needs a native run and
is not yet recorded. A libtsk cross-check (INT-01) is deferred out of scope for this
release, so no third-party parser corroborates these results.

## Implemented scope and limitations

- Image-mode NTFS, APFS and ext4 parsers and supported named-stream, timestamp,
  and slack operations; consult `features` and filesystem documentation for limits.
- Shared detect/clear commands and backup manifests for framed slack.
- Linux ext4 native user xattrs and accessed/modified timestamps; final symlinks
  and non-ext4 mounts rejected. Linux live slack is unsupported.
- APFS native operations on macOS; Windows NTFS live mode remains read-only.
- Detection cannot establish whether a timestamp was maliciously changed.
  Slack detection recognizes Nemo frames, not arbitrary hidden residual bytes.
- The older unmerged ext4 slack format differs from the shared format now used.
  Recreate old demo fixtures; no legacy-frame migration is provided.
- Live mutation custody records contain logical operation metadata. Image write
  hashes do not imply equivalent byte-level audit coverage for native syscalls.

Mutations can destroy data. Demonstrate only on disposable files and unmounted
copies of images; retain backups and slack manifests. Never use original evidence,
production volumes, or mounted images. Clearing slack without its matching backup
zero-fills the frame rather than recovering previous residual bytes.
