"""Check STAC evidence production without downloading schemas or starting a server."""
from contextlib import redirect_stdout
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("stac_runner", ROOT / "testing/stac/run.py")
runner = importlib.util.module_from_spec(spec)
with patch.dict(sys.modules, {"requests": SimpleNamespace(), "pystac_client": SimpleNamespace(Client=None)}):
    spec.loader.exec_module(runner)


class STACRunnerTests(unittest.TestCase):
    def execute(self, output, codes, search_fault=None):
        items = [SimpleNamespace(id=str(i)) for i in range(300)]
        items[0].bbox = [20.2, 20.2, 20.4, 20.4]

        def search(**kwargs):
            result = items
            if "bbox" in kwargs:
                if search_fault == "missing-probe":
                    result = []
                elif search_fault == "ignore-bbox" or (search_fault == "post-ignore-bbox" and kwargs["method"] == "POST") or kwargs["bbox"] == [20, 20, 21, 21]:
                    result = items[:1]
                else:
                    result = []
            return SimpleNamespace(items=lambda: iter(result))

        client = SimpleNamespace(search=search)

        def open_client(url):
            print("Original client diagnostic", file=sys.stderr)
            return client

        def command(args, stdout, **kwargs):
            stdout.write("Original subprocess output\n")
            return SimpleNamespace(returncode=next(codes))

        response = SimpleNamespace(raise_for_status=lambda: None, json=lambda: {"features": items})
        versions = json.loads((ROOT / "testing/officialets/versions.lock.json").read_text())["stac_validation"]["tools"]
        with patch.dict(os.environ, STAC_RESULTS=str(output), NEOSERVER_COMMIT="fixture-commit", NEOSERVER_IMAGE_ID="sha256:fixture"), \
             patch.object(runner, "version", side_effect=versions.__getitem__), \
             patch.object(runner.subprocess, "run", side_effect=command), \
             patch.object(runner, "Client", SimpleNamespace(open=open_client)), \
             patch.object(runner, "requests", SimpleNamespace(get=lambda *args, **kwargs: response)), \
             redirect_stdout(io.StringIO()):
            return runner.main()

    def test_versioned_provenance_and_original_logs(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            self.assertEqual(self.execute(output, iter([0, 0, 0, 0])), 0)
            result = json.loads((output / "results.json").read_text())
            self.assertEqual(result["schema_version"], 1)
            self.assertEqual(result["neoserver_commit"], "fixture-commit")
            self.assertEqual(result["neoserver_image_id"], "sha256:fixture")
            self.assertLessEqual(result["started_at"], result["completed_at"])
            self.assertEqual(len(result["checks"]), 3)
            self.assertIn("Original client diagnostic", (output / "pystac-client.log").read_text())
            for method in ("GET", "POST"):
                self.assertIn(f"{method} ids+bbox: 0, matching bbox [20, 20, 21, 21]: 1 Item; non-intersecting bbox [21.4, 21.4, 22.4, 22.4]: 0 Items.",
                              (output / "pystac-client.log").read_text())
            for check in result["checks"].values():
                self.assertEqual(check["exit_code"], 0)
                self.assertTrue(check["commands"])
                self.assertTrue(check["version"])
                self.assertTrue((output / check["log"]).is_file())

    def test_missing_probe_or_ids_overriding_bbox_cannot_pass(self):
        for fault in ("missing-probe", "ignore-bbox", "post-ignore-bbox"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as tmp:
                output = Path(tmp)
                self.assertEqual(self.execute(output, iter([0, 0, 0, 0]), search_fault=fault), 1)
                result = json.loads((output / "results.json").read_text())
                self.assertEqual(result["checks"]["pystac-client"]["exit_code"], 1)
                message = "probe box" if fault == "missing-probe" else "must not override"
                self.assertIn(message, (output / "pystac-client.log").read_text())

    def test_terminated_document_validator_cannot_be_masked_by_later_success(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            self.assertEqual(self.execute(output, iter([0, -9, 0, 0])), 1)
            result = json.loads((output / "results.json").read_text())
            self.assertEqual(result["checks"]["stac-validator"]["exit_code"], -9)

    def test_interrupted_run_removes_previous_success(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            (output / "results.json").write_text('{"exit_codes":{"stac-api-validator":0}}')
            with self.assertRaises(StopIteration):
                self.execute(output, iter([]))
            self.assertFalse((output / "results.json").exists())


if __name__ == "__main__":
    unittest.main()
