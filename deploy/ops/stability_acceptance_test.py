import hashlib
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

from stability_acceptance import fixture_archive, validate_profile


class StabilityEvidenceGuards(unittest.TestCase):
    def test_short_or_unbounded_profiles_cannot_claim_stability(self):
        validate_profile(180,12000,48,6,1000)
        for args in [(60,12000,48,6,1000),(180,2,48,6,1000),(180,12000,1,6,1000),
                     (180,12000,48,1,1000),(180,12000,48,6,1),(100000,12000,48,6,1000)]:
            with self.subTest(args=args), self.assertRaises(ValueError): validate_profile(*args)

    def test_source_archive_hashes_every_file_and_is_reproducible(self):
        with tempfile.TemporaryDirectory() as directory:
            one,two = Path(directory)/"one.zip",Path(directory)/"two.zip"
            expected = fixture_archive(one,17,100000)
            self.assertEqual(expected,fixture_archive(two,17,100000))
            self.assertEqual(one.read_bytes(),two.read_bytes())
            tree = hashlib.sha256(); total=0
            with zipfile.ZipFile(one) as archive:
                marker = json.loads(archive.read("fixture-manifest.json"))
                names = sorted(name for name in archive.namelist() if name != "fixture-manifest.json")
                self.assertEqual(len(names),17)
                for name in names:
                    content=archive.read(name);total+=len(content)
                    self.assertIn(b"Fixtures / Mock",content)
                    tree.update(name.encode()+b"\0"+hashlib.sha256(content).hexdigest().encode()+b"\n")
                self.assertTrue(marker["fixtureOnly"])
                self.assertEqual(marker["inventory"],{"files":17,"bytes":total,"sha256":tree.hexdigest()})
                self.assertEqual(marker["inventory"],expected)


if __name__ == "__main__": unittest.main()
