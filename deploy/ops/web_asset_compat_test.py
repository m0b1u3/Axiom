from pathlib import Path
import hashlib
import tempfile
import unittest
from web_asset_compat import digest, retain_assets


class WebAssetCompatibility(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.old, self.new = self.root/"previous/assets", self.root/"candidate/assets"
        self.old.mkdir(parents=True)
        self.new.mkdir(parents=True)

    def test_old_lazy_modules_survive_and_new_entry_and_modules_are_preserved(self):
        (self.old/"NewScan-old.js").write_text("Fixtures / Mock old lazy module")
        (self.new/"NewScan-new.js").write_text("Fixtures / Mock new lazy module")
        (self.old/"shared.js").write_text("same")
        (self.new/"shared.js").write_text("same")
        index = self.new.parent/"index.html"
        index.write_text("new candidate entry")
        result = retain_assets(self.old, self.new)
        self.assertEqual(result["retainedAssets"], 1)
        self.assertEqual((self.new/"NewScan-old.js").read_text(), "Fixtures / Mock old lazy module")
        self.assertEqual((self.new/"NewScan-new.js").read_text(), "Fixtures / Mock new lazy module")
        self.assertEqual(index.read_text(), "new candidate entry")
        self.assertEqual(retain_assets(self.old, self.new)["retainedAssets"], 0)

    def test_name_collision_rejects_before_any_copy(self):
        (self.old/"a-old.js").write_text("old")
        (self.old/"z-collision.js").write_text("different")
        (self.new/"z-collision.js").write_text("candidate")
        with self.assertRaisesRegex(ValueError, "collision"):
            retain_assets(self.old, self.new)
        self.assertFalse((self.new/"a-old.js").exists())
        self.assertEqual((self.new/"z-collision.js").read_text(), "candidate")

    def test_links_and_overlapping_trees_are_rejected(self):
        (self.old/"link.js").symlink_to(self.new)
        with self.assertRaisesRegex(ValueError, "links"):
            retain_assets(self.old, self.new)
        (self.old/"link.js").unlink()
        with self.assertRaisesRegex(ValueError, "independent"):
            retain_assets(self.old, self.old)

    def test_large_asset_hash_covers_every_block(self):
        content = b"Fixtures / Mock public asset" * 100000
        path = self.old/"large.js"
        path.write_bytes(content)
        self.assertEqual(digest(path), hashlib.sha256(content).hexdigest())


if __name__ == "__main__":
    unittest.main()
