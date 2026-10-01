#!/usr/bin/env python3
"""Build deterministic archives from the current source without publishing a tag."""
import argparse
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = (("linux", "amd64"), ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64"))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="new directory for release archives")
    args = parser.parse_args()
    version = (ROOT / "cmd/VERSION").read_text().strip()
    if not version or any(c not in "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ.-" for c in version):
        parser.error("invalid cmd/VERSION")
    args.output.mkdir(parents=True, exist_ok=False)
    checksums = []
    with tempfile.TemporaryDirectory(prefix="nemo-build-") as temp:
        for system, arch in TARGETS:
            name = "nemo.exe" if system == "windows" else "nemo"
            binary = Path(temp) / name
            env = dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED="0")
            subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-buildid=", "-o", str(binary), "."], cwd=ROOT, env=env, check=True)
            files = {name: binary.read_bytes()}
            for doc in ("LICENSE", "CHANGELOG.md", "docs/release/README.md", "docs/release/demo.md"):
                files[Path(doc).name if doc != "docs/release/README.md" else "RELEASE.md"] = (ROOT / doc).read_bytes()
            stem = f"nemo-{version}-{system}-{arch}"
            if system == "windows":
                archive = args.output / (stem + ".zip")
                with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as out:
                    for filename, data in sorted(files.items()):
                        info = zipfile.ZipInfo(stem + "/" + filename, (1980, 1, 1, 0, 0, 0))
                        info.compress_type = zipfile.ZIP_DEFLATED
                        info.create_system = 3
                        info.external_attr = (0o100755 if filename == name else 0o100644) << 16
                        out.writestr(info, data)
            else:
                archive = args.output / (stem + ".tar")
                with tarfile.open(archive, "w", format=tarfile.USTAR_FORMAT) as out:
                    for filename, data in sorted(files.items()):
                        info = tarfile.TarInfo(stem + "/" + filename)
                        info.size = len(data)
                        info.mode = 0o755 if filename == name else 0o644
                        info.mtime = 0
                        out.addfile(info, io.BytesIO(data))
            checksums.append(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}")
    (args.output / "SHA256SUMS").write_text("\n".join(checksums) + "\n")
    print("\n".join(checksums))


if __name__ == "__main__":
    main()
