#!/usr/bin/env python3
"""Run independent STAC API, document, and client interoperability lanes.

Stock OGC Features ETS runs separately through ogcapi-features10/stac. Results
are kept verbatim; no validator failures or warnings are rewritten as passes.
"""
import json
from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timezone
from importlib.metadata import version
import os
from pathlib import Path
import subprocess
import sys

import requests
from pystac_client import Client


def main():
    root = os.environ.get("STAC_URL", "http://server:9000/workspaces/demo/stac/").rstrip("/")
    output = Path(os.environ.get("STAC_RESULTS", "/results/conformance/stac"))
    output.mkdir(parents=True, exist_ok=True)
    # A failed or interrupted rerun must never leave a previous success record.
    (output / "results.json").unlink(missing_ok=True)
    started = datetime.now(timezone.utc).isoformat()
    checks = {}

    def record(tool, code, commands):
        checks[tool] = {"version": version(tool), "exit_code": code,
                        "commands": commands, "log": tool + ".log"}

    geometry = {"type": "Polygon", "coordinates": [[[-10, -10], [10, -10], [10, 10], [-10, 10], [-10, -10]]]}
    command = ["stac-api-validator", "--root-url", root + "/", "--collection", "conformance-scenes", "--geometry", json.dumps(geometry), "--validate-pagination"]
    for conformance in ("core", "collections", "features", "item-search"):
        command += ["--conformance", conformance]
    results = {}
    with (output / "stac-api-validator.log").open("w") as log:
        results["stac-api-validator"] = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=False).returncode
    record("stac-api-validator", results["stac-api-validator"], [command])
    # Map pinned upstream schema URLs to vendored files. This validates against
    # the same versions without silently following a future upstream schema.
    runtime_schemas = Path(__file__).resolve().parents[2] / "internal/stacmodel/schemas"
    catalog_schemas = Path(__file__).resolve().parent / "schemas"
    schema_args = []
    for schemas in (runtime_schemas, catalog_schemas):
        for url, entry in json.loads((schemas / "manifest.json").read_text()).items():
            schema_args += ["--schema-map", url, str(schemas / entry["file"])]
    with (output / "stac-validator.log").open("w") as log:
        codes = []
        commands = []
        for suffix, extra in (("/", []), ("/collections", ["--collections"]), ("/search?limit=1000", ["--item-collection"])):
            cmd = ["stac-validator", "validate", root + suffix, "--core", *schema_args, *extra]
            commands.append(cmd)
            codes.append(subprocess.run(cmd, stdout=log, stderr=subprocess.STDOUT, check=False).returncode)
        results["stac-validator"] = next((code for code in codes if code != 0), 0)
    record("stac-validator", results["stac-validator"], commands)
    with (output / "pystac-client.log").open("w") as log, redirect_stdout(log), redirect_stderr(log):
        try:
            client = Client.open(root + "/")
            get_items = list(client.search(collections=["conformance-scenes"], method="GET", limit=17).items())
            post_items = list(client.search(collections=["conformance-scenes"], method="POST", limit=17).items())
            assert len(get_items) == 300
            assert [item.id for item in get_items] == [item.id for item in post_items]
            assert len({item.id for item in get_items}) == 300
            response = requests.get(root + "/search", params={"limit": 10000}, timeout=30)
            response.raise_for_status()
            assert len(response.json()["features"]) == 300
            print("GET and POST pagination returned the same 300 unique Items. Limit clamping passed.")
            # The pinned API validator needs an Item in this probe box before
            # it can exercise its ids/other-parameters check. Keep that fixture
            # precondition and the GET/POST outcomes explicit in our evidence.
            probe_box = [20, 20, 21, 21]
            probe = next(client.search(collections=["conformance-scenes"], bbox=probe_box,
                                       method="GET", limit=1, max_items=1).items(), None)
            assert probe is not None, "The fixture must contain an Item in the API validator's 20,20,21,21 probe box"
            assert probe.bbox is not None, "The probe Item must have a bbox"
            outside_box = [probe.bbox[2] + 1, probe.bbox[3] + 1, probe.bbox[2] + 2, probe.bbox[3] + 2]
            for method in ("GET", "POST"):
                params = {"collections": ["conformance-scenes"], "ids": [probe.id], "method": method}
                matching = list(client.search(**params, bbox=probe_box).items())
                assert [item.id for item in matching] == [probe.id], f"{method} ids+bbox must return the matching probe Item"
                excluded = list(client.search(**params, bbox=outside_box).items())
                assert not excluded, f"{method} ids must not override a non-intersecting bbox"
                print(f"{method} ids+bbox: {probe.id}, matching bbox {probe_box}: 1 Item; non-intersecting bbox {outside_box}: 0 Items.")
            results["pystac-client"] = 0
        except Exception as error:
            print(repr(error))
            results["pystac-client"] = 1
    record("pystac-client", results["pystac-client"], [[sys.executable, *sys.argv]])
    evidence = {"schema_version": 1, "root": root, "exit_codes": results,
                "neoserver_commit": os.environ.get("NEOSERVER_COMMIT", ""),
                "neoserver_image_id": os.environ.get("NEOSERVER_IMAGE_ID", ""),
                "started_at": started, "completed_at": datetime.now(timezone.utc).isoformat(),
                "checks": checks}
    temporary = output / "results.json.tmp"
    temporary.write_text(json.dumps(evidence, indent=2) + "\n")
    temporary.replace(output / "results.json")
    print(json.dumps(results))
    return int(any(results.values()))


if __name__ == "__main__":
    sys.exit(main())
