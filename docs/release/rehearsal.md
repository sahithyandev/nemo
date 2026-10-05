# Clean-clone release rehearsal

Recorded 2026-10-01 at source commit
`6027afa5e0d476f2bb215158a1205bea5912bf45` on Linux amd64.
The final tracking commit adds this evidence and the work report only.

Toolchain: `go version go1.27.1-X:nodwarf5 linux/amd64`.
Go dependencies were downloaded before rehearsal; this is a clean source checkout,
not a cold dependency-cache or offline-install test. `GOCACHE=/tmp/nemo-go-cache`
provided a writable build cache.

## Commands and results

```sh
git clone --no-hardlinks --single-branch \
  --branch NirmalKBandara/val-02-rel-01-validation-release \
  /home/nirmal/Projects/nemo /tmp/nemo-clean-release-rehearsal
cd /tmp/nemo-clean-release-rehearsal
GOCACHE=/tmp/nemo-go-cache go test ./...
GOCACHE=/tmp/nemo-go-cache go vet ./...
GOCACHE=/tmp/nemo-go-cache python3 scripts/package-release.py --output /tmp/nemo-clean-pack-1
GOCACHE=/tmp/nemo-go-cache python3 scripts/package-release.py --output /tmp/nemo-clean-pack-2
diff /tmp/nemo-clean-pack-1/SHA256SUMS /tmp/nemo-clean-pack-2/SHA256SUMS
git status --porcelain
```

- All Go packages passed their tests, including the generated ext4 validation sample.
- `go vet ./...` passed.
- Linux amd64, macOS amd64/arm64 and Windows amd64 archives built successfully.
- Two packaging runs produced identical SHA256SUMS; all archive hashes were
  independently recomputed with Python hashlib and matched.
- The packaged Linux binary executed and reported `0.1.0-rc.1`.
- The clone had no tracked modifications or untracked files after rehearsal.
- macOS and Windows executables were cross-built, not executed on native hosts.

## Recorded archive hashes

```
e312a665fc40b723e539731cda1431fd8bd38c597c1ce45d36755171858f6ae4  nemo-0.1.0-rc.1-linux-amd64.tar
691ec3504870abaae445a0b865cfab2ef631de3b4d5a1e2776cd46caed729d85  nemo-0.1.0-rc.1-darwin-amd64.tar
83acf5abb7c7e9ca18c293cb5a9978dc94ad34b8705a5d0b35300281a7f17dd3  nemo-0.1.0-rc.1-darwin-arm64.tar
6d2e4858bcc226195996c8eb72b1c6228cc383f52d433fb79bd3bfb55f5b8f02  nemo-0.1.0-rc.1-windows-amd64.zip
```

The two package directories above are session-local artifacts. Recreate them with
the checked-in packaging script; a different Go toolchain may change the hashes.
No release tag, remote publication or final acceptance is implied.

## Remaining gates

A verified public-dataset subset and ground truth are needed to finish VAL-02's
public acceptance. DOC-01/QA-01 completion, teammate reviews (both teammates for
shared contract changes), native Windows runtime evidence, and an actual timed
spoken demo remain external requirements. Native macOS acceptance is now
recorded; see [macOS acceptance](macos-acceptance.md). The automated demo
workflows were rehearsed; the eight-minute presentation schedule is a plan, not
a recorded speaker rehearsal. A libtsk cross-check (INT-01) is deferred out of
scope for this release. Only then should `cmd/VERSION` become `0.1.0` and a
final tag and release be published.
