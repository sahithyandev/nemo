# macOS/APFS acceptance evidence

Recorded 2026-10-05 on macOS 26.5.1 arm64 (go1.26.6), using the integrated
CLI and a disposable 64 MiB APFS disk image created and mounted with
`hdiutil`. No production filesystem metadata or existing custody log was
modified. The live cases mutate a disposable file under the script's own
temporary directory, on the host's native APFS root volume.

## Reproduce

Requires Go, Python 3.11+, and `hdiutil`/`xattr` (both ship with macOS). Run
from the repository root:

```sh
python3 scripts/macos-acceptance.py
# Optionally use an already-built binary:
python3 scripts/macos-acceptance.py --nemo /absolute/path/to/nemo --work-parent .
```

The script builds into its temporary directory by default, uses a private
subprocess HOME for custody records, retains numbered command/output logs
and fixtures, and prints their location. Remove only that printed directory
after inspection. Each command has a timeout and any failed assertion
terminates the run; do not use Python's `-O` option, which disables
assertions.

## Observed results

The complete script passed **10 consecutive local executions** with no
retries (100%: 10/10). Each execution independently checked 10 mutation
custody records using Python SHA-256 and compared CLI output records against
the persisted JSONL log.

| Check | Observed result |
| --- | --- |
| Version, features, root/hide/detect/clear help | PASS |
| APFS disk image creation via `hdiutil create` | PASS |
| Whole-image default detection | PASS; source image bytes unchanged |
| Image named-stream hide/detect/clear | PASS; scoped past macOS's own `com.apple.provenance` xattr on copied files |
| Image slack hide/detect/manifest-backed clear | PASS; entire image restored byte-for-byte |
| Image modified timestamp hide/restore | PASS; independently read by mounting via `hdiutil attach`; host file content unchanged |
| Targeted detection for each image technique | PASS |
| Native named-stream hide/detect/clear | PASS; independently read with `xattr -px` (raw `os.getxattr` is Linux-only) |
| Native modified timestamp hide/restore | PASS; independently read through `stat` |
| Native live slack-space refusal | PASS; macOS cannot open a mounted volume's device for write, per `internal/filesystem/apfs/live_darwin.go` |
| Independent custody verification | PASS; 10/10 records per run |

Session artifacts were removed after execution; rerun the commands above to
generate fresh evidence.

## Fixes made during this rehearsal

The script (drafted in a prior session without the ability to execute it on
macOS) needed the following corrections before it passed:

- `hdiutil create -type UDRW` is not a valid create type; dropped the flag
  entirely (an unformatted create already defaults to a read/write image).
- The attach-output parser split the device path on the letter `s`, which
  also matches inside `disk` itself (`/dev/disk4` → `/dev/di` + `k4`).
  Rewrote it to take the first line's device verbatim and find the `/Volumes/`
  mount point by prefix instead.
- `shutil.copy` onto the mounted volume picks up macOS's own
  `com.apple.provenance` xattr, so a blanket "detect finds nothing before
  hide" assertion for named-stream is false on real APFS. Scoped that check
  to the stream name the test itself creates.
- A whole-image SHA-256 equality check spanned two `hdiutil attach`/`detach`
  cycles used only to read back a timestamp; mounting APFS updates journal
  and volume metadata even for a read-only stat, so the hash drifts for
  reasons unrelated to nemo. Dropped that comparison and kept the host-file
  content equality check, which is the real restoration proof.
- `os.getxattr`/`os.listxattr` are Linux-only in the Python stdlib; macOS has
  no equivalent, so native live assertions shelled out to the `xattr` tool
  instead. Plain `xattr -p` also text-mangles and truncates non-printable
  values, so it uses `-px` (hex dump) and decodes that.

`go vet ./...` and `go test ./...` were clean; this run did not surface a
code bug the way the Linux rehearsal did.

## Limits

Timestomp detection intentionally returns no findings: it does not infer
historical tampering. Custody hashes describe logical mutation data (payload,
restored frame bytes, or timestamp detail), not a complete independently
persisted audit of every low-level filesystem write.

This evidence does not certify native Windows behavior, public-dataset
coverage, or a privileged root live-slack attempt. Live slack-space hiding is
expected to refuse on macOS (see the table above), not a gap in this
rehearsal. Teammate review and cross-platform acceptance remain separate
release gates.
