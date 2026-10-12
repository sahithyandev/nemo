#!/usr/bin/env python3
"""Explicitly fetch and verify one pinned public corpus; detection never downloads."""
import argparse
import gzip
import hashlib
import shutil
from pathlib import Path
import urllib.request

REVISION = "decd14bd9cb39a978a3a2ce0d2a0d472ac88b703"
BASE = "https://raw.githubusercontent.com/fkie-cad/hide-and-seek-dataset/" + REVISION + "/scenario_4_additional_data_units/ntfs_ads/"
FILES = {
    "metadata.json": (626, "749ebe42809dcd2fdffd9be8dfad8ecf1ef790ef8dce9836a7249374d9bddb7a"),
    "image.img.gz": (1706438, "9d90e539ea2d40aabf3153773afb2cfc8e1a5ad74f1643aa49a67fa5bfdb8a33"),
}
IMAGE_SIZE = 1073741824
IMAGE_HASH = "ac31a4bb2d07b0b0d23c31a8501f389758b17df7739ac92fa13edd2e0d7da790"


def prepare(root):
    if root.exists():
        raise ValueError("output directory must not exist")
    if not root.parent.is_dir():
        raise ValueError("output parent directory must exist")
    if shutil.disk_usage(root.parent).free < IMAGE_SIZE + 16 * 1024 * 1024:
        raise ValueError("at least 1 GiB plus 16 MiB free space required")
    target = root / "ntfs_ads"
    target.mkdir(parents=True)
    for name, (size, digest) in FILES.items():
        h = hashlib.sha256()
        count = 0
        with urllib.request.urlopen(BASE + name, timeout=60) as source, (target / name).open("xb") as output:
            while chunk := source.read(1024 * 1024):
                count += len(chunk)
                if count > size:
                    raise ValueError("download exceeds pinned size: " + name)
                h.update(chunk)
                output.write(chunk)
        if count != size or h.hexdigest() != digest:
            raise ValueError("download checksum/size mismatch: " + name)
    h = hashlib.sha256()
    count = 0
    with gzip.open(target / "image.img.gz", "rb") as source, (target / "image.img").open("xb") as output:
        while chunk := source.read(1024 * 1024):
            count += len(chunk)
            if count > IMAGE_SIZE:
                raise ValueError("decompressed image exceeds pinned size")
            h.update(chunk)
            # Sparse output preserves exact logical bytes without storing zero runs.
            if chunk == bytes(len(chunk)):
                output.seek(len(chunk), 1)
            else:
                output.write(chunk)
        output.truncate(count)
    if count != IMAGE_SIZE or h.hexdigest() != IMAGE_HASH:
        raise ValueError("decompressed image checksum/size mismatch")
    print("Prepared verified read-only validation input:", root)
    print("Corpus discrepancies are documented in docs/validation/public-ntfs-ads.md")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Explicit download: 1.7 MB compressed, 1 GiB logical raw NTFS image.")
    parser.add_argument("output", type=Path, help="new disposable directory under an existing parent")
    args = parser.parse_args()
    try:
        prepare(args.output)
    except (ValueError, OSError) as error:
        parser.exit(1, str(error) + "\n")
