import copy
import importlib.util
import io
import json
from pathlib import Path
import stat
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location("publish_pages", Path(__file__).with_name("publish-pages.py"))
pages = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pages)


def run_record(**values):
    return dict({
        "id": 123, "run_number": 10, "run_attempt": 1, "head_sha": "new",
        "repository": {"full_name": pages.REPOSITORY}, "head_repository": {"full_name": pages.REPOSITORY},
        "status": "completed", "conclusion": "success", "path": pages.CONFORMANCE,
        "head_branch": "main", "event": "push", "created_at": "2026-09-19T10:00:00Z",
    }, **values)


class EligibilityTests(unittest.TestCase):
    def test_events(self):
        for event in ("push", "schedule", "workflow_dispatch"):
            self.assertEqual(pages.eligible(run_record(event=event)), "latest")
        self.assertEqual(pages.eligible(run_record(conclusion="failure")), "latest")
        self.assertEqual(pages.eligible(run_record(path=pages.RELEASE, head_branch="v0.1.1")), "releases/v0.1.1")
        self.assertEqual(pages.eligible(run_record(path=pages.RELEASE, head_branch="v1.0.0-beta.1")), "releases/v1.0.0-beta.1")
        for overrides in (
            {"event": "pull_request"}, {"head_branch": "feature"}, {"head_branch": "v0.1.1"},
            {"status": "in_progress"}, {"conclusion": "cancelled"},
            {"head_repository": {"full_name": "someone/fork"}},
            {"repository": {"full_name": "someone/fork"}},
            {"path": pages.RELEASE, "head_branch": "v0.1.1", "event": "workflow_dispatch"},
            {"path": pages.RELEASE, "head_branch": "v0.1.1", "conclusion": "failure"},
            {"path": pages.RELEASE, "head_branch": "../escape"},
        ):
            with self.subTest(overrides=overrides):
                self.assertIsNone(pages.eligible(run_record(**overrides)))

    def test_stale_commits_and_attempts(self):
        previous = {"commit": "old", "run_number": 9, "run_attempt": 1}
        self.assertTrue(pages.newer(run_record(), previous, lambda *_: "ahead"))
        self.assertFalse(pages.newer(run_record(), previous, lambda *_: "behind"))
        self.assertFalse(pages.newer(run_record(), previous, lambda *_: "diverged"))
        previous["commit"] = "new"
        self.assertTrue(pages.newer(run_record(), previous, lambda *_: self.fail("should not compare same commit")))
        previous["run_number"] = 10
        self.assertFalse(pages.newer(run_record(), previous, lambda *_: None))
        self.assertTrue(pages.newer(run_record(run_attempt=2), previous, lambda *_: None))


class ArtifactTests(unittest.TestCase):
    def archive(self, name, body=b"exact original evidence\r\n", mode=0):
        data = io.BytesIO()
        with zipfile.ZipFile(data, "w") as z:
            entry = zipfile.ZipInfo(name)
            entry.external_attr = mode << 16
            z.writestr(entry, body)
        return data.getvalue()

    def test_raw_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            pages.extract(self.archive("core/result.xml"), root)
            self.assertEqual((root / "core/result.xml").read_bytes(), b"exact original evidence\r\n")

    def test_paths_and_symlinks(self):
        for name in ("../escape", "/absolute", "core/../../escape", "C:\\windows", ".git/config"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                with self.assertRaises(ValueError):
                    pages.extract(self.archive(name), Path(tmp))
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):
                pages.extract(self.archive("link", mode=stat.S_IFLNK | 0o777), Path(tmp))

    def test_limits(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(pages, "MAX_EXTRACT", 2):
            with self.assertRaises(ValueError):
                pages.extract(self.archive("core/result.xml"), Path(tmp))
        with tempfile.TemporaryDirectory() as tmp, patch.object(pages, "MAX_SITE", 2):
            (Path(tmp) / "index.html").write_text("report")
            with self.assertRaises(ValueError):
                pages.validate_size(Path(tmp))


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.history = Path(self.tmp.name) / "history"
        self.history.mkdir()
        (self.history / ".git").write_text("worktree marker")
        self.state = {"schema_version": 1, "since": "2026-09-01T00:00:00Z", "releases": {}}
        self.runs = [run_record()]
        self.rendered = []

    def api(self, path, paginated=False):
        if path.startswith("actions/runs/"):
            return copy.deepcopy(self.runs[0])
        if "workflows/release.yml" in path:
            return [{"workflow_runs": [r for r in self.runs if r["path"] == pages.RELEASE]}]
        if "workflows/conformance.yml" in path:
            return [{"workflow_runs": [r for r in self.runs if r["path"] == pages.CONFORMANCE]}]
        if path.startswith("compare/"):
            return {"status": "ahead"}
        self.fail("unexpected API call " + path)

    def render(self, run, prefix, generator, work):
        self.rendered.append(run["id"])
        site = work / "site"
        site.mkdir(parents=True)
        (site / "index.html").write_text(str(run["id"]))
        return site

    def command(self, args):
        (Path(args[-1]) / "index.html").write_text("archive index")

    def publish(self):
        (self.history / "state.json").write_text(json.dumps(self.state))
        with patch.object(pages, "api", self.api), patch.object(pages, "render_run", self.render), patch.object(pages, "command", self.command):
            return pages.publish(SimpleNamespace(run_id=123, history=self.history, generator=Path("etsreport")))

    def test_coalesced_release_and_main_events(self):
        self.runs += [run_record(id=124, path=pages.RELEASE, head_branch="v0.1.1")]
        self.assertTrue(self.publish())
        self.assertEqual(self.rendered, [123, 124])
        self.assertTrue((self.history / "latest/index.html").exists())
        self.assertTrue((self.history / "releases/v0.1.1/index.html").exists())
        self.assertEqual((self.history / ".git").read_text(), "worktree marker")
        self.state = json.loads((self.history / "state.json").read_text())
        self.assertFalse(self.publish())
        self.assertEqual(self.rendered, [123, 124])

    def test_invalid_generation_leaves_history_untouched(self):
        (self.history / "index.html").write_text("previous deployment")
        with patch.object(pages, "MAX_SITE", 1):
            with self.assertRaises(ValueError):
                self.publish()
        self.assertEqual((self.history / "index.html").read_text(), "previous deployment")
        self.assertFalse((self.history / "latest").exists())

    def test_new_main_preserves_releases(self):
        release = self.history / "releases/v0.1.0"
        release.mkdir(parents=True)
        (release / "evidence.zip").write_bytes(b"historical evidence")
        self.assertTrue(self.publish())
        self.assertEqual((release / "evidence.zip").read_bytes(), b"historical evidence")

    def test_release_started_before_enablement_is_retained_if_it_finishes_after(self):
        self.state["since"] = "2026-09-19T09:00:00Z"
        self.runs += [run_record(id=125, path=pages.RELEASE, head_branch="v0.1.1",
                                 created_at="2026-09-19T08:00:00Z", updated_at="2026-09-19T11:00:00Z")]
        self.assertTrue(self.publish())
        self.assertTrue((self.history / "releases/v0.1.1/index.html").exists())

    def test_first_publication_uses_workflow_enablement_time(self):
        with patch.object(pages, "api", return_value={"created_at": "2026-09-01T00:00:00Z"}):
            self.assertEqual(pages.load_state(self.history)["since"], "2026-09-01T00:00:00Z")

    def test_timezone_offsets_do_not_drop_release_completions(self):
        self.state["since"] = "2026-09-19T11:30:00.000+02:00"
        self.runs += [run_record(id=126, path=pages.RELEASE, head_branch="v0.1.1")]
        self.assertTrue(self.publish())
        self.assertTrue((self.history / "releases/v0.1.1/index.html").exists())


if __name__ == "__main__":
    unittest.main()
