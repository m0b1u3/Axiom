import copy
import unittest

from acceptance import verify_runtime_baseline


class RuntimeBaselineAcceptance(unittest.TestCase):
    def setUp(self):
        self.matrix = {"controlAPIs":[1, 2, 3, 4], "profiles":[{"description":9}, {"description":7}]}
        self.identity = {"descriptionVersion":10, "identityVersion":1}
        self.parameters = {"descriptionVersion":11, "parametersVersion":1, "sdkVersion":"0.1.25"}
        self.concurrency = {"descriptionVersion":12, "concurrencyVersion":1, "sdkVersion":"0.1.25"}
        self.description = {"schemaVersion":12, "sdkIdentityVersion":1, "sdkParametersVersion":1, "sdkConcurrencyVersion":1,
                            "configuredSdk":{"package":"@openai/codex-security", "version":"0.1.25"}}
        self.runtime = {"state":"ready", "controlAPI":4, "scannerDescriptionVersion":12,
                        "agentBuild":"fixture-release", "scannerBuild":"fixture-scanner"}

    def check(self, runtime=None, description=None):
        verify_runtime_baseline(runtime or self.runtime, self.matrix, description or self.description,
                                self.identity, self.parameters, self.concurrency, "0.1.25", "fixture-release")

    def test_sdk_capability_descriptions_are_independent_of_scan_profiles_and_order(self):
        self.check()
        self.matrix["profiles"].reverse()
        self.check()

    def test_old_scanner_cannot_pass_by_matching_the_latest_scan_profile(self):
        for version in [9, 10, 11, 13]:
            description = {**self.description, "schemaVersion":version}
            runtime = {**self.runtime, "scannerDescriptionVersion":version}
            with self.subTest(version=version), self.assertRaisesRegex(AssertionError, "SDK capability baselines"):
                self.check(runtime, description)

    def test_reported_runtime_must_match_the_installed_scanner_and_release(self):
        for mutation in ({"scannerDescriptionVersion":9}, {"state":"blocked"}, {"controlAPI":3},
                         {"agentBuild":"old-release"}, {"scannerBuild":None}):
            with self.subTest(mutation=mutation), self.assertRaises(AssertionError):
                self.check(runtime={**self.runtime, **mutation})

    def test_sdk_pin_and_capability_versions_cannot_drift(self):
        for mutation in ({"sdkIdentityVersion":0},
                         {"sdkParametersVersion":0}, {"sdkParametersVersion":2},
                         {"sdkConcurrencyVersion":0}, {"sdkConcurrencyVersion":2},
                         {"configuredSdk":{"package":"@openai/codex-security", "version":"0.1.26"}}):
            with self.subTest(mutation=mutation), self.assertRaises(AssertionError):
                self.check(description={**self.description, **mutation})

    def test_missing_sdk_capability_or_unpinned_metadata_is_rejected(self):
        for key in ["sdkParametersVersion", "sdkConcurrencyVersion"]:
            description = copy.deepcopy(self.description)
            del description[key]
            with self.subTest(key=key), self.assertRaises(KeyError):
                self.check(description=description)
        for metadata in [self.parameters, self.concurrency]:
            metadata["sdkVersion"] = "0.1.26"
            with self.assertRaisesRegex(AssertionError, "pinned dependency"):
                self.check()
            metadata["sdkVersion"] = "0.1.25"


if __name__ == "__main__":
    unittest.main()
