#!/usr/bin/env python3
"""Collect trusted workflow evidence and update the generated Pages tree.

Run from the default-branch checkout. Downloaded artifacts are data only; the
generator executable always comes from this checkout. Git commits and Pages
deployment are separate workflow steps, after every size/content check passes.
"""

import argparse
import base64
from datetime import datetime
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import subprocess
import tempfile
import zipfile

REPOSITORY = "tobilg/neoserver"
ORIGIN = "https://conformance.neoserver.cloud"
CONFORMANCE = ".github/workflows/conformance.yml"
RELEASE = ".github/workflows/release.yml"
TAG = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?\Z")
SLUG = re.compile(r"[a-z0-9][a-z0-9-]*\Z")
MAX_SITE = 900 * 1024 * 1024
MAX_FILE = 95 * 1024 * 1024
MAX_EXTRACT = 1024 * 1024 * 1024


def command(args, **kwargs):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, **kwargs).stdout


def api(path, paginated=False):
    args = ["gh", "api", f"repos/{REPOSITORY}/{path}"]
    if paginated:
        args += ["--paginate", "--slurp"]
    return json.loads(command(args))


def timestamp(value):
    # GitHub workflow creation timestamps can carry offsets; run timestamps
    # commonly use Z. Compare instants rather than their string encodings.
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("Workflow timestamps must include a timezone")
    return parsed


def eligible(run):
    """Only full main runs and successful real release runs can publish."""
    if run.get("repository", {}).get("full_name") != REPOSITORY:
        return None
    if run.get("head_repository", {}).get("full_name") != REPOSITORY:
        return None
    if run.get("status") != "completed" or run.get("conclusion") in ("cancelled", "skipped", "action_required", "stale"):
        return None
    if run.get("path") == CONFORMANCE and run.get("head_branch") == "main" and run.get("event") in ("push", "schedule", "workflow_dispatch"):
        return "latest"
    if run.get("path") == RELEASE and run.get("event") == "push" and run.get("conclusion") == "success" and TAG.fullmatch(run.get("head_branch", "")):
        return "releases/" + run["head_branch"]
    return None


def newer(candidate, previous, compare):
    """An older commit rerun must never replace newer main-branch evidence."""
    if not previous:
        return True
    if candidate["head_sha"] != previous["commit"]:
        relation = compare(previous["commit"], candidate["head_sha"])
        if relation == "ahead":
            return True
        if relation in ("behind", "diverged"):
            return False
    return (candidate["run_number"], candidate["run_attempt"]) > (previous["run_number"], previous["run_attempt"])


def extract(data, destination):
    """Do not trust ZIP paths, permissions, sizes, or symlinks."""
    destination.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        total = 0
        seen = set()
        for member in archive.infolist():
            name = member.filename
            path = PurePosixPath(name)
            mode = member.external_attr >> 16
            if (not name or "\\" in name or path.is_absolute() or ".." in path.parts
                    or ".git" in path.parts or ":" in name or name in seen
                    or stat.S_ISLNK(mode) or (stat.S_IFMT(mode) not in (0, stat.S_IFREG, stat.S_IFDIR))):
                raise ValueError(f"Unsafe archive entry: {name!r}")
            seen.add(name)
            total += member.file_size
            if total > MAX_EXTRACT:
                raise ValueError("Evidence archive exceeds extraction limit")
            target = destination.joinpath(*path.parts)
            if member.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            with archive.open(member) as source, target.open("xb") as output:
                shutil.copyfileobj(source, output)


def expected_artifacts(manifest):
    result = set()
    for name, profile in manifest["profiles"].items():
        suite = profile["suite"]
        if not SLUG.fullmatch(suite):
            raise ValueError("Invalid suite in source manifest")
        if profile["evidence_kind"] == "official":
            result.add("official-ets-" + suite)
        elif profile["evidence_kind"] == "official-derived" and name == "wcs20/interpolation":
            result.add("official-derived-ets-wcs20-interpolation")
        else:
            raise ValueError("Unsupported evidence kind/profile")
    return result


def validate_size(site):
    total = 0
    for path in site.rglob("*"):
        if ".git" in path.relative_to(site).parts:
            continue
        if path.is_symlink():
            raise ValueError(f"Generated site contains a symlink: {path}")
        if path.is_file():
            size = path.stat().st_size
            if size > MAX_FILE:
                raise ValueError(f"Generated file exceeds 95 MiB: {path}")
            total += size
    if total > MAX_SITE:
        raise ValueError("Site exceeds 900 MiB budget; retained releases were not pruned")
    return total


def render_run(run, prefix, generator, work):
    source = api(f"contents/testing/officialets/versions.lock.json?ref={run['head_sha']}")
    manifest_bytes = base64.b64decode(source["content"], validate=False)
    manifest = json.loads(manifest_bytes)
    work.mkdir(parents=True)
    manifest_path = work / "manifest.json"
    manifest_path.write_bytes(manifest_bytes)
    evidence = work / "evidence"
    evidence.mkdir()
    artifacts = [a for page in api(f"actions/runs/{run['id']}/artifacts?per_page=100", True) for a in page["artifacts"]]
    # Re-running failed jobs can reuse evidence from successful jobs in an
    # earlier attempt of this same run, but never evidence from another run.
    for name in sorted(expected_artifacts(manifest)):
        matches = [a for a in artifacts if a["name"] == name and not a["expired"]]
        if not matches:
            if prefix.startswith("releases/"):
                raise ValueError(f"Release evidence is missing or expired: {name}")
            print(f"Missing evidence for {name}; report will mark its profiles incomplete")
            continue
        artifact = max(matches, key=lambda a: a["id"])
        data = command(["gh", "api", f"repos/{REPOSITORY}/actions/artifacts/{artifact['id']}/zip"])
        extract(data, evidence / name)
    jobs = [j for page in api(f"actions/runs/{run['id']}/jobs?filter=all&per_page=100", True) for j in page["jobs"]]
    outcomes = {}
    for job in sorted(jobs, key=lambda j: j["id"]):
        name = job["name"]
        if "Official ETS" in name or "Official-derived ETS" in name or "Select official ETS" in name:
            outcomes[name] = job["conclusion"] or job["status"]
    context = {
        "schema_version": 1, "repository": REPOSITORY, "run_id": str(run["id"]),
        "run_attempt": run["run_attempt"], "run_number": run["run_number"], "commit": run["head_sha"],
        "ref": "refs/heads/main" if prefix == "latest" else "refs/tags/" + run["head_branch"],
        "event": run["event"], "url": run["html_url"], "created_at": run["created_at"],
        "selected_suites": sorted(manifest["suites"]), "selection_known": True, "jobs": outcomes,
    }
    if prefix.startswith("releases/"):
        release = api("releases/tags/" + run["head_branch"])
        commit = api("commits/" + run["head_branch"])
        if release["draft"] or not release.get("published_at") or commit["sha"] != run["head_sha"]:
            raise ValueError("Release tag does not identify this successful release run")
        context.update(release=release["tag_name"], prerelease=release["prerelease"])
    context_path = work / "run.json"
    context_path.write_text(json.dumps(context))
    output = work / "report"
    command([str(generator), "--input", str(evidence), "--manifest", str(manifest_path), "--context", str(context_path), "--output", str(output), "--prefix", prefix])
    return output / "site"


def load_state(history):
    path = history / "state.json"
    if path.exists():
        state = json.loads(path.read_text())
        if state.get("schema_version") != 1:
            raise ValueError("Unsupported publishing state schema")
        return state
    # Anchor retention to workflow enablement, not the first deployment's
    # start time: release events can complete while that deployment is queued.
    enabled = api("actions/workflows/conformance-pages.yml")["created_at"]
    return {"schema_version": 1, "since": enabled, "releases": {}}


def publish(args):
    history = args.history.resolve()
    generator = args.generator.resolve()
    source_run = api(f"actions/runs/{args.run_id}")
    destination = eligible(source_run)
    if not destination:
        print("Source run is not eligible for public publishing")
        return False
    state = load_state(history)
    candidates = {source_run["id"]: source_run}
    # Concurrency may coalesce pending events. Every surviving invocation scans
    # for unarchived release completions since reporting was enabled.
    for page in api("actions/workflows/release.yml/runs?status=success&per_page=100", True):
        for run in page["workflow_runs"]:
            if timestamp(run.get("updated_at", run["created_at"])) < timestamp(state["since"]):
                continue
            if eligible(run) and run["head_branch"] not in state["releases"]:
                candidates[run["id"]] = run
    # Likewise pick up the newest main completion if its event was coalesced.
    for page in api("actions/workflows/conformance.yml/runs?branch=main&status=completed&per_page=100", True):
        for run in page["workflow_runs"]:
            if timestamp(run.get("updated_at", run["created_at"])) >= timestamp(state["since"]) and eligible(run) == "latest":
                candidates[run["id"]] = run
    compare = lambda base, head: api(f"compare/{base}...{head}")["status"]
    # Stage all modifications outside the branch. A validation failure leaves
    # the retained tree and the currently deployed site unchanged.
    with tempfile.TemporaryDirectory(prefix="conformance-pages-") as tmp:
        tmp = Path(tmp)
        staged = tmp / "site"
        shutil.copytree(history, staged, ignore=shutil.ignore_patterns(".git"), dirs_exist_ok=True)
        changed = False
        for run in sorted(candidates.values(), key=lambda r: (timestamp(r["created_at"]), r["run_number"], r["run_attempt"])):
            prefix = eligible(run)
            if not prefix:
                continue
            if prefix == "latest":
                if not newer(run, state.get("latest"), compare):
                    continue
            elif prefix.removeprefix("releases/") in state["releases"]:
                previous = state["releases"][prefix.removeprefix("releases/")]
                if previous["commit"] != run["head_sha"]:
                    raise ValueError("Refusing to overwrite a retained release with a different commit")
                continue
            site = render_run(run, prefix, generator, tmp / f"run-{run['id']}")
            dest = staged / prefix
            if dest.exists():
                shutil.rmtree(dest)
            shutil.copytree(site, dest)
            record = {"commit": run["head_sha"], "run_id": str(run["id"]), "run_number": run["run_number"], "run_attempt": run["run_attempt"]}
            if prefix == "latest":
                state["latest"] = record
            else:
                state["releases"][run["head_branch"]] = record
            changed = True
        if changed:
            (staged / "state.json").write_text(json.dumps(state, indent=2) + "\n")
            command([str(generator), "--index", str(staged)])
            size = validate_size(staged)
            for entry in history.iterdir():
                if entry.name == ".git":
                    continue
                if entry.is_dir():
                    shutil.rmtree(entry)
                else:
                    entry.unlink()
            shutil.copytree(staged, history, dirs_exist_ok=True)
            print(f"Prepared {size:,} bytes for {ORIGIN}/")
    return changed


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run-id", required=True, type=int)
    parser.add_argument("--history", required=True, type=Path)
    parser.add_argument("--generator", required=True, type=Path)
    args = parser.parse_args()
    changed = publish(args)
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a") as output:
            output.write(f"changed={'true' if changed else 'false'}\n")
