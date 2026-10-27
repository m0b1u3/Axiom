import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from check import WORKFLOWS, check_python_syntax, check_real_sdk_acceptance, git_inventory, remote_check


class ScopedAcceptanceGuards(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.path = self.root/"docs/acceptance/deepseek-fixture.json"
        self.path.parent.mkdir(parents=True)
        self.evidence = {
            "schemaVersion": 1, "kind": "scoped-real-model-acceptance",
            "status": "passed_for_recorded_deployment", "candidateRelationship": "historical_workflow_evidence",
            "provider": "deepseek", "sdkVersion": "0.5.0", "wireAPI": "responses",
            "reviewOutputMode": "native_schema", "reviewStrictSchema": True,
            "workflow": {k: "passed" for k in ("scan", "reviewReport", "sourceReferences", "formalArchive", "evidenceGraph")},
            "requests": {"scan": 2, "review": 1, "total": 3, "failed": 0, "automaticRetries": 0},
            "currentCandidateRealModelAcceptance": "not-run", "configuredPricingRealAcceptance": "not-run",
            "providerInvoiceReconciled": False, "limitations": ["Fixtures / Mock evidence for release-check unit tests only"],
        }
        self.release = {"realSDKAcceptance": "limited", "currentCandidateRealModelAcceptance": "not-run",
                        "configuredPricingRealAcceptance": "not-run"}
        self.write_evidence()

    def write_evidence(self):
        self.path.write_text(json.dumps(self.evidence))
        self.release["realSDKAcceptanceEvidence"] = {
            "path": self.path.relative_to(self.root).as_posix(),
            "sha256": hashlib.sha256(self.path.read_bytes()).hexdigest(),
        }

    def test_scoped_history_is_reported_as_limited_and_new_pricing_is_not_certified(self):
        result = check_real_sdk_acceptance(self.root, self.release)
        self.assertEqual(result["status"], "limited")
        self.assertEqual(result["configuredPricingRealAcceptance"], "not-run")
        self.assertEqual(check_real_sdk_acceptance(self.root, {"realSDKAcceptance": "not-run"}), {"status": "not-run"})

    def test_missing_or_changed_evidence_cannot_supply_acceptance(self):
        self.path.write_text("{}")
        with self.assertRaisesRegex(ValueError, "SHA-256"):
            check_real_sdk_acceptance(self.root, self.release)
        self.path.unlink()
        with self.assertRaisesRegex(ValueError, "missing"):
            check_real_sdk_acceptance(self.root, self.release)

    def test_unknown_global_pass_states_and_private_paths_are_rejected(self):
        for status in ("passed", "complete", "unknown"):
            with self.subTest(status=status), self.assertRaises(ValueError):
                check_real_sdk_acceptance(self.root, dict(self.release, realSDKAcceptance=status))
        self.release["realSDKAcceptanceEvidence"]["path"] = "../private/acceptance.json"
        with self.assertRaisesRegex(ValueError, "source-controlled"):
            check_real_sdk_acceptance(self.root, self.release)

    def test_report_cannot_claim_chat_json_mode_or_unverified_workflow(self):
        for key, value in (("wireAPI", "chat"), ("reviewOutputMode", "json_object"), ("reviewStrictSchema", False)):
            before = copy.deepcopy(self.evidence)
            self.evidence[key] = value
            self.write_evidence()
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "scope"):
                check_real_sdk_acceptance(self.root, self.release)
            self.evidence = before
        self.evidence["workflow"]["formalArchive"] = "not-run"
        self.write_evidence()
        with self.assertRaisesRegex(ValueError, "incomplete workflow"):
            check_real_sdk_acceptance(self.root, self.release)

    def test_inconsistent_counts_or_retries_cannot_supply_this_evidence(self):
        for key, value in (("total", 4), ("scan", True), ("failed", 1), ("automaticRetries", 1)):
            before = copy.deepcopy(self.evidence)
            self.evidence["requests"][key] = value
            self.write_evidence()
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "counts"):
                check_real_sdk_acceptance(self.root, self.release)
            self.evidence = before

    def test_historical_evidence_cannot_certify_new_candidate_pricing_or_billing(self):
        for document in (self.release, self.evidence):
            for key in ("currentCandidateRealModelAcceptance", "configuredPricingRealAcceptance"):
                document[key] = "passed"
                self.write_evidence()
                with self.subTest(key=key), self.assertRaisesRegex(ValueError, "cannot certify"):
                    check_real_sdk_acceptance(self.root, self.release)
                document[key] = "not-run"
        self.evidence["providerInvoiceReconciled"] = True
        self.write_evidence()
        with self.assertRaisesRegex(ValueError, "limitations"):
            check_real_sdk_acceptance(self.root, self.release)


class RemoteEvidenceGuards(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root/"VERSION").write_text("0.1.0-beta.1\n")
        (self.root/"README.md").write_text("Fixture source\n")
        self.revision = "a"*40
        self.runs = [{"path":".github/workflows/"+name, "head_sha":self.revision,
                      "run_number":1, "status":"completed", "conclusion":"success",
                      "html_url":"https://github.com/fixture/project/actions/runs/"+str(index)}
                     for index, name in enumerate(WORKFLOWS, start=1)]

    def check(self, runs=None, enabled=True, wrong_tree=False):
        def output(command, **kwargs):
            if command[-1].endswith("private-vulnerability-reporting"):
                return json.dumps({"enabled":enabled})
            if "/git/trees/" in command[-1]:
                rows = [{"path":name, "type":"blob", **entry} for name,entry in git_inventory(self.root).items()]
                if wrong_tree:
                    rows[0]["sha"] = "b"*40
                return json.dumps({"tree":rows,"truncated":False})
            return json.dumps({"workflow_runs":self.runs if runs is None else runs})
        with patch("check.subprocess.check_output", side_effect=output):
            return remote_check("https://github.com/fixture/project", self.revision, self.root)

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

    def test_successful_ci_for_different_local_source_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "does not match"):
            self.check(wrong_tree=True)

    def test_deployment_syntax_is_checked_without_executing_the_script(self):
        folder = self.root/"deploy/ops"
        folder.mkdir(parents=True)
        script = folder/"acceptance.py"
        script.write_text("raise RuntimeError('A release check must not execute deployment code')\n")
        check_python_syntax(self.root)
        script.write_text("try:\n    pass\nfinally:\npass\n")
        with self.assertRaisesRegex(ValueError, "Invalid deployment Python"):
            check_python_syntax(self.root)


if __name__ == "__main__":
    unittest.main()
