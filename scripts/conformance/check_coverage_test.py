import importlib.util
import io
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("coverage_check", Path(__file__).with_name("check-coverage.py"))
coverage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(coverage)


class CoveragePolicyTests(unittest.TestCase):
    policy = {"minimum_assertions": 2, "allowed_skips": [{"class": "assertion.points", "name": "curve", "maximum": 1, "reason_contains": "point geometry"}]}

    def run_check(self, content):
        return coverage.check(io.StringIO("<testsuite>" + content + "</testsuite>"), self.policy)

    def test_reviewed_skip_and_pass(self):
        result = self.run_check('<testcase classname="assertion.points" name="curve"><skipped message="point geometry"/></testcase><testcase classname="assertion.bbox" name="bbox"/>')
        self.assertTrue(result["passed"])

    def test_lost_assertions_cannot_look_like_zero_skips(self):
        self.assertFalse(self.run_check('<testcase classname="assertion.bbox" name="bbox"/>')["passed"])

    def test_new_skip_or_changed_reason_fails(self):
        for class_name, reason in (("assertion.bbox", "empty"), ("assertion.points", "setup failed")):
            result = self.run_check(f'<testcase classname="{class_name}" name="curve"><skipped message="{reason}"/></testcase><testcase classname="assertion.bbox" name="bbox"/>')
            self.assertFalse(result["passed"])


if __name__ == "__main__":
    unittest.main()
