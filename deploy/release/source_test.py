import json
from pathlib import Path
import tempfile
import unittest

from source import export, verify


class SourceDeliveryGuards(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base/"source"
        self.root.mkdir()
        (self.root/"VERSION").write_text("0.1.0-beta.1\n")
        (self.root/"README.md").write_text("Fixture project\n")

    def put(self, name, content="fixture"):
        path = self.root/name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path

    def test_runtime_data_never_enters_source_and_sdk_replacements_do(self):
        for path in ["release/old/private.txt", "code-audit-agent/data/private.txt", "deploy/secret.env",
                     "deploy/.env.private", "code-audit-web/node_modules/secret", "contracts/go/bin/program",
                     "deploy/database.dump", "code-audit-scanner/auth.json", "code-audit-web/dist/app.js"]:
            self.put(path)
        for path in ["contracts/data/execution-compatibility.json", "deploy/sdk/overrides/dist/config.js",
                     "deploy/docker/.env.production.example", ".github/workflows/test.yml"]:
            self.put(path)
        data = export(self.root, self.base/"export")
        names = {item["path"] for item in data["files"]}
        self.assertEqual(names, {"VERSION", "README.md", "contracts/data/execution-compatibility.json",
                                "deploy/sdk/overrides/dist/config.js", "deploy/docker/.env.production.example",
                                ".github/workflows/test.yml"})

    def test_changed_omitted_and_unlisted_files_are_rejected(self):
        export(self.root, self.base/"export")
        other = self.base/"export/README.md"
        other.write_text("changed")
        with self.assertRaisesRegex(ValueError, "checksum"):
            verify(self.base/"export")
        other.unlink()
        with self.assertRaisesRegex(ValueError, "checksum"):
            verify(self.base/"export")
        other.write_text("Fixture project\n")
        (self.base/"export/unlisted.env").write_text("private")
        with self.assertRaisesRegex(ValueError, "tree differs"):
            verify(self.base/"export")

    def test_only_reviewed_legacy_sdk_replacements_survive_export(self):
        retained = "deploy/sdk/legacy/0.1.25/overrides/@openai/codex-security/dist/config.js"
        self.put(retained, "// reviewed legacy source\n")
        for path in ("deploy/sdk/legacy/0.1.25/dist/output.js",
                     "deploy/sdk/legacy/unreviewed/overrides/dist/config.js",
                     "deploy/sdk/legacy/0.1.25/overrides/auth.json",
                     "deploy/sdk/legacy/0.1.25/overrides/dist/private.key"):
            self.put(path, "private fixture")
        data = export(self.root, self.base/"export")
        self.assertEqual({item["path"] for item in data["files"]}, {"VERSION", "README.md", retained})
        verify(self.base/"export")

    def test_identity_credentials_browser_state_and_database_sidecars_are_excluded(self):
        for name in [".sdk-identities/agent/identity/credential", "state/.sdk-identities/private.json",
                     ".playwright-cli/page.yml", "auth.json", ".npmrc", ".netrc", "id_rsa", "id_ed25519",
                     "instance.sqlite", "instance.sqlite-shm", "instance.sqlite-wal", "instance.db-shm",
                     "instance.db-wal", "instance.sqlite3-shm", "instance.sqlite3-wal",
                     "database.dump", "identity.p12", "identity.pfx", "server.pem", "identity.key"]:
            self.put("code-audit-scanner/"+name, "private fixture")
        self.put(".gitattributes", "deploy/sdk/overrides/** -text\n")
        self.put("code-audit-scanner/src/main.ts", "// source fixture\n")
        data = export(self.root, self.base/"export")
        names = {item["path"] for item in data["files"]}
        self.assertEqual(names, {"VERSION", "README.md", ".gitattributes", "code-audit-scanner/src/main.ts"})

    def test_source_link_is_rejected_without_exporting_its_target(self):
        outside = self.base/"private"
        outside.write_text("private")
        self.put("deploy/placeholder")
        (self.root/"deploy/escape").symlink_to(outside)
        with self.assertRaisesRegex(ValueError, "link or special"):
            export(self.root, self.base/"export")
        self.assertFalse((self.base/"export").exists())

    def test_modified_manifest_cannot_escape_or_repeat_paths(self):
        export(self.root, self.base/"export")
        manifest = self.base/"export/SOURCE_MANIFEST.json"
        original = json.loads(manifest.read_text())
        for path in ["../outside", "/outside", "README.md/../README.md", "SOURCE_MANIFEST.json"]:
            changed = json.loads(json.dumps(original))
            changed["files"][0]["path"] = path
            manifest.write_text(json.dumps(changed))
            with self.assertRaisesRegex(ValueError, "Unsafe or duplicate"):
                verify(self.base/"export")

    def test_exports_are_deterministic_and_existing_output_is_preserved(self):
        export(self.root, self.base/"first", self.base/"first.tar.gz")
        export(self.root, self.base/"second", self.base/"second.tar.gz")
        self.assertEqual((self.base/"first.tar.gz").read_bytes(), (self.base/"second.tar.gz").read_bytes())
        before = (self.base/"first/README.md").read_bytes()
        with self.assertRaises(FileExistsError):
            export(self.root, self.base/"first")
        self.assertEqual((self.base/"first/README.md").read_bytes(), before)
        with self.assertRaisesRegex(ValueError, "outside"):
            export(self.root, self.root/"nested")


if __name__ == "__main__":
    unittest.main()
