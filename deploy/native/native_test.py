import base64
import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest

HERE = Path(__file__).parent


def module(name):
    spec = importlib.util.spec_from_file_location(name, HERE / (name + ".py"))
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


fetcher = module("fetch")
inventory = module("platform_manifest")


class NativeInputTests(unittest.TestCase):
    def test_cached_asset_must_match_lock_and_cannot_be_a_link(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "runtime.zip"
            path.write_bytes(b"verified input")
            asset = {"filename": path.name, "url": "https://example.invalid/runtime.zip",
                     "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
            self.assertEqual(fetcher.fetch(asset, directory), path)
            path.write_bytes(b"changed")
            with self.assertRaisesRegex(ValueError, "does not match"):
                fetcher.fetch(asset, directory)
            path.unlink()
            try:
                path.symlink_to(Path(directory) / "missing-input")
            except (OSError, NotImplementedError):
                return
            with self.assertRaisesRegex(ValueError, "cannot be a link"):
                fetcher.fetch(asset, directory)

    def test_unsafe_filename_and_unpinned_dependency_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            for filename, checksum in [("../escape.zip", "a" * 64), ("runtime.zip", "latest")]:
                with self.assertRaises(ValueError):
                    fetcher.fetch({"filename": filename, "sha256": checksum, "url": "https://example.invalid/a"}, directory)

    def test_archive_integrity_precedes_inventory_and_paths_are_checked(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "package.tgz"
            def archive(name):
                with tarfile.open(path, "w:gz") as stream:
                    for filename, data in [("package/package.json", b'{"name":"example","version":"1"}'), (name, b"native executable")]:
                        item = tarfile.TarInfo(filename)
                        item.size = len(data)
                        stream.addfile(item, io.BytesIO(data))
                return "sha512-" + base64.b64encode(hashlib.sha512(path.read_bytes()).digest()).decode()
            sri = archive("package/bin/runtime.exe")
            result = inventory.npm_inventory(path, "example", "example-win32", "win32-x64", sri)
            self.assertEqual(len(result["files"]), 2)
            with self.assertRaisesRegex(ValueError, "integrity mismatch"):
                inventory.npm_inventory(path, "example", "example", "win32-x64", "sha512-incorrect")
            sri = archive("package/../escape.exe")
            with self.assertRaisesRegex(ValueError, "Unsafe"):
                inventory.npm_inventory(path, "example", "example", "win32-x64", sri)


if __name__ == "__main__":
    unittest.main()
