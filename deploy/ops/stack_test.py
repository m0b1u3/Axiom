import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest

from stack import APP_SERVICES, Stack, digest, env_bytes, init_env, private_write, read_env, restore, retarget_env, verify_backup


class BackupGuards(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        init_env(self.root/"runtime.env", "axiom-accept-unit", 0)
        (self.root/"database.dump").write_bytes(b"Fixture only: database archive")

    def manifest(self, member=None):
        with tarfile.open(self.root/"data.tar.gz", "w:gz") as archive:
            item = member or tarfile.TarInfo("snapshots/fixture.ts")
            if item.isfile():
                item.size = 7
                archive.addfile(item, io.BytesIO(b"fixture"))
            else:
                archive.addfile(item)
        data = {"schemaVersion":1, "project":"axiom-accept-unit",
                "images":{s:{"imageId":"sha256:"+"a"*64, "reference":"fixture/"+s+":unit"} for s in APP_SERVICES},
                "files":{s:{"sizeBytes":(self.root/s).stat().st_size, "sha256":digest(self.root/s)} for s in ("runtime.env","data.tar.gz","database.dump")}}
        (self.root/"manifest.json").write_text(json.dumps(data))
        return data

    def test_corrupt_backup_refused_before_new_configuration(self):
        self.manifest()
        verify_backup(self.root)
        with (self.root/"database.dump").open("ab") as file:
            file.write(b"corruption")
        with self.assertRaisesRegex(ValueError, "checksum"):
            restore(self.root, self.root/"new.env", "axiom-accept-new", 0, [])
        self.assertFalse((self.root/"new.env").exists())

    def test_unsafe_paths_and_links_refused(self):
        for name, kind in [("../escape",tarfile.REGTYPE), ("/outside",tarfile.REGTYPE), ("sdk/link",tarfile.SYMTYPE), ("sdk/hardlink",tarfile.LNKTYPE)]:
            with self.subTest(name=name):
                item = tarfile.TarInfo(name)
                item.type = kind
                item.linkname = "/outside"
                self.manifest(item)
                with self.assertRaisesRegex(ValueError, "unsafe path"):
                    verify_backup(self.root)

    def test_original_project_refused_without_creating_config(self):
        self.manifest()
        with self.assertRaisesRegex(ValueError, "unused project"):
            restore(self.root, self.root/"new.env", "axiom-accept-unit", 0, [])
        self.assertFalse((self.root/"new.env").exists())

    def test_environment_not_overwritten_and_external_db_refused(self):
        before = (self.root/"runtime.env").read_bytes()
        with self.assertRaises(FileExistsError):
            init_env(self.root/"runtime.env", "another-project", 0)
        self.assertEqual((self.root/"runtime.env").read_bytes(), before)
        if os.name != "nt":
            self.assertEqual((self.root/"runtime.env").stat().st_mode & 0o777, 0o600)
        values = read_env(self.root/"runtime.env")
        values["AXIOM_DATABASE_URL"] = "postgres://fixture:fixture@external:5432/wrong"
        private_write(self.root/"external.env", env_bytes(values))
        with self.assertRaisesRegex(ValueError, "matching POSTGRES"):
            Stack(self.root/"external.env")

    def test_invalid_metadata_returns_controlled_error(self):
        manifest = self.manifest()
        manifest["images"]["server"]["imageId"] = 0
        (self.root/"manifest.json").write_text(json.dumps(manifest))
        with self.assertRaisesRegex(ValueError, "image identity"):
            verify_backup(self.root)

    def test_release_identity_is_explicit_and_cannot_inject_environment(self):
        candidate = self.root/"candidate.env"
        init_env(candidate, "axiom-accept-candidate", 0, "0.1.0-beta.1")
        self.assertEqual(read_env(candidate)["AXIOM_RELEASE"], "0.1.0-beta.1")
        for value in ("release\nAXIOM_SCAN_MOCK=1", "a/b", "$(command)", "-bad", ""):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "image tag"):
                init_env(self.root/"rejected.env", "axiom-accept-candidate", 0, value)
        self.assertFalse((self.root/"rejected.env").exists())

    def test_retarget_preserves_keys_and_original_configuration(self):
        source = self.root/"runtime.env"
        with source.open("a") as output:
            output.write("# Literal secret quoting must survive image selection.\nAXIOM_ADMIN_PASSWORD='literal$fixture-password'\n")
        original = source.read_bytes()
        destination = retarget_env(source, self.root/"candidate.env", "0.1.0-beta.1")
        before, after = read_env(source), read_env(destination)
        self.assertEqual({k:v for k,v in before.items() if k != "AXIOM_RELEASE"},
                         {k:v for k,v in after.items() if k != "AXIOM_RELEASE"})
        self.assertEqual(source.read_bytes(), original)
        with self.assertRaises(FileExistsError):
            retarget_env(source, destination, "0.1.0-beta.2")
        with self.assertRaisesRegex(ValueError, "image tag"):
            retarget_env(source, self.root/"rejected.env", "latest")


if __name__ == "__main__":
    unittest.main()
