import copy
import json
import unittest
from unittest.mock import patch

from check import WORKFLOWS, remote_check


class RemoteEvidenceGuards(unittest.TestCase):
    def setUp(self):
        self.revision = "a"*40
        self.runs = [{"path":".github/workflows/"+name, "head_sha":self.revision,
                      "run_number":1, "status":"completed", "conclusion":"success",
                      "html_url":"https://github.com/fixture/project/actions/runs/"+str(index)}
                     for index, name in enumerate(WORKFLOWS, start=1)]

    def check(self, runs=None, enabled=True):
        def output(command, **kwargs):
            return json.dumps({"enabled":enabled} if command[-1].endswith("private-vulnerability-reporting")
                              else {"workflow_runs":self.runs if runs is None else runs})
        with patch("check.subprocess.check_output", side_effect=output):
            return remote_check("https://github.com/fixture/project", self.revision)

    def test_all_five_exact_revision_runs_are_required(self):
        result = self.check()
        self.assertEqual(len(result["workflows"]), 5)
        with self.assertRaisesRegex(ValueError, "No remote run"):
            self.check(self.runs[:-1])

    def test_another_commit_cannot_supply_success_evidence(self):
        runs = copy.deepcopy(self.runs)
        runs[0]["head_sha"] = "b"*40
        with self.assertRaisesRegex(ValueError, "exact commit"):
            self.check(runs)

    def test_newer_failed_or_pending_run_overrides_old_success(self):
        for status, conclusion in [("completed", "failure"), ("in_progress", None), ("completed", "cancelled")]:
            runs = copy.deepcopy(self.runs)
            newer = dict(runs[0], run_number=2, status=status, conclusion=conclusion)
            with self.subTest(status=status, conclusion=conclusion), self.assertRaisesRegex(ValueError, "has not passed"):
                self.check(runs+[newer])

    def test_private_channel_must_be_enabled(self):
        with self.assertRaisesRegex(ValueError, "not enabled"):
            self.check(enabled=False)

    def test_short_revision_cannot_be_accepted(self):
        with self.assertRaisesRegex(ValueError, "full commit"):
            remote_check("https://github.com/fixture/project", "abc123")


if __name__ == "__main__":
    unittest.main()
