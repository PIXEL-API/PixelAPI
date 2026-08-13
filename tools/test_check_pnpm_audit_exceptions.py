import importlib.util
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = REPOSITORY_ROOT / "tools" / "check_pnpm_audit_exceptions.py"
SPEC = importlib.util.spec_from_file_location("check_pnpm_audit_exceptions", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"Unable to load audit checker from {MODULE_PATH}")
AUDIT_CHECKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(AUDIT_CHECKER)


def audit_payload(*, high=0, critical=0, advisories=None):
    return {
        "advisories": advisories or {},
        "metadata": {
            "vulnerabilities": {
                "info": 0,
                "low": 0,
                "moderate": 0,
                "high": high,
                "critical": critical,
            },
            "totalDependencies": 1,
        },
    }


class ValidateAuditPayloadTest(unittest.TestCase):
    def test_accepts_consistent_zero_high_and_critical_counts(self):
        self.assertEqual(AUDIT_CHECKER.validate_audit_payload(audit_payload()), [])

    def test_rejects_result_without_metadata(self):
        self.assertEqual(
            AUDIT_CHECKER.validate_audit_payload({"advisories": {}}),
            ["Audit output is missing metadata"],
        )

    def test_rejects_high_count_without_matching_advisory(self):
        self.assertEqual(
            AUDIT_CHECKER.validate_audit_payload(audit_payload(high=1)),
            [
                "Audit metadata/result count mismatch for high: "
                "metadata=1, results=0"
            ],
        )

    def test_rejects_advisory_missing_from_metadata_count(self):
        payload = audit_payload(
            advisories={
                "1": {
                    "module_name": "example",
                    "severity": "critical",
                    "github_advisory_id": "GHSA-example",
                }
            }
        )
        self.assertEqual(
            AUDIT_CHECKER.validate_audit_payload(payload),
            [
                "Audit metadata/result count mismatch for critical: "
                "metadata=0, results=1"
            ],
        )

    def test_accepts_matching_high_advisory_count(self):
        payload = audit_payload(
            high=1,
            advisories={
                "1": {
                    "module_name": "example",
                    "severity": "high",
                    "github_advisory_id": "GHSA-example",
                }
            },
        )
        self.assertEqual(AUDIT_CHECKER.validate_audit_payload(payload), [])

    def test_counts_packages_for_new_vulnerability_shape(self):
        payload = audit_payload()
        payload.pop("advisories")
        payload["vulnerabilities"] = {
            "example": {
                "severity": "critical",
                "via": ["GHSA-example", "GHSA-second-example"],
            }
        }
        payload["metadata"]["vulnerabilities"]["critical"] = 1
        self.assertEqual(AUDIT_CHECKER.validate_audit_payload(payload), [])

    def test_rejects_ambiguous_result_shapes(self):
        payload = audit_payload()
        payload["vulnerabilities"] = {}
        self.assertEqual(
            AUDIT_CHECKER.validate_audit_payload(payload),
            ["Audit output has ambiguous advisories/vulnerabilities results"],
        )


if __name__ == "__main__":
    unittest.main()
