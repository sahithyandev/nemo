"""Offline regressions for bounded, verified public fixture preparation."""
import gzip
import hashlib
import importlib.util
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("prepare_public", Path(__file__).with_name("prepare-public-validation.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PreparationTest(unittest.TestCase):
    def run_fixture(self, data, payload=None, image_size=None):
        compressed = gzip.compress(data)
        files = {"image.img.gz": (len(compressed), hashlib.sha256(compressed).hexdigest())}
        with tempfile.TemporaryDirectory() as parent:
            output = Path(parent) / "dataset"
            with patch.object(module, "FILES", files), patch.object(module, "IMAGE_SIZE", len(data) if image_size is None else image_size), patch.object(module, "IMAGE_HASH", hashlib.sha256(data).hexdigest()), patch.object(module.urllib.request, "urlopen", return_value=io.BytesIO(compressed if payload is None else payload)):
                module.prepare(output)
            self.assertEqual((output / "ntfs_ads/image.img").read_bytes(), data)

    def test_verified_logical_bytes_including_trailing_zeros(self):
        self.run_fixture(b"NTFS" + bytes(2 * 1024 * 1024))

    def test_oversize_download_rejected(self):
        data = b"fixture"
        with self.assertRaisesRegex(ValueError, "exceeds pinned size"):
            self.run_fixture(data, gzip.compress(data) + b"extra")

    def test_bad_checksum_rejected(self):
        data = b"fixture"
        compressed = gzip.compress(data)
        with self.assertRaisesRegex(ValueError, "checksum/size mismatch"):
            self.run_fixture(data, bytes([compressed[0] ^ 1]) + compressed[1:])

    def test_decompression_bound_rejected(self):
        with self.assertRaisesRegex(ValueError, "exceeds pinned size"):
            self.run_fixture(b"fixture", image_size=2)

    def test_existing_directory_refused_without_network(self):
        with tempfile.TemporaryDirectory() as parent, patch.object(module.urllib.request, "urlopen") as network:
            with self.assertRaisesRegex(ValueError, "must not exist"):
                module.prepare(Path(parent))
            network.assert_not_called()


if __name__ == "__main__":
    unittest.main()
