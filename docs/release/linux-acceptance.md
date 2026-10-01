# Linux/ext4 acceptance evidence

Recorded 2026-10-01 on Linux amd64, using the integrated CLI and disposable
32 MiB ext4 images generated with `mke2fs` and populated with `debugfs`.
No downloaded image, production filesystem metadata, or existing custody log
was modified. The live cases create new temporary files on the selected parent
filesystem.

## Reproduce

Requires Go, Python 3.11+, and e2fsprogs (`mke2fs`, `debugfs`). Run from the
repository root:

```sh
python3 scripts/linux-acceptance.py
# Exercise native live mode when /tmp is not ext4:
python3 scripts/linux-acceptance.py --work-parent .
# Optionally use an already-built binary:
python3 scripts/linux-acceptance.py --nemo /absolute/path/to/nemo --work-parent .
```

Choose an ext4 parent for native coverage. A non-ext4 temporary filesystem is
reported as a native-mode skip, not as a pass. The script builds into its
temporary directory by default, uses a private subprocess HOME for custody
records, retains numbered command/output logs and fixtures, and prints their
location. Remove only that printed directory after inspection. Each command
has a timeout and any failed assertion terminates the run; do not use Python's
`-O` option, which disables assertions.

## Observed results

The complete native/image script passed **10 consecutive local executions**
with no retries (100%: 10/10). Each execution independently checked 10 mutation
custody records using Python SHA-256 and compared CLI output records against
the persisted JSONL log. CI execution has not been observed.

| Check | Observed result |
| --- | --- |
| Version, features, root/hide/detect/clear help | PASS |
| Whole-image default detection | PASS; full image SHA-256 unchanged |
| Image named-stream hide/detect/clear | PASS |
| Image slack hide/detect/manifest-backed clear | PASS; entire image restored byte-for-byte |
| Image modified timestamp hide/restore | PASS; independently read through debugfs; entire image restored |
| Targeted detection for each image technique | PASS; entire image SHA-256 unchanged |
| Original file content after all image mutations | PASS; independently dumped with debugfs |
| Native named-stream hide/detect/clear | PASS; independently read with os.getxattr |
| Native modified timestamp hide/restore | PASS; independently read through stat |
| Native detection content/xattr/mtime/ctime checks | PASS |
| Native symlink target refusal | PASS |
| Native unreadable-file permission denial | PASS; executed as non-root |
| Native slack mutation refusal | PASS; non-root privilege error |
| Independent custody verification | PASS; 10/10 records per native run |

The existing lower-level Linux live tests also passed ten repetitions:

```sh
go test ./internal/filesystem/ext4 -run TestLive -count=10
```

The initial default-directory run passed all image checks and reported a native
skip because /tmp is tmpfs. The first ext4-parent CLI run exposed a traversal
contract bug: live regular files returned an error from Children(), which the
CLI walker calls on every entry. Integration corrected the leaf contract to
return no children and added a regression test. All ten native runs above
follow that correction.

Session artifacts were moved outside the repository after execution. One
complete passing set is at `/tmp/nemo-acceptance-native-o1gkhgpu`; repetitions
have summaries `/tmp/nemo-acceptance-repeat-1.log` through
`/tmp/nemo-acceptance-repeat-9.log`. These paths are session-local, not shipped
release artifacts; rerun the commands above to generate your own evidence.

## Limits

Timestomp detection intentionally returns no findings: it does not infer
historical tampering. Custody hashes describe logical mutation data (payload,
restored frame bytes, or timestamp detail), not a complete independently
persisted audit of every low-level filesystem write. The script verifies that
contract explicitly; image immutability/restoration checks cover entire image
bytes separately.

This evidence does not certify native Windows/macOS behavior, public-dataset
coverage, or a privileged root live-slack attempt. Created/changed live
timestamps have platform limitations; this CLI rehearsal exercises modified
timestamps. Teammate review and cross-platform acceptance remain separate
release gates.
