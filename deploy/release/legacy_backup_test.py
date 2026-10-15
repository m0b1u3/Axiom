import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from source import ROOT


class LegacyBackupConfiguration(unittest.TestCase):
    def test_missing_configuration_never_contacts_a_development_host(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            script = root/"pull-vps-backup.sh"
            shutil.copyfile(ROOT/"deploy/docker/pull-vps-backup.sh", script)
            tools = root/"bin"
            tools.mkdir()
            marker = root/"network-attempted"
            for name in ("ssh", "scp"):
                tool = tools/name
                tool.write_text('#!/bin/sh\ntouch "'+str(marker)+'"\nexit 99\n')
                tool.chmod(0o755)
            base = {key:value for key,value in os.environ.items() if not key.startswith("AXIOM_VPS_")}
            base["PATH"] = str(tools)+os.pathsep+base.get("PATH", "")
            for config in ({}, {"AXIOM_VPS_HOST":"operator@fixture.invalid"},
                           {"AXIOM_VPS_HOST":"operator@fixture.invalid", "AXIOM_VPS_KEY":str(root/"missing-key")}):
                with self.subTest(config=list(config)):
                    result = subprocess.run([shutil.which("bash"), str(script)], env={**base, **config},
                                            capture_output=True, text=True, timeout=5)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("AXIOM_VPS_", result.stderr)
                    self.assertFalse(marker.exists(), "legacy helper attempted a network request without explicit configuration")
                    self.assertFalse((root/"backups").exists())
