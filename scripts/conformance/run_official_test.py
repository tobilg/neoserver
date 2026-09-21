"""Exercise the stock runner and coverage gate without downloading ETS images."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class OfficialRunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="neoserver-ets-runner-")
        self.root = Path(self.temp.name)
        for name in ("scripts/conformance/run-official.sh", "scripts/conformance/check-coverage.py",
                     "testing/officialets/coverage-policy.json"):
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, target)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.command("go", '''
import sys
if "profiles" in sys.argv:
    print("core\\t{}\\ncrs\\t{}")
else:
    print("wcs20\\tfixture-image")
''')
        self.command("docker", '''
import json
import os
from pathlib import Path
import sys
args = sys.argv[1:]
if args[0] == "inspect":
    print("sha256:fixture")
elif "ps" in args:
    print("fixture-server")
elif "run" in args and args[-1] == "ets-controller":
    destination = next(arg.split("=", 1)[1] for arg in args if arg.startswith("ETS_RESULT_DIR="))
    directory = Path("test-results") / Path(destination).relative_to("/results")
    host_created = directory.is_dir()
    directory.mkdir(parents=True, exist_ok=True)
    policy = json.loads(Path("testing/officialets/coverage-policy.json").read_text())
    count = policy["wcs20/" + directory.name]["minimum_assertions"]
    skip = '<skipped message="unexpected skip"/>' if os.environ.get("FIXTURE_SKIP") else ""
    cases = ''.join(f'<testcase classname="assertion.fixture" name="case{i}">{skip if i == 0 else ""}</testcase>' for i in range(count))
    (directory / "junit.xml").write_text("<testsuite>" + cases + "</testsuite>")
    if not host_created:
        # Model a root-created 0755 bind-mount directory: the non-root host
        # can read controller evidence but cannot add coverage-check.json.
        directory.chmod(0o555)
    if os.environ.get("FIXTURE_CONTROLLER_FAILURE"):
        sys.exit(1)
''')

    def tearDown(self):
        for path in self.root.rglob("*"):
            if path.is_dir():
                path.chmod(0o755)
        self.temp.cleanup()

    def command(self, name, code):
        path = self.bin / name
        path.write_text(f"#!{sys.executable}\n" + code)
        path.chmod(0o755)

    def run_fixture(self, **extra_env):
        env = dict(os.environ, PATH=f"{self.bin}{os.pathsep}{os.environ['PATH']}",
                   CONFORMANCE_SKIP_BUILD="false", KEEP_CONFORMANCE_ENVIRONMENT="false")
        env.update(extra_env)
        return subprocess.run(["bash", "scripts/conformance/run-official.sh", "wcs20"],
                              cwd=self.root, env=env, text=True, capture_output=True, timeout=30)

    def coverage(self, profile):
        return json.loads((self.root / "test-results/conformance/wcs20" / profile / "coverage-check.json").read_text())

    @unittest.skipIf(os.geteuid() == 0, "requires an unprivileged host to reproduce CI directory permissions")
    def test_host_can_write_coverage_for_every_profile(self):
        result = self.run_fixture()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        for profile in ("core", "crs"):
            self.assertTrue(self.coverage(profile)["passed"])

    def test_unreviewed_skips_still_fail_the_runner(self):
        result = self.run_fixture(FIXTURE_SKIP="true")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        for profile in ("core", "crs"):
            coverage = self.coverage(profile)
            self.assertFalse(coverage["passed"])
            self.assertIn("Unreviewed skips", coverage["errors"][0])

    def test_controller_failure_is_not_masked_by_a_passing_coverage_check(self):
        result = self.run_fixture(FIXTURE_CONTROLLER_FAILURE="true")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        for profile in ("core", "crs"):
            self.assertTrue(self.coverage(profile)["passed"])


if __name__ == "__main__":
    unittest.main()
