import copy
import unittest

from acceptance import verify_runtime_baseline


class RuntimeBaselineAcceptance(unittest.TestCase):
    def setUp(self):
        self.matrix = {"controlAPIs":[1, 2, 3, 4], "profiles":[{"description":9}, {"description":7}]}
        self.identity = {"descriptionVersion":10, "identityVersion":1}
        self.description = {"schemaVersion":10, "sdkIdentityVersion":1,
                            "configuredSdk":{"package":"@openai/codex-security", "version":"0.1.25"}}
        self.runtime = {"state":"ready", "controlAPI":4, "scannerDescriptionVersion":10,
                        "agentBuild":"fixture-release", "scannerBuild":"fixture-scanner"}

    def check(self, runtime=None, description=None):
        verify_runtime_baseline(runtime or self.runtime, self.matrix, description or self.description,
                                self.identity, "0.1.25", "fixture-release")

    def test_identity_description_is_independent_of_scan_profiles_and_order(self):
        self.check()
        self.matrix["profiles"].reverse()
        self.check()

    def test_old_scanner_cannot_pass_by_matching_the_latest_scan_profile(self):
        description = copy.deepcopy(self.description)
        description["schemaVersion"] = 9
        runtime = {**self.runtime, "scannerDescriptionVersion":9}
        with self.assertRaisesRegex(AssertionError, "identity baseline"):
            self.check(runtime, description)

    def test_reported_runtime_must_match_the_installed_scanner_and_release(self):
        for mutation in ({"scannerDescriptionVersion":9}, {"state":"blocked"}, {"controlAPI":3},
                         {"agentBuild":"old-release"}, {"scannerBuild":None}):
            with self.subTest(mutation=mutation), self.assertRaises(AssertionError):
                self.check(runtime={**self.runtime, **mutation})

    def test_sdk_pin_and_identity_capability_cannot_drift(self):
        for mutation in ({"sdkIdentityVersion":0},
                         {"configuredSdk":{"package":"@openai/codex-security", "version":"0.1.26"}}):
            with self.subTest(mutation=mutation), self.assertRaises(AssertionError):
                self.check(description={**self.description, **mutation})


if __name__ == "__main__":
    unittest.main()
