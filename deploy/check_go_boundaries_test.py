import unittest

from check_go_boundaries import decode_stream, violations


class BoundaryPolicyTests(unittest.TestCase):
    def test_server_cannot_import_agent_even_from_tests(self):
        owner = "axiom/code-audit-server"
        self.assertTrue(violations(owner, [{"ImportPath": "axiom/code-audit-agent/pkg/execution"}], []))
        self.assertTrue(violations(owner, [], [{"Path": "axiom/code-audit-agent"}]))

    def test_agent_rejects_transitive_sql_and_unused_database_modules(self):
        owner = "axiom/code-audit-agent"
        for name in ("database/sql", "github.com/jackc/pgx/v5/stdlib", "modernc.org/sqlite"):
            with self.subTest(name=name):
                self.assertTrue(violations(owner, [{"ImportPath": name}], []))
        self.assertTrue(violations(owner, [], [{"Path": "modernc.org/sqlite"}]))

    def test_contracts_cannot_pull_application_or_database_implementations(self):
        owner = "axiom/contracts"
        for name in ("axiom/code-audit-server/internal/store", "axiom/code-audit-agent/pkg/execution", "axiom/sourceidentity"):
            self.assertTrue(violations(owner, [{"ImportPath": name}], []))

    def test_allowed_shared_dtos_and_server_database(self):
        packages = [{"ImportPath": "axiom/contracts/control"}, {"ImportPath": "axiom/sourceidentity"}, {"ImportPath": "net/http", "Standard": True}]
        self.assertFalse(violations("axiom/code-audit-agent", packages, [{"Path": "axiom/contracts"}]))
        self.assertFalse(violations("axiom/code-audit-server", packages + [{"ImportPath": "modernc.org/sqlite"}], []))

    def test_shared_hashing_stays_dependency_free(self):
        self.assertTrue(violations("axiom/sourceidentity", [{"ImportPath": "axiom/contracts"}], []))
        self.assertFalse(violations("axiom/sourceidentity", [{"ImportPath": "crypto/sha256", "Standard": True}], []))

    def test_reads_go_json_stream(self):
        self.assertEqual(list(decode_stream('  {"Path":"a"}\n\n{"Path":"b"}\n')), [{"Path": "a"}, {"Path": "b"}])

    def test_go_test_package_variants_keep_the_same_owner(self):
        self.assertFalse(violations("axiom/contracts", [{"ImportPath": "axiom/contracts [axiom/contracts.test]"}, {"ImportPath": "axiom/contracts.test"}], []))
        self.assertTrue(violations("axiom/code-audit-server", [{"ImportPath": "axiom/code-audit-agent [axiom/code-audit-agent.test]"}], []))


if __name__ == "__main__":
    unittest.main()
